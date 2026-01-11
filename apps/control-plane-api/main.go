// Package main implements the control-plane API for qfactory.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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
	"github.com/AIfactory-hq/qfactory/internal/capsules"
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

	// Initialize capsule signer (v0.9)
	capsuleSigner, err := capsules.NewSigner()
	if err != nil {
		log.Fatalf("Failed to initialize capsule signer: %v", err)
	}
	log.Printf("Capsule signer initialized (fingerprint: %s)", capsuleSigner.PublicKeyFingerprint()[:16]+"...")

	capsuleExporter := capsules.NewExporter(capsuleSigner, contracts.EvidenceDir)

	server := &Server{
		store:           pgStore,
		sseHub:          sseHub,
		temporal:        tc,
		evidenceSvc:     evidenceSvc,
		searchSvc:       searchSvc,
		modelRuntime:    modelRT,
		budgetMgr:       budgetMgr,
		gateRunner:      gateRunner,
		capsuleSigner:   capsuleSigner,
		capsuleExporter: capsuleExporter,
		opLimits:        contracts.DefaultOperationalLimits(), // v1.0
	}

	// Wire up gate runner with event publisher
	gateRunner.SetEventPublisher(server)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /workflows", server.handleCreateWorkflow)
	mux.HandleFunc("GET /runs", server.handleListRuns)
	mux.HandleFunc("GET /runs/{id}", server.handleGetRun)
	mux.HandleFunc("GET /runs/{id}/events", server.handleSSE)
	mux.HandleFunc("POST /runs/{id}/gates/pr1", server.handlePR1Gate)
	mux.HandleFunc("POST /runs/{id}/gates/pr2", server.handlePR2Gate)
	mux.HandleFunc("POST /runs/{id}/gates/pr3", server.handlePR3Gate)
	mux.HandleFunc("GET /runs/{id}/gates/{level}/{name}/evidence.zip", server.handleGetGateEvidenceZip)
	mux.HandleFunc("GET /runs/{id}/gates/{level}/{name}/files", server.handleGetGateFiles)
	mux.HandleFunc("GET /runs/{id}/gates/{level}/{name}/exec/{execId}/evidence.zip", server.handleGetExecEvidenceZip)
	mux.HandleFunc("GET /runs/{id}/gates/{level}/{name}/exec/{execId}/files", server.handleGetExecFiles)
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

	// Gate policy and trust endpoints (v0.6)
	mux.HandleFunc("POST /runs/{id}/gates/{level}/{name}/run", server.handleRunGate)
	mux.HandleFunc("PATCH /runs/{id}/gate-policy", server.handlePatchGatePolicy)
	mux.HandleFunc("GET /runs/{id}/gate-policy/decision", server.handleGetPolicyDecision)
	mux.HandleFunc("GET /runs/{id}/trust", server.handleGetTrustIndex)

	// Gate lineage endpoints (v0.8)
	mux.HandleFunc("POST /runs/{id}/gates/{level}/{name}/retry", server.handleRetryGate)
	mux.HandleFunc("GET /runs/{id}/gates/{level}/{name}/diff", server.handleGateDiff)
	mux.HandleFunc("GET /runs/{id}/gates/{level}/{name}/latest", server.handleGetLatestGate)

	// Finalization and capsule endpoints (v0.9)
	mux.HandleFunc("POST /runs/{id}/finalize", server.handleFinalizeRun)
	mux.HandleFunc("GET /runs/{id}/capsule", server.handleGetCapsule)
	mux.HandleFunc("GET /runs/{id}/capsule.zip", server.handleGetCapsuleZip)
	mux.HandleFunc("POST /capsules/verify", server.handleVerifyCapsule)

	addr := ":8090"
	log.Printf("Control-plane API listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Server holds dependencies for HTTP handlers.
type Server struct {
	store           store.Store
	sseHub          *store.SSEHub
	temporal        client.Client
	evidenceSvc     *EvidenceService
	searchSvc       *SearchService
	modelRuntime    modelruntime.Runtime
	budgetMgr       *modelruntime.BudgetManager
	gateRunner      *gates.GateRunner
	capsuleSigner   *capsules.Signer            // v0.9
	capsuleExporter *capsules.Exporter          // v0.9
	opLimits        contracts.OperationalLimits // v1.0
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

// checkNotFinalized returns true if the handler should abort (run is finalized).
// Writes 409 Conflict if the run is finalized.
func checkNotFinalized(w http.ResponseWriter, run *contracts.WorkflowRun) bool {
	if run.IsFinalized() {
		writeAPIError(w, http.StatusConflict, contracts.ErrCodeAlreadyFinalized, "run is finalized and cannot be modified", "")
		return true
	}
	return false
}

// writeAPIError writes a structured API error response (v1.0).
func writeAPIError(w http.ResponseWriter, httpCode int, code contracts.ErrorCode, message, details string) {
	writeJSON(w, httpCode, contracts.APIError{
		Code:    code,
		Message: message,
		Details: details,
	})
}

// v1.0: RBAC middleware helpers

// extractRequestContext extracts tenant/role info from request headers.
// Headers: X-Tenant-ID, X-Project-ID, X-Role, X-User-ID
func extractRequestContext(r *http.Request) contracts.RequestContext {
	return contracts.RequestContext{
		TenantID:  r.Header.Get("X-Tenant-ID"),
		ProjectID: r.Header.Get("X-Project-ID"),
		Role:      contracts.Role(r.Header.Get("X-Role")),
		UserID:    r.Header.Get("X-User-ID"),
	}
}

// checkPermission returns true if the handler should abort (insufficient permission).
// Writes 403 Forbidden with structured error if permission denied.
func checkPermission(w http.ResponseWriter, reqCtx contracts.RequestContext, required contracts.Permission) bool {
	// Default role is viewer if not specified
	role := reqCtx.Role
	if role == "" {
		role = contracts.RoleViewer
	}

	if !contracts.RoleHasPermission(role, required) {
		writeAPIError(w, http.StatusForbidden, contracts.ErrCodeForbiddenRole,
			fmt.Sprintf("role '%s' does not have permission '%s'", role, required),
			fmt.Sprintf("required_permission=%s", required))
		return true
	}
	return false
}

// checkTenantAccess returns true if the handler should abort (tenant mismatch).
// Writes 403 Forbidden with structured error if tenant doesn't match.
func checkTenantAccess(w http.ResponseWriter, reqCtx contracts.RequestContext, run *contracts.WorkflowRun) bool {
	// Skip check if request has no tenant specified (backward compat)
	if reqCtx.TenantID == "" {
		return false
	}

	// Skip check if run has no tenant (legacy runs)
	if run.TenantID == "" {
		return false
	}

	if reqCtx.TenantID != run.TenantID {
		writeAPIError(w, http.StatusForbidden, contracts.ErrCodeTenantMismatch,
			"access denied: tenant mismatch",
			fmt.Sprintf("request_tenant=%s, run_tenant=%s", reqCtx.TenantID, run.TenantID))
		return true
	}
	return false
}

// checkGatesNotRunning returns true if the handler should abort (gates are running).
// Writes 409 Conflict with structured error if gates are running.
func checkGatesNotRunning(w http.ResponseWriter, run *contracts.WorkflowRun) bool {
	if run.GatesRunning {
		writeAPIError(w, http.StatusConflict, contracts.ErrCodeGatesRunning,
			"cannot proceed: gates are currently running",
			"wait for gates to complete before retrying")
		return true
	}
	return false
}

// PublishEvent implements gates.EventPublisher interface.
func (s *Server) PublishEvent(ctx context.Context, event events.Event) error {
	// Persist to DB
	if err := s.store.AddEvent(ctx, event); err != nil {
		log.Printf("Warning: failed to persist event: %v", err)
	}

	// Notify SSE subscribers
	s.sseHub.Publish(event.RunID, event)

	// Append to evidence JSONL
	s.appendEventToEvidence(event)

	return nil
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

	// Optionally include gate history
	if r.URL.Query().Get("include_history") == "1" {
		history, err := s.store.ListGateHistory(ctx, id, 100)
		if err != nil {
			log.Printf("Warning: failed to get gate history: %v", err)
		} else {
			run.GateHistory = history
		}
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

	// Check if run is finalized (v0.9)
	finalized, err := s.store.IsRunFinalized(ctx, event.RunID)
	if err != nil {
		log.Printf("Warning: failed to check if run is finalized: %v", err)
	}
	if finalized {
		writeJSONError(w, http.StatusConflict, "run is finalized and cannot be modified")
		return
	}

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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
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

	startTime := time.Now().UTC()

	// Determine workspace directory for this run
	// Priority: 1) evidence/{id}/workspace, 2) QF_WORKSPACE_ROOT/{id}, 3) current directory
	workspaceDir := filepath.Join(contracts.EvidenceDir, id, "workspace")
	if envRoot := os.Getenv("QF_WORKSPACE_ROOT"); envRoot != "" {
		workspaceDir = filepath.Join(envRoot, id)
	}

	var stdout, stderr bytes.Buffer
	var cmdErr error
	var exitCode int
	var passed bool
	var checkMsg string
	var testCommand string

	// Check if workspace exists and has Go files
	hasGoFiles := false
	if info, err := os.Stat(workspaceDir); err == nil && info.IsDir() {
		// Check for Go files in workspace
		entries, _ := os.ReadDir(workspaceDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				hasGoFiles = true
				break
			}
		}
		// Also check for go.mod to indicate a Go module
		if !hasGoFiles {
			if _, err := os.Stat(filepath.Join(workspaceDir, "go.mod")); err == nil {
				hasGoFiles = true
			}
		}
	}

	if hasGoFiles {
		// Run tests in the workspace directory
		testCommand = fmt.Sprintf("go test ./... (in %s)", workspaceDir)
		gateCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()

		cmd := exec.CommandContext(gateCtx, "go", "test", "./...")
		cmd.Dir = workspaceDir
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		cmdErr = cmd.Run()

		if cmdErr != nil {
			if exitErr, ok := cmdErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else if gateCtx.Err() == context.DeadlineExceeded {
				exitCode = -1 // Timeout
			} else {
				exitCode = -2 // Other error
			}
		}

		passed = exitCode == 0
		if passed {
			checkMsg = "all tests passed"
		} else if gateCtx.Err() == context.DeadlineExceeded {
			checkMsg = fmt.Sprintf("timeout after %ds", timeoutSec)
		} else {
			checkMsg = fmt.Sprintf("tests failed with exit code %d", exitCode)
		}
	} else {
		// No workspace or no Go files - pass with informational message
		testCommand = "no tests (no workspace)"
		passed = true
		exitCode = 0
		checkMsg = fmt.Sprintf("no Go files found in workspace (%s) - skipped", workspaceDir)
		stdout.WriteString("PR1 gate: No workspace directory or Go files found.\n")
		stdout.WriteString(fmt.Sprintf("Expected workspace at: %s\n", workspaceDir))
		stdout.WriteString("This is normal for stub workflows that don't generate code.\n")
		stdout.WriteString("Gate passed (no tests to run).\n")
	}

	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	// Build evidence
	evidence := contracts.GateEvidence{
		Level:      contracts.GateLevelPR1,
		Command:    testCommand,
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

	if !passed && exitCode == -1 {
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
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

func (s *Server) handlePR3Gate(w http.ResponseWriter, r *http.Request) {
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
		return
	}

	// Reject if run is not completed
	if run.Status != contracts.RunStatusCompleted {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("run must be completed to run PR3 gate (current status: %s)", run.Status))
		return
	}

	// Execute PR3 gate
	gate := gates.NewSecurityScanGate()
	result, err := s.gateRunner.RunGate(ctx, id, gate)
	if err != nil {
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

func (s *Server) handleGetGateEvidenceZip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")

	if id == "" || level == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, or name")
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

	gateDir := filepath.Join(contracts.EvidenceDir, id, "gates", level, name)
	if _, err := os.Stat(gateDir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "gate evidence not found")
		return
	}

	// Create zip in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	err = filepath.Walk(gateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(gateDir, path)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.Join(level, name, relPath)
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
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create zip: %v", err))
		return
	}

	if err := zipWriter.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to finalize zip: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s-%s.zip\"", id, level, name))
	w.Write(buf.Bytes())
}

