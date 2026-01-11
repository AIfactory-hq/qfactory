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
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/client"

	"github.com/AIfactory-hq/qfactory/apps/orchestrator-worker/workflow"
	"github.com/AIfactory-hq/qfactory/internal/gates"
	"github.com/AIfactory-hq/qfactory/internal/modelruntime"
	"github.com/AIfactory-hq/qfactory/internal/store"
	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

const TaskQueue = "qfactory-v0"

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

	// Initialize PostgreSQL store
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://qfactory:qfactory@localhost:5432/qfactory?sslmode=disable"
	}

	ctx := context.Background()
	pgStore, err := store.NewPostgresStore(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pgStore.Close()

	// SSE hub for real-time notifications
	sseHub := store.NewSSEHub()

	evidenceSvc := NewEvidenceService(contracts.EvidenceDir)

	// Initialize search service
	qdrantURL := os.Getenv("QDRANT_URL")
	if qdrantURL == "" {
		qdrantURL = "http://localhost:6333"
	}

	searchSvc, err := NewSearchService(qdrantURL)
	if err != nil {
		log.Printf("Warning: Search service not available: %v", err)
		// Continue without search - it's optional for v0.3b
	}

	// Initialize model runtime
	modelProvider := os.Getenv("MODEL_PROVIDER")
	if modelProvider == "" {
		modelProvider = "ollama"
	}

	var modelRT modelruntime.Runtime
	switch modelProvider {
	case "mock":
		modelRT = modelruntime.NewMockRuntime()
		log.Printf("Using mock model runtime")
	case "ollama":
		cfg := modelruntime.LoadOllamaConfigFromEnv()
		modelRT = modelruntime.NewOllamaRuntime(cfg)
		log.Printf("Using Ollama model runtime at %s", cfg.URL)
	default:
		log.Printf("Unknown MODEL_PROVIDER=%s, defaulting to ollama", modelProvider)
		cfg := modelruntime.LoadOllamaConfigFromEnv()
		modelRT = modelruntime.NewOllamaRuntime(cfg)
	}

	budgetMgr := modelruntime.NewBudgetManager()
	budgetMgr.SetStore(pgStore)

	gateRunner := gates.NewGateRunner(pgStore, contracts.EvidenceDir)

	server := &Server{
		store:        pgStore,
		sseHub:       sseHub,
		temporal:     tc,
		evidenceSvc:  evidenceSvc,
		searchSvc:    searchSvc,
		modelRuntime: modelRT,
		budgetMgr:    budgetMgr,
		gateRunner:   gateRunner,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /workflows", server.handleCreateWorkflow)
	mux.HandleFunc("GET /runs", server.handleListRuns)
	mux.HandleFunc("GET /runs/{id}", server.handleGetRun)
	mux.HandleFunc("GET /runs/{id}/events", server.handleSSE)
	mux.HandleFunc("POST /runs/{id}/gates/pr1", server.handlePR1Gate)
	mux.HandleFunc("POST /runs/{id}/gates/pr2", server.handlePR2Gate)
	mux.HandleFunc("GET /runs/{id}/evidence", server.handleGetEvidence)
	mux.HandleFunc("GET /runs/{id}/evidence.zip", server.handleGetEvidenceZip)
	mux.HandleFunc("POST /internal/events", server.handleInternalEvent)

	// Search endpoints (v0.3b)
	mux.HandleFunc("POST /search/index", server.handleSearchIndex)
	mux.HandleFunc("GET /search", server.handleSearch)

	// Model runtime endpoints (v0.3c)
	mux.HandleFunc("GET /models/health", server.handleModelsHealth)
	mux.HandleFunc("POST /runs/{id}/model/complete", server.handleModelComplete)
	mux.HandleFunc("PATCH /runs/{id}/budget", server.handlePatchBudget)

	addr := ":8090"
	log.Printf("Control-plane API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Server holds dependencies for HTTP handlers.
type Server struct {
	store        store.Store
	sseHub       *store.SSEHub
	temporal     client.Client
	evidenceSvc  *EvidenceService
	searchSvc    *SearchService
	modelRuntime modelruntime.Runtime
	budgetMgr    *modelruntime.BudgetManager
	gateRunner   *gates.GateRunner
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

	ctx := r.Context()
	runID, err := generateRunID()
	if err != nil {
		runID = fmt.Sprintf("run-%d", time.Now().UnixNano())
	}
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

	if err := s.store.CreateRun(ctx, run); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create run: %v", err))
		return
	}

	// Start Temporal workflow
	opts := client.StartWorkflowOptions{
		ID:        "demo-" + runID,
		TaskQueue: TaskQueue,
	}

	we, err := s.temporal.ExecuteWorkflow(ctx, opts, workflow.DemoWorkflow, runID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to start workflow: %v", err))
		return
	}

	run.TemporalID = we.GetID()
	run.UpdatedAt = time.Now().UTC()

	// Update run with temporal ID
	if err := s.store.UpdateRun(ctx, run); err != nil {
		log.Printf("Warning: failed to update run with temporal ID: %v", err)
	}

	writeJSON(w, http.StatusCreated, run)
}

func (s *Server) handleGetRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	ctx := r.Context()
	run, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	writeJSON(w, http.StatusOK, run)
}

func (s *Server) handleListRuns(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	runs, err := s.store.ListRuns(ctx, 100)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list runs: %v", err))
		return
	}
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

	ctx := r.Context()
	_, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
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

	// Replay existing events from DB
	existingEvents, err := s.store.ListEvents(ctx, id, 1000)
	if err != nil {
		log.Printf("Warning: failed to list events for SSE replay: %v", err)
	}
	for _, event := range existingEvents {
		data, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	}
	flusher.Flush()

	// Subscribe to new events via in-memory hub
	ch := s.sseHub.Subscribe(id)
	defer s.sseHub.Unsubscribe(id, ch)

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

	ctx := r.Context()

	// Persist event and update run state in DB
	if err := s.store.AddEvent(ctx, event); err != nil {
		log.Printf("Warning: failed to persist event: %v", err)
	}

	// Notify SSE subscribers
	s.sseHub.Publish(event.RunID, event)

	// Append to evidence JSONL
	s.appendEventToEvidence(event)

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) handlePR1Gate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	ctx := r.Context()
	run, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
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
	gateCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	startTime := time.Now().UTC()
	cmd := exec.CommandContext(gateCtx, "go", "test", "./...")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	exitCode := 0
	if cmdErr != nil {
		if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if gateCtx.Err() == context.DeadlineExceeded {
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
	evts, err := s.store.ListEvents(ctx, id, 1000)
	if err != nil {
		log.Printf("Warning: failed to list events: %v", err)
	}
	if err := s.evidenceSvc.WriteEventsJSONL(id, evts); err != nil {
		log.Printf("Failed to write events.jsonl: %v", err)
	}

	// Build check result
	checkMsg := "all tests passed"
	if !passed {
		if gateCtx.Err() == context.DeadlineExceeded {
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

	if !passed && gateCtx.Err() == context.DeadlineExceeded {
		result.Error = "timeout exceeded"
	}

	// Add gate result to DB
	if err := s.store.AddGateResult(ctx, id, result); err != nil {
		log.Printf("Warning: failed to add gate result: %v", err)
	}

	// Refresh run for manifest
	run, _, err = s.store.GetRun(ctx, id)
	if err != nil {
		log.Printf("Warning: failed to refresh run: %v", err)
	}

	// Write manifest
	if run != nil {
		evts, _ = s.store.ListEvents(ctx, id, 1000)
		if err := s.evidenceSvc.WriteManifest(id, run, evts); err != nil {
			log.Printf("Failed to write manifest: %v", err)
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePR2Gate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	ctx := r.Context()
	run, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	// Reject if run is not completed (PR0/PR1 must have finished)
	if run.Status != contracts.RunStatusCompleted {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("run must be completed to run PR2 gate (current status: %s)", run.Status))
		return
	}

	// Execute PR2 gate
	gate := gates.NewIntegrationSmokeGate()
	result, err := s.gateRunner.RunGate(ctx, id, gate)
	if err != nil {
		// Gate runner already persisted failure evidence; return 500
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("gate execution failed: %v", err))
		return
	}

	// Update evidence files
	evts, err := s.store.ListEvents(ctx, id, 1000)
	if err != nil {
		log.Printf("Warning: failed to list events: %v", err)
	}
	if err := s.evidenceSvc.WriteEventsJSONL(id, evts); err != nil {
		log.Printf("Failed to write events.jsonl: %v", err)
	}

	// Refresh run and write manifest
	run, _, err = s.store.GetRun(ctx, id)
	if err != nil {
		log.Printf("Warning: failed to refresh run: %v", err)
	}
	if run != nil {
		if err := s.evidenceSvc.WriteManifest(id, run, evts); err != nil {
			log.Printf("Failed to write manifest: %v", err)
		}
	}

	if !result.Passed {
		writeJSON(w, http.StatusUnprocessableEntity, result)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetEvidence(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	ctx := r.Context()
	_, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
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

	ctx := r.Context()
	run, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
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
		evts, _ := s.store.ListEvents(ctx, id, 1000)
		if err := s.evidenceSvc.ZipEvidence(id, run, evts); err != nil {
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create zip: %v", err))
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", id))
	http.ServeFile(w, r, zipPath)
}

func generateRunID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// appendEventToEvidence writes an event to the evidence JSONL file.
func (s *Server) appendEventToEvidence(event events.Event) {
	evts, _ := s.store.ListEvents(context.Background(), event.RunID, 1000)
	if err := s.evidenceSvc.WriteEventsJSONL(event.RunID, evts); err != nil {
		log.Printf("Warning: failed to write events.jsonl: %v", err)
	}
}

// truncateString truncates a string to maxLen bytes.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
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

// WriteModelCallEvidence appends a model call to model_calls.jsonl.
func (e *EvidenceService) WriteModelCallEvidence(runID string, req contracts.CompletionRequest, resp *contracts.CompletionResponse, errMsg string) error {
	runDir := filepath.Join(e.baseDir, runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		return fmt.Errorf("failed to create run dir: %w", err)
	}

	// Redact: truncate prompt and system to 4KB each
	record := map[string]interface{}{
		"timestamp": time.Now().UTC(),
		"request": map[string]interface{}{
			"model":             req.Model,
			"prompt":            truncateString(req.Prompt, 4096),
			"system":            truncateString(req.System, 4096),
			"max_output_tokens": req.MaxOutputTokens,
			"temperature":       req.Temperature,
		},
		"response": resp,
		"error":    errMsg,
	}

	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("failed to marshal model call: %w", err)
	}

	f, err := os.OpenFile(filepath.Join(runDir, "model_calls.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open model_calls.jsonl: %w", err)
	}
	defer f.Close()

	f.Write(line)
	f.WriteString("\n")
	return nil
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

// handleSearchIndex handles POST /search/index
func (s *Server) handleSearchIndex(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if s.searchSvc == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "search service not available")
		return
	}

	var req IndexRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Path == "" {
		writeJSONError(w, http.StatusBadRequest, "path is required")
		return
	}

	resp, err := s.searchSvc.Index(req)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSearch handles GET /search
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if s.searchSvc == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "search service not available")
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSONError(w, http.StatusBadRequest, "query parameter 'q' is required")
		return
	}

	k := DefaultSearchK
	if kStr := r.URL.Query().Get("k"); kStr != "" {
		if kVal, err := strconv.Atoi(kStr); err == nil && kVal > 0 {
			k = kVal
		}
	}

	resp, err := s.searchSvc.Search(query, k)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// setCORSHeaders sets CORS headers for cross-origin requests.
func setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// handleModelsHealth handles GET /models/health
func (s *Server) handleModelsHealth(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	status, err := s.modelRuntime.Health(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, status)
}

// ModelCompleteRequest is the request body for model completion.
type ModelCompleteRequest struct {
	Model           string  `json:"model,omitempty"`
	Prompt          string  `json:"prompt"`
	System          string  `json:"system,omitempty"`
	MaxOutputTokens int     `json:"max_output_tokens,omitempty"`
	Temperature     float64 `json:"temperature,omitempty"`
}

// ModelCompleteResponse is the response for model completion.
type ModelCompleteResponse struct {
	OK       bool                          `json:"ok"`
	Response *contracts.CompletionResponse `json:"response,omitempty"`
	Budget   contracts.BudgetDecision      `json:"budget"`
	Error    string                        `json:"error,omitempty"`
}

// handleModelComplete handles POST /runs/{id}/model/complete
func (s *Server) handleModelComplete(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	ctx := r.Context()
	run, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	var req ModelCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Initialize budget state with run's policy
	s.budgetMgr.GetOrCreateState(id, run.BudgetPolicy)

	// Estimate request tokens (prompt + system chars / 4)
	estimatedTokens := (len(req.Prompt) + len(req.System)) / 4

	// Check budget before calling model
	decision := s.budgetMgr.CheckBudget(id, estimatedTokens)
	if !decision.Allowed {
		writeJSON(w, http.StatusTooManyRequests, ModelCompleteResponse{
			OK:     false,
			Budget: decision,
			Error:  decision.Reason,
		})
		return
	}

	// Build completion request
	completionReq := contracts.CompletionRequest{
		Model:           req.Model,
		Prompt:          req.Prompt,
		System:          req.System,
		MaxOutputTokens: req.MaxOutputTokens,
		Temperature:     req.Temperature,
	}

	// Set up context with timeout based on budget policy
	maxLatencyMs := s.budgetMgr.GetMaxLatencyMs(id)
	modelCtx, cancel := context.WithTimeout(ctx, time.Duration(maxLatencyMs)*time.Millisecond)
	defer cancel()

	// Call model runtime
	resp, modelErr := s.modelRuntime.Complete(modelCtx, completionReq)

	// Record model call summary
	callSummary := contracts.ModelCallSummary{
		Timestamp: time.Now().UTC(),
		Stage:     run.CurrentStage,
		OK:        modelErr == nil,
	}

	if resp != nil {
		callSummary.Provider = resp.Provider
		callSummary.Model = resp.Model
		callSummary.InputTokens = resp.Usage.InputTokens
		callSummary.OutputTokens = resp.Usage.OutputTokens
		callSummary.TotalTokens = resp.Usage.TotalTokens
		callSummary.LatencyMs = resp.LatencyMs

		// Record usage in budget manager
		s.budgetMgr.RecordUsage(id, resp.Usage.TotalTokens, resp.LatencyMs)
	}

	if modelErr != nil {
		callSummary.Error = modelErr.Error()
	}

	// Add model call to run in DB
	if addErr := s.store.AddModelCall(ctx, id, callSummary); addErr != nil {
		log.Printf("Warning: failed to add model call: %v", addErr)
	}

	// Write evidence
	errMsg := ""
	if modelErr != nil {
		errMsg = modelErr.Error()
	}
	if writeErr := s.evidenceSvc.WriteModelCallEvidence(id, completionReq, resp, errMsg); writeErr != nil {
		log.Printf("Failed to write model call evidence: %v", writeErr)
	}

	// Get updated budget decision
	updatedDecision := s.budgetMgr.CheckBudget(id, 0)

	if modelErr != nil {
		writeJSON(w, http.StatusBadGateway, ModelCompleteResponse{
			OK:     false,
			Budget: updatedDecision,
			Error:  modelErr.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, ModelCompleteResponse{
		OK:       true,
		Response: resp,
		Budget:   updatedDecision,
	})
}

// handlePatchBudget handles PATCH /runs/{id}/budget
func (s *Server) handlePatchBudget(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	id := r.PathValue("id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id")
		return
	}

	ctx := r.Context()
	run, ok, err := s.store.GetRun(ctx, id)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get run: %v", err))
		return
	}
	if !ok {
		writeJSONError(w, http.StatusNotFound, "run not found")
		return
	}

	var policy contracts.BudgetPolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate policy values
	if policy.MaxTotalTokensPerRun <= 0 {
		writeJSONError(w, http.StatusBadRequest, "max_total_tokens_per_run must be > 0")
		return
	}
	if policy.MaxTotalRequestsPerRun <= 0 {
		writeJSONError(w, http.StatusBadRequest, "max_total_requests_per_run must be > 0")
		return
	}
	if policy.MaxRequestTokens <= 0 {
		writeJSONError(w, http.StatusBadRequest, "max_request_tokens must be > 0")
		return
	}
	if policy.MaxLatencyMs <= 0 {
		writeJSONError(w, http.StatusBadRequest, "max_latency_ms must be > 0")
		return
	}

	// Update run and budget manager
	run.BudgetPolicy = &policy
	run.UpdatedAt = time.Now().UTC()
	if err := s.store.UpdateRun(ctx, run); err != nil {
		log.Printf("Warning: failed to update run: %v", err)
	}
	s.budgetMgr.SetPolicy(id, &policy)

	writeJSON(w, http.StatusOK, run)
}
