// Package main implements the control-plane API for qfactory.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
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

// ListRuns returns all runs, newest first.
func (s *Store) ListRuns() []*contracts.WorkflowRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]*contracts.WorkflowRun, 0, len(s.runs))
	for _, run := range s.runs {
		runs = append(runs, run)
	}
	// Sort by created_at descending
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	return runs
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

// AddGateResult adds a gate result to a run.
func (s *Store) AddGateResult(runID string, result contracts.GateResult) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[runID]
	if !ok {
		return false
	}
	run.Gates = append(run.Gates, result)
	return true
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

	evidenceSvc := NewEvidenceService(contracts.EvidenceDir)

	store := NewStore()
	server := &Server{
		store:       store,
		temporal:    tc,
		evidenceSvc: evidenceSvc,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /workflows", server.handleCreateWorkflow)
	mux.HandleFunc("GET /runs", server.handleListRuns)
	mux.HandleFunc("GET /runs/{id}", server.handleGetRun)
	mux.HandleFunc("GET /runs/{id}/events", server.handleSSE)
	mux.HandleFunc("POST /runs/{id}/gates/pr1", server.handlePR1Gate)
	mux.HandleFunc("GET /runs/{id}/evidence", server.handleGetEvidence)
	mux.HandleFunc("GET /runs/{id}/evidence.zip", server.handleGetEvidenceZip)
	mux.HandleFunc("POST /internal/events", server.handleInternalEvent)

	addr := ":8090"
	log.Printf("Control-plane API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Server holds dependencies for HTTP handlers.
type Server struct {
	store       *Store
	temporal    client.Client
	evidenceSvc *EvidenceService
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

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	runs := s.store.ListRuns()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":  runs,
		"count": len(runs),
	})
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

func (s *Server) handlePR1Gate(w http.ResponseWriter, r *http.Request) {
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

	// Get timeout from env, default 60s
	timeoutSec := 60
	if envTimeout := os.Getenv("QF_PR1_TIMEOUT_SECONDS"); envTimeout != "" {
		if t, err := strconv.Atoi(envTimeout); err == nil && t > 0 {
			timeoutSec = t
		}
	}

	// Get Go version
	goVersionOut, _ := exec.Command("go", "version").Output()
	goVersion := string(bytes.TrimSpace(goVersionOut))

	// Execute go test ./...
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	startTime := time.Now().UTC()
	cmd := exec.CommandContext(ctx, "go", "test", "./...")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if ctx.Err() == context.DeadlineExceeded {
			exitCode = -1 // Timeout
		} else {
			exitCode = -2 // Other error
		}
	}

	passed := exitCode == 0

	// Build evidence
	evidence := contracts.GateEvidence{
		Level:      contracts.GateLevelPR1,
		Command:    "go test ./...",
		StartedAt:  startTime,
		EndedAt:    endTime,
		ExitCode:   exitCode,
		DurationMs: durationMs,
		Stdout:     stdout.String(),
		Stderr:     stderr.String(),
		GoVersion:  goVersion,
	}

	// Write evidence to disk
	evidencePath, writeErr := s.evidenceSvc.WriteGateEvidence(id, contracts.GateLevelPR1, evidence)
	if writeErr != nil {
		log.Printf("Failed to write evidence: %v", writeErr)
	}

	// Write events.jsonl
	evts := s.store.GetEvents(id)
	if err := s.evidenceSvc.WriteEventsJSONL(id, evts); err != nil {
		log.Printf("Failed to write events.jsonl: %v", err)
	}

	// Build check result
	checkMsg := "all tests passed"
	if !passed {
		if ctx.Err() == context.DeadlineExceeded {
			checkMsg = fmt.Sprintf("timeout after %ds", timeoutSec)
		} else {
			checkMsg = fmt.Sprintf("tests failed with exit code %d", exitCode)
		}
	}

	result := contracts.GateResult{
		Level:        contracts.GateLevelPR1,
		Passed:       passed,
		Timestamp:    endTime,
		DurationMs:   durationMs,
		EvidencePath: evidencePath,
		Checks: []contracts.Check{
			{
				Name:    "unit_tests",
				Passed:  passed,
				Message: checkMsg,
			},
		},
	}

	if !passed && ctx.Err() == context.DeadlineExceeded {
		result.Error = "timeout exceeded"
	}

	// Add to run
	s.store.AddGateResult(id, result)

	// Write manifest
	if err := s.evidenceSvc.WriteManifest(id, run, s.store.GetEvents(id)); err != nil {
		log.Printf("Failed to write manifest: %v", err)
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetEvidence(w http.ResponseWriter, r *http.Request) {
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

	manifestPath := filepath.Join(contracts.EvidenceDir, id, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONError(w, http.StatusNotFound, "evidence not found - run PR1 gate first")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to read manifest")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) handleGetEvidenceZip(w http.ResponseWriter, r *http.Request) {
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

	evidenceDir := filepath.Join(contracts.EvidenceDir, id)
	if _, err := os.Stat(evidenceDir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "evidence not found - run PR1 gate first")
		return
	}

	zipPath := filepath.Join(contracts.EvidenceDir, id+".zip")

	// Generate zip if not exists or older than evidence dir
	needsRegen := true
	if zipInfo, err := os.Stat(zipPath); err == nil {
		if dirInfo, err := os.Stat(evidenceDir); err == nil {
			needsRegen = zipInfo.ModTime().Before(dirInfo.ModTime())
		}
	}

	if needsRegen {
		if err := s.evidenceSvc.ZipEvidence(id, run, s.store.GetEvents(id)); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create zip: %v", err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", id))
	http.ServeFile(w, r, zipPath)
}

func generateRunID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// EvidenceService handles writing evidence bundles to disk.
type EvidenceService struct {
	baseDir string
}

// NewEvidenceService creates a new evidence service.
func NewEvidenceService(baseDir string) *EvidenceService {
	return &EvidenceService{baseDir: baseDir}
}

// WriteGateEvidence writes gate evidence to disk.
func (e *EvidenceService) WriteGateEvidence(runID, level string, evidence contracts.GateEvidence) (string, error) {
	gateDir := filepath.Join(e.baseDir, runID, "gates", level)
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create gate dir: %w", err)
	}

	// Write command.txt
	if err := os.WriteFile(filepath.Join(gateDir, "command.txt"), []byte(evidence.Command+"\n"), 0644); err != nil {
		return "", fmt.Errorf("failed to write command.txt: %w", err)
	}

	// Write stdout.log
	if err := os.WriteFile(filepath.Join(gateDir, "stdout.log"), []byte(evidence.Stdout), 0644); err != nil {
		return "", fmt.Errorf("failed to write stdout.log: %w", err)
	}

	// Write stderr.log
	if err := os.WriteFile(filepath.Join(gateDir, "stderr.log"), []byte(evidence.Stderr), 0644); err != nil {
		return "", fmt.Errorf("failed to write stderr.log: %w", err)
	}

	// Write result.json
	resultJSON, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	if err := os.WriteFile(filepath.Join(gateDir, "result.json"), resultJSON, 0644); err != nil {
		return "", fmt.Errorf("failed to write result.json: %w", err)
	}

	return gateDir, nil
}