func (s *Server) handleGetGateFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")

	if id == "" || level == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, or name")
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

	gateDir := filepath.Join(contracts.EvidenceDir, id, "gates", level, name)
	if _, err := os.Stat(gateDir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "gate evidence not found")
		return
	}

	type fileInfo struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	var files []fileInfo
	err = filepath.Walk(gateDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(gateDir, path)
		files = append(files, fileInfo{Name: relPath, Size: info.Size()})
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list files: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run_id": id,
		"level":  level,
		"name":   name,
		"files":  files,
	})
}

func (s *Server) handleGetExecEvidenceZip(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")
	execId := r.PathValue("execId")

	if id == "" || level == "" || name == "" || execId == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, name, or execution id")
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

	// Path with execution ID: evidence/<runID>/gates/<level>/<name>/<execID>/
	execDir := filepath.Join(contracts.EvidenceDir, id, "gates", level, name, execId)
	if _, err := os.Stat(execDir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "execution evidence not found")
		return
	}

	// Create zip in memory
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	err = filepath.Walk(execDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(execDir, path)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.Join(level, name, execId, relPath)
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
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create zip: %v", err))
		return
	}

	if err := zipWriter.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to finalize zip: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s-%s-%s.zip\"", id, level, name, execId))
	w.Write(buf.Bytes())
}

