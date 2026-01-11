// Package main implements the control-plane API for qfactory.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/AIfactory-hq/qfactory/apps/orchestrator-worker/workflow"
	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

const TaskQueue = "qfactory-v0"

// Store holds in-memory state for runs and events.
// TODO: Replace with Postgres in v0.2+
type Store struct {
	mu       sync.RWMutex
	runs     map[string]*contracts.WorkflowRun
	events   map[string][]events.Event
	sseChans map[string][]chan events.Event
}

// NewStore creates a new in-memory store.
func NewStore() *Store {
	return &Store{
		runs:     make(map[string]*contracts.WorkflowRun),
		events:   make(map[string][]events.Event),
		sseChans: make(map[string][]chan events.Event),
	}
}

// CreateRun stores a new workflow run.
func (s *Store) CreateRun(run *contracts.WorkflowRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = run
	s.events[run.ID] = []events.Event{}
}

// GetRun retrieves a workflow run by ID.
func (s *Store) GetRun(id string) (*contracts.WorkflowRun, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	return run, ok
}

// AddEvent stores an event and notifies SSE subscribers.
func (s *Store) AddEvent(event events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events[event.RunID] = append(s.events[event.RunID], event)

	// Update run state based on event
	if run, ok := s.runs[event.RunID]; ok {
		s.updateRunFromEvent(run, event)
	}

	// Notify SSE subscribers
	for _, ch := range s.sseChans[event.RunID] {
		select {
		case ch <- event:
		default:
			// Drop if channel is full
		}
	}
}

func (s *Store) updateRunFromEvent(run *contracts.WorkflowRun, event events.Event) {
	var payload events.StagePayload
	_ = json.Unmarshal(event.Payload, &payload)

	run.UpdatedAt = event.Timestamp

	switch event.Type {
	case events.EventTypeStageStarted:
		run.CurrentStage = payload.StageName
		if payload.StageIndex < len(run.Stages) {
			run.Stages[payload.StageIndex].State = contracts.StageStateRunning
			run.Stages[payload.StageIndex].StartedAt = &event.Timestamp
		}
	case events.EventTypeStageCompleted:
		if payload.StageIndex < len(run.Stages) {
			run.Stages[payload.StageIndex].State = contracts.StageStateCompleted
			run.Stages[payload.StageIndex].CompletedAt = &event.Timestamp
		}
		// Check if all stages completed
		allDone := true
		for _, stage := range run.Stages {
			if stage.State != contracts.StageStateCompleted {
				allDone = false
				break
			}
		}
		if allDone {
			run.Status = contracts.RunStatusCompleted
			run.CompletedAt = &event.Timestamp
			run.CurrentStage = ""
		}
	case events.EventTypeStageFailed:
		if payload.StageIndex < len(run.Stages) {
			run.Stages[payload.StageIndex].State = contracts.StageStateFailed
			run.Stages[payload.StageIndex].Error = payload.Error
		}
		run.Status = contracts.RunStatusFailed
		run.Error = payload.Error
	}
}

// GetEvents retrieves all events for a run.
func (s *Store) GetEvents(runID string) []events.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evts := s.events[runID]
	result := make([]events.Event, len(evts))
	copy(result, evts)
	return result
}

// SubscribeSSE creates a channel for SSE events.
func (s *Store) SubscribeSSE(runID string) chan events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch := make(chan events.Event, 100)
	s.sseChans[runID] = append(s.sseChans[runID], ch)
	return ch
}

// UnsubscribeSSE removes an SSE subscription.
func (s *Store) UnsubscribeSSE(runID string, ch chan events.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	chans := s.sseChans[runID]
	for i, c := range chans {
		if c == ch {
			s.sseChans[runID] = append(chans[:i], chans[i+1:]...)
			close(ch)
			return
		}
	}
}

func main() {
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	tc, err := client.Dial(client.Options{
		HostPort: temporalAddr,
	})
	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer tc.Close()

	store := NewStore()
	server := &Server{
		store:    store,
		temporal: tc,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /workflows", server.handleCreateWorkflow)
	mux.HandleFunc("GET /runs/{id}", server.handleGetRun)
	mux.HandleFunc("GET /runs/{id}/events", server.handleSSE)
	mux.HandleFunc("POST /internal/events", server.handleInternalEvent)

	addr := ":8090"
	log.Printf("Control-plane API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Server holds dependencies for HTTP handlers.
type Server struct {
	store    *Store
	temporal client.Client
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req contracts.WorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := req.Validate(); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	runID := generateRunID()
	now := time.Now().UTC()

	// Initialize stages
	stages := make([]contracts.StageStatus, len(contracts.DemoWorkflowStages))
	for i, name := range contracts.DemoWorkflowStages {
		stages[i] = contracts.StageStatus{
			Name:  name,
			State: contracts.StageStatePending,
		}
	}

	run := &contracts.WorkflowRun{
		ID:        runID,
		Status:    contracts.RunStatusRunning,
		Stages:    stages,
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.store.CreateRun(run)

	// Start Temporal workflow
	opts := client.StartWorkflowOptions{
		ID:        "demo-" + runID,
		TaskQueue: TaskQueue,
	}

	we, err := s.temporal.ExecuteWorkflow(context.Background(), opts, workflow.DemoWorkflow, runID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start workflow: %v", err))
		return
	}

	run.TemporalID = we.GetID()

	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	run, ok := s.store.GetRun(id)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	_, ok := s.store.GetRun(id)
	if !ok {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Replay existing events
	existingEvents := s.store.GetEvents(id)
	for _, event := range existingEvents {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	}
	flusher.Flush()

	// Subscribe to new events
	ch := s.store.SubscribeSSE(id)
	defer s.store.UnsubscribeSSE(id, ch)

	for {
		select {
		case event, ok := <-ch:
			if !ok {
				// Channel closed, end stream
				return
			}
			data, _ := json.Marshal(event)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleInternalEvent(w http.ResponseWriter, r *http.Request) {
	// TODO: Add authentication for production
	var event events.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid event payload")
		return
	}

	s.store.AddEvent(event)

	w.WriteHeader(http.StatusAccepted)
}

func generateRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