// WriteEventsJSONL writes all events for a run in JSONL format.
func (e *EvidenceService) WriteEventsJSONL(runID string, evts []events.Event) error {
	runDir := filepath.Join(e.baseDir, runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run dir: %w", err)
	}

	f, err := os.Create(filepath.Join(runDir, "events.jsonl"))
	if err != nil {
		return fmt.Errorf("failed to create events.jsonl: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	for _, evt := range evts {
		line, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		w.Write(line)
		w.WriteByte('\n')
	}
	return w.Flush()
}

// WriteManifest writes the evidence manifest.
func (e *EvidenceService) WriteManifest(runID string, run *contracts.WorkflowRun, evts []events.Event) error {
	runDir := filepath.Join(e.baseDir, runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run dir: %w", err)
	}

	// Collect files
	var files []string
	filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(runDir, path)
		files = append(files, relPath)
		return nil
	})

	manifest := contracts.EvidenceManifest{
		RunID:       run.ID,
		TemporalID:  run.TemporalID,
		CreatedAt:   run.CreatedAt,
		CompletedAt: run.CompletedAt,
		Status:      run.Status,
		Gates:       run.Gates,
		Files:       files,
		GeneratedAt: time.Now().UTC(),
		Version:     "0.2.0",
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	return os.WriteFile(filepath.Join(runDir, "manifest.json"), data, 0644)
}

// WriteMetadata writes run metadata.
func (e *EvidenceService) WriteMetadata(runID string, run *contracts.WorkflowRun) error {
	runDir := filepath.Join(e.baseDir, runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run dir: %w", err)
	}

	metadata := map[string]interface{}{
		"id":           run.ID,
		"temporal_id":  run.TemporalID,
		"created_at":   run.CreatedAt,
		"completed_at": run.CompletedAt,
		"status":       run.Status,
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(filepath.Join(runDir, "metadata.json"), data, 0644)
}

// ZipEvidence creates a zip archive of the evidence bundle.
func (e *EvidenceService) ZipEvidence(runID string, run *contracts.WorkflowRun, evts []events.Event) error {
	runDir := filepath.Join(e.baseDir, runID)
	zipPath := filepath.Join(e.baseDir, runID+".zip")

	// Ensure manifest and metadata are up to date
	if err := e.WriteManifest(runID, run, evts); err != nil {
		return err
	}
	if err := e.WriteMetadata(runID, run); err != nil {
		return err
	}

	zipFile, err := os.Create(zipPath)
	if err != nil {
		return fmt.Errorf("failed to create zip file: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	return filepath.Walk(runDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(runDir, path)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.Join(runID, relPath)
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}