func (s *Server) handleGetExecFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")
	execId := r.PathValue("execId")

	if id == "" || level == "" || name == "" || execId == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, name, or execution id")
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

	// Path with execution ID: evidence/<runID>/gates/<level>/<name>/<execID>/
	execDir := filepath.Join(contracts.EvidenceDir, id, "gates", level, name, execId)
	if _, err := os.Stat(execDir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "execution evidence not found")
		return
	}

	type fileInfo struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}

	var files []fileInfo
	err = filepath.Walk(execDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(execDir, path)
		files = append(files, fileInfo{Name: relPath, Size: info.Size()})
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list files: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"run_id":       id,
		"level":        level,
		"name":         name,
		"execution_id": execId,
		"files":        files,
	})
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
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

// RunGateRequest is the request body for running a gate.
type RunGateRequest struct {
	Executor       string `json:"executor,omitempty"`        // "local" or "remote"
	RunnerURL      string `json:"runner_url,omitempty"`      // required if executor is "remote"
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"` // optional timeout
}

// handleRunGate handles POST /runs/{id}/gates/{level}/{name}/run
func (s *Server) handleRunGate(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")

	if id == "" || level == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, or name")
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
		return
	}

	// v1.0: RBAC check - require run_gates permission
	reqCtx := extractRequestContext(r)
	if checkPermission(w, reqCtx, contracts.PermissionRunGates) {
		return
	}
	if checkTenantAccess(w, reqCtx, run) {
		return
	}

	// Reject if run is not completed
	if run.Status != contracts.RunStatusCompleted {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("run must be completed to run gate (current status: %s)", run.Status))
		return
	}

	// Parse request body
	var req RunGateRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Create gate based on level
	var gate gates.Gate
	switch level {
	case contracts.GateLevelPR1:
		gate = gates.NewUnitTestGate()
	case contracts.GateLevelPR2:
		gate = gates.NewIntegrationSmokeGate()
	case contracts.GateLevelPR3:
		gate = gates.NewSecurityScanGate()
	default:
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unsupported gate level: %s", level))
		return
	}

	// Create executor based on request
	var executor gates.GateExecutor
	if req.Executor == "remote" {
		if req.RunnerURL == "" {
			writeJSONError(w, http.StatusBadRequest, "runner_url is required for remote executor")
			return
		}
		executor = gates.NewRemoteExecutor(req.RunnerURL)
	} else {
		executor = gates.NewLocalExecutor()
	}

	// Set up context with timeout (v1.0: use operational limits)
	timeoutSeconds := s.opLimits.MaxGateRuntimeSeconds
	if req.TimeoutSeconds > 0 && req.TimeoutSeconds < timeoutSeconds {
		// Allow shorter timeouts, but not longer than the limit
		timeoutSeconds = req.TimeoutSeconds
	}
	gateCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// v1.0: Set gates_running flag for finalization safety
	if err := s.store.SetGatesRunning(ctx, id, true); err != nil {
		log.Printf("Warning: failed to set gates_running: %v", err)
	}
	defer func() {
		if err := s.store.SetGatesRunning(ctx, id, false); err != nil {
			log.Printf("Warning: failed to clear gates_running: %v", err)
		}
	}()

	// Execute gate with executor
	result, err := s.gateRunner.RunGateWithExecutor(gateCtx, id, gate, executor)
	if err != nil {
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

// handlePatchGatePolicy handles PATCH /runs/{id}/gate-policy
func (s *Server) handlePatchGatePolicy(w http.ResponseWriter, r *http.Request) {
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
		return
	}

	// v1.0: RBAC check - require manage_policy permission (admin only)
	reqCtx := extractRequestContext(r)
	if checkPermission(w, reqCtx, contracts.PermissionManagePolicy) {
		return
	}
	if checkTenantAccess(w, reqCtx, run) {
		return
	}

	var policy contracts.GatePolicy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Update gate policy in store
	if err := s.store.UpdateGatePolicy(ctx, id, &policy); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update gate policy: %v", err))
		return
	}

	// Return updated run
	run, _, _ = s.store.GetRun(ctx, id)
	writeJSON(w, http.StatusOK, run)
}

// handleGetPolicyDecision handles GET /runs/{id}/gate-policy/decision
func (s *Server) handleGetPolicyDecision(w http.ResponseWriter, r *http.Request) {
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

	// Evaluate policy
	evaluator := gates.NewPolicyEvaluator()
	decision := evaluator.Evaluate(run.GatePolicy, run.Gates)

	writeJSON(w, http.StatusOK, decision)
}

// handleGetTrustIndex handles GET /runs/{id}/trust
func (s *Server) handleGetTrustIndex(w http.ResponseWriter, r *http.Request) {
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

	// Get gate history for enhanced trust calculation
	history, _ := s.store.ListGateHistory(ctx, id, 100)

	// Calculate trust index with history
	calculator := gates.NewTrustCalculator()
	trustIndex := calculator.CalculateWithHistory(run.GatePolicy, run.Gates, history)

	writeJSON(w, http.StatusOK, trustIndex)
}

// RetryGateRequest is the request body for retrying a gate.
type RetryGateRequest struct {
	Reason         string `json:"reason,omitempty"`          // why retry was requested
	Executor       string `json:"executor,omitempty"`        // "local" or "remote"
	RunnerURL      string `json:"runner_url,omitempty"`      // required if executor is "remote"
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"` // optional timeout
}

// handleRetryGate handles POST /runs/{id}/gates/{level}/{name}/retry
func (s *Server) handleRetryGate(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")

	if id == "" || level == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, or name")
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

	// Reject if finalized (v0.9)
	if checkNotFinalized(w, run) {
		return
	}

	// v1.0: RBAC check - require run_gates permission
	reqCtx := extractRequestContext(r)
	if checkPermission(w, reqCtx, contracts.PermissionRunGates) {
		return
	}
	if checkTenantAccess(w, reqCtx, run) {
		return
	}

	// Reject if run is not completed
	if run.Status != contracts.RunStatusCompleted {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("run must be completed to retry gate (current status: %s)", run.Status))
		return
	}

	// Parse request body
	var req RetryGateRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Normalize gate name
	name = contracts.NormalizeGateName(name)

	// v1.0: Check retry limit
	history, _ := s.store.ListGateHistory(ctx, id, 100)
	retryCount := 0
	for _, item := range history {
		itemName := contracts.NormalizeGateName(item.Result.Name)
		if item.Result.Level == level && itemName == name {
			retryCount++
		}
	}

	// Get max retries from policy or operational limits
	maxRetries := s.opLimits.MaxRetriesPerGate
	if run.GatePolicy != nil && run.GatePolicy.MaxRetries > 0 {
		maxRetries = run.GatePolicy.MaxRetries
	}

	if retryCount >= maxRetries {
		writeAPIError(w, http.StatusTooManyRequests, contracts.ErrCodeLimitExceeded,
			fmt.Sprintf("retry limit exceeded: %d retries for gate %s/%s", retryCount, level, name),
			fmt.Sprintf("max_retries=%d", maxRetries))
		return
	}

	// Set up context with timeout (v1.0: use operational limits)
	timeoutSeconds := s.opLimits.MaxGateRuntimeSeconds
	if req.TimeoutSeconds > 0 && req.TimeoutSeconds < timeoutSeconds {
		timeoutSeconds = req.TimeoutSeconds
	}
	gateCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// v1.0: Set gates_running flag for finalization safety
	if err := s.store.SetGatesRunning(ctx, id, true); err != nil {
		log.Printf("Warning: failed to set gates_running: %v", err)
	}
	defer func() {
		if err := s.store.SetGatesRunning(ctx, id, false); err != nil {
			log.Printf("Warning: failed to clear gates_running: %v", err)
		}
	}()

	// Execute retry using GateRunner
	result, err := s.gateRunner.RetryGate(gateCtx, id, level, name, req.Reason)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("gate retry failed: %v", err))
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

// GateDiffResponse represents the diff between two gate executions.
type GateDiffResponse struct {
	RunID      string          `json:"run_id"`
	Level      string          `json:"level"`
	Name       string          `json:"name"`
	ExecA      string          `json:"exec_a"`
	ExecB      string          `json:"exec_b"`
	FileDiffs  []FileDiff      `json:"file_diffs"`
	ResultDiff *GateResultDiff `json:"result_diff,omitempty"`
}

// FileDiff describes the difference between two files.
type FileDiff struct {
	Name     string `json:"name"`
	SHA256A  string `json:"sha256_a,omitempty"`
	SHA256B  string `json:"sha256_b,omitempty"`
	SizeA    int64  `json:"size_a"`
	SizeB    int64  `json:"size_b"`
	Modified bool   `json:"modified"`
}

// GateResultDiff shows differences in gate results.
type GateResultDiff struct {
	PassedA    bool  `json:"passed_a"`
	PassedB    bool  `json:"passed_b"`
	DurationA  int64 `json:"duration_ms_a"`
	DurationB  int64 `json:"duration_ms_b"`
	ChecksPass []int `json:"checks_pass_diff,omitempty"` // indices of checks that changed
}

// handleGateDiff handles GET /runs/{id}/gates/{level}/{name}/diff
func (s *Server) handleGateDiff(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")

	if id == "" || level == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, or name")
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

	// Get exec_a and exec_b from query params
	execA := r.URL.Query().Get("exec_a")
	execB := r.URL.Query().Get("exec_b")

	if execA == "" || execB == "" {
		writeJSONError(w, http.StatusBadRequest, "exec_a and exec_b query parameters are required")
		return
	}

	name = contracts.NormalizeGateName(name)

	// Build evidence paths
	execADir := filepath.Join(contracts.EvidenceDir, id, "gates", level, name, execA)
	execBDir := filepath.Join(contracts.EvidenceDir, id, "gates", level, name, execB)

	// Check both directories exist
	if _, err := os.Stat(execADir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("execution evidence not found: %s", execA))
		return
	}
	if _, err := os.Stat(execBDir); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("execution evidence not found: %s", execB))
		return
	}

	// Compare files
	fileDiffs, err := compareEvidenceDirs(execADir, execBDir)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to compare evidence: %v", err))
		return
	}

	// Compare results
	resultDiff, err := compareGateResults(execADir, execBDir)
	if err != nil {
		log.Printf("Warning: failed to compare gate results: %v", err)
	}

	resp := GateDiffResponse{
		RunID:      id,
		Level:      level,
		Name:       name,
		ExecA:      execA,
		ExecB:      execB,
		FileDiffs:  fileDiffs,
		ResultDiff: resultDiff,
	}

	writeJSON(w, http.StatusOK, resp)
}

// compareEvidenceDirs compares files in two evidence directories.
func compareEvidenceDirs(dirA, dirB string) ([]FileDiff, error) {
	filesA := make(map[string]os.FileInfo)
	filesB := make(map[string]os.FileInfo)

	// Collect files from dirA
	filepath.Walk(dirA, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(dirA, path)
		filesA[relPath] = info
		return nil
	})

	// Collect files from dirB
	filepath.Walk(dirB, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(dirB, path)
		filesB[relPath] = info
		return nil
	})

	// Build diff list
	allFiles := make(map[string]bool)
	for f := range filesA {
		allFiles[f] = true
	}
	for f := range filesB {
		allFiles[f] = true
	}

	var diffs []FileDiff
	for name := range allFiles {
		infoA, hasA := filesA[name]
		infoB, hasB := filesB[name]

		diff := FileDiff{Name: name}

		if hasA {
			diff.SizeA = infoA.Size()
			diff.SHA256A = computeFileSHA256(filepath.Join(dirA, name))
		}
		if hasB {
			diff.SizeB = infoB.Size()
			diff.SHA256B = computeFileSHA256(filepath.Join(dirB, name))
		}

		diff.Modified = diff.SHA256A != diff.SHA256B
		diffs = append(diffs, diff)
	}

	return diffs, nil
}

// computeFileSHA256 computes the SHA256 hash of a file.
func computeFileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// compareGateResults compares result.json from two executions.
func compareGateResults(dirA, dirB string) (*GateResultDiff, error) {
	resultA, err := loadGateResult(filepath.Join(dirA, "result.json"))
	if err != nil {
		return nil, err
	}
	resultB, err := loadGateResult(filepath.Join(dirB, "result.json"))
	if err != nil {
		return nil, err
	}

	diff := &GateResultDiff{
		PassedA:   resultA.Passed,
		PassedB:   resultB.Passed,
		DurationA: resultA.DurationMs,
		DurationB: resultB.DurationMs,
	}

	// Compare checks
	minChecks := len(resultA.Checks)
	if len(resultB.Checks) < minChecks {
		minChecks = len(resultB.Checks)
	}
	for i := 0; i < minChecks; i++ {
		if resultA.Checks[i].Passed != resultB.Checks[i].Passed {
			diff.ChecksPass = append(diff.ChecksPass, i)
		}
	}

	return diff, nil
}

// loadGateResult loads a gate result from result.json.
func loadGateResult(path string) (*contracts.GateResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var result contracts.GateResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// LatestGateResponse represents the latest gate execution info.
type LatestGateResponse struct {
	RunID       string                `json:"run_id"`
	Level       string                `json:"level"`
	Name        string                `json:"name"`
	ExecutionID string                `json:"execution_id"`
	Result      *contracts.GateResult `json:"result,omitempty"`
}

// handleGetLatestGate handles GET /runs/{id}/gates/{level}/{name}/latest
func (s *Server) handleGetLatestGate(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	id := r.PathValue("id")
	level := r.PathValue("level")
	name := r.PathValue("name")

	if id == "" || level == "" || name == "" {
		writeJSONError(w, http.StatusBadRequest, "missing run id, level, or name")
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

	name = contracts.NormalizeGateName(name)

	// Get latest execution ID
	execID, err := s.store.GetLatestExecution(ctx, id, level, name)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, fmt.Sprintf("no execution found for gate %s/%s", level, name))
		return
	}

	// Find the result in the run's gates
	var result *contracts.GateResult
	for _, gate := range run.Gates {
		if gate.Level == level && contracts.NormalizeGateName(gate.Name) == name {
			result = &gate
			break
		}
	}

	resp := LatestGateResponse{
		RunID:       id,
		Level:       level,
		Name:        name,
		ExecutionID: execID,
		Result:      result,
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleFinalizeRun handles POST /runs/{id}/finalize (v0.9)
func (s *Server) handleFinalizeRun(w http.ResponseWriter, r *http.Request) {
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

	// Check if already finalized (idempotent)
	if run.IsFinalized() {
		writeJSON(w, http.StatusOK, contracts.FinalizeResponse{
			RunID:              run.ID,
			Finalized:          true,
			CapsuleID:          run.CapsuleID,
			CapsulePath:        run.CapsulePath,
			ManifestSHA256:     run.CapsuleManifestSHA256,
			FinalizedAt:        run.FinalizedAt,
			FinalizedBy:        run.FinalizedBy,
			Override:           run.FinalizeOverride,
			OverrideReason:     run.FinalizeOverrideReason,
		})
		return
	}

	// v1.0: RBAC check - require finalize permission
	reqCtx := extractRequestContext(r)
	if checkPermission(w, reqCtx, contracts.PermissionFinalize) {
		return
	}
	if checkTenantAccess(w, reqCtx, run) {
		return
	}

	// Must be completed to finalize
	if run.Status != contracts.RunStatusCompleted {
		writeJSONError(w, http.StatusUnprocessableEntity, fmt.Sprintf("run must be completed to finalize (current status: %s)", run.Status))
		return
	}

	// v1.0: Check if gates are currently running
	if checkGatesNotRunning(w, run) {
		return
	}

	// Parse request body
	var req contracts.FinalizeRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Get gate history for trust calculation
	history, _ := s.store.ListGateHistory(ctx, id, 100)

	// Calculate trust index
	calculator := gates.NewTrustCalculator()
	trustIndex := calculator.CalculateWithHistory(run.GatePolicy, run.Gates, history)

	// Check trust threshold (v1.0: use policy's MinTrustScore if set)
	threshold := req.TrustThreshold
	if threshold == 0 {
		if run.GatePolicy != nil && run.GatePolicy.MinTrustScore > 0 {
			threshold = run.GatePolicy.MinTrustScore
		} else {
			threshold = contracts.DefaultTrustThreshold
		}
	}

	// v1.0: Check policy AllowOverride flag
	allowOverride := true
	if run.GatePolicy != nil {
		allowOverride = run.GatePolicy.AllowOverride
	}

	if trustIndex.Score < threshold && !req.Override {
		writeAPIError(w, http.StatusUnprocessableEntity, contracts.ErrCodePolicyViolation,
			fmt.Sprintf("trust score %d is below threshold %d; set override=true to proceed", trustIndex.Score, threshold), "")
		return
	}

	if req.Override && !allowOverride {
		writeAPIError(w, http.StatusForbidden, contracts.ErrCodePolicyViolation,
			"policy does not allow trust score override", "allow_override=false")
		return
	}

	if req.Override && req.OverrideReason == "" {
		writeJSONError(w, http.StatusBadRequest, "override_reason is required when override is true")
		return
	}

	// Get all events for the capsule
	evts, err := s.store.ListEvents(ctx, id, 10000)
	if err != nil {
		log.Printf("Warning: failed to list events: %v", err)
	}

	// Build export params
	finalizedAt := time.Now().UTC()
	finalizedBy := req.FinalizedBy
	if finalizedBy == "" {
		finalizedBy = "api"
	}

	exportParams := capsules.ExportParams{
		Run:         run,
		Events:      evts,
		Trust:       trustIndex,
		GateHistory: history,
		FinalizedAt: finalizedAt,
		FinalizedBy: finalizedBy,
		Override:    req.Override,
		OverrideReason: req.OverrideReason,
	}

	// Export capsule
	result, err := s.capsuleExporter.Export(exportParams)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to export capsule: %v", err))
		return
	}

	// Finalize run in DB
	finalizeParams := store.FinalizeParams{
		FinalizedAt:            finalizedAt,
		FinalizedBy:            finalizedBy,
		FinalizeReason:         req.Reason,
		FinalizeOverride:       req.Override,
		FinalizeOverrideReason: req.OverrideReason,
		CapsuleID:              result.CapsuleID,
		CapsulePath:            result.CapsulePath,
		CapsuleManifestSHA256:  result.ManifestSHA256,
	}

	if err := s.store.FinalizeRun(ctx, id, finalizeParams); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to finalize run: %v", err))
		return
	}

	// Emit run.finalized event
	finalizedPayload := events.FinalizedPayload{
		FinalizedAt:    finalizedAt.Format(time.RFC3339),
		FinalizedBy:    finalizedBy,
		CapsuleID:      result.CapsuleID,
		CapsulePath:    result.CapsulePath,
		ManifestSHA256: result.ManifestSHA256,
		TrustScore:     trustIndex.Score,
		TrustGrade:     trustIndex.Grade,
		Override:       req.Override,
		OverrideReason: req.OverrideReason,
	}
	evt := events.NewRunFinalizedEvent(id, finalizedPayload)
	s.PublishEvent(ctx, evt)

	writeJSON(w, http.StatusOK, contracts.FinalizeResponse{
		RunID:          id,
		Finalized:      true,
		CapsuleID:      result.CapsuleID,
		CapsulePath:    result.CapsulePath,
		ManifestSHA256: result.ManifestSHA256,
		FinalizedAt:    &finalizedAt,
		FinalizedBy:    finalizedBy,
		TrustScore:     trustIndex.Score,
		TrustGrade:     trustIndex.Grade,
		Override:       req.Override,
		OverrideReason: req.OverrideReason,
	})
}

// handleGetCapsule handles GET /runs/{id}/capsule (v0.9)
func (s *Server) handleGetCapsule(w http.ResponseWriter, r *http.Request) {
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

	if !run.IsFinalized() {
		writeJSONError(w, http.StatusNotFound, "run is not finalized")
		return
	}

	// Read capsule.json from the zip (staging is cleaned up after export)
	zipPath := run.CapsulePath
	if zipPath == "" {
		writeJSONError(w, http.StatusNotFound, "capsule not found")
		return
	}

	// Open the zip and read capsule.json
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to open capsule zip: %v", err))
		return
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name == "capsule.json" {
			rc, err := f.Open()
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "failed to read capsule.json")
				return
			}
			defer rc.Close()

			w.Header().Set("Content-Type", "application/json")
			io.Copy(w, rc)
			return
		}
	}

	writeJSONError(w, http.StatusNotFound, "capsule.json not found in archive")
}

// handleGetCapsuleZip handles GET /runs/{id}/capsule.zip (v0.9)
func (s *Server) handleGetCapsuleZip(w http.ResponseWriter, r *http.Request) {
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

	if !run.IsFinalized() {
		writeJSONError(w, http.StatusNotFound, "run is not finalized")
		return
	}

	if run.CapsulePath == "" {
		writeJSONError(w, http.StatusNotFound, "capsule not found")
		return
	}

	// Check if file exists
	if _, err := os.Stat(run.CapsulePath); os.IsNotExist(err) {
		writeJSONError(w, http.StatusNotFound, "capsule file not found")
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-capsule.zip\"", id))
	http.ServeFile(w, r, run.CapsulePath)
}

// handleVerifyCapsule handles POST /capsules/verify (v0.9)
func (s *Server) handleVerifyCapsule(w http.ResponseWriter, r *http.Request) {
	setCORSHeaders(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Accept multipart form with file upload
	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB max
		writeJSONError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}

	file, _, err := r.FormFile("capsule")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "capsule file is required")
		return
	}
	defer file.Close()

	// Read file into memory
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, file); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "failed to read capsule file")
		return
	}

	// Open as zip
	zipReader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid zip file")
		return
	}

	// Verify capsule
	verifier := capsules.NewVerifier()
	result, err := verifier.Verify(zipReader)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("verification failed: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, result)
}
