package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AIfactory-hq/qfactory/internal/gates"
	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

func TestHealthEndpoint(t *testing.T) {
	runner := NewRunner(Config{RunnerName: "test-runner"})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	runner.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["ok"] != true {
		t.Error("expected ok=true")
	}
	if resp["runner"] != "test-runner" {
		t.Errorf("expected runner=test-runner, got %v", resp["runner"])
	}
}

func TestNotFoundEndpoint(t *testing.T) {
	runner := NewRunner(Config{RunnerName: "test-runner"})

	req := httptest.NewRequest("GET", "/unknown", nil)
	w := httptest.NewRecorder()

	runner.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestExecutePR1UnitTests(t *testing.T) {
	// Get current directory as repo root
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Navigate to repo root (two levels up from apps/gate-runner)
	repoRoot := cwd
	for i := 0; i < 2; i++ {
		idx := strings.LastIndex(repoRoot, "/")
		if idx > 0 {
			repoRoot = repoRoot[:idx]
		}
	}

	runner := NewRunner(Config{
		RunnerName: "test-runner",
		RepoRoot:   repoRoot,
	})

	execReq := gates.RemoteExecuteRequest{
		RunID:          "test-run-123",
		GateLevel:      contracts.GateLevelPR1,
		GateName:       "unit_tests",
		TimeoutSeconds: 120,
	}

	body, _ := json.Marshal(execReq)
	req := httptest.NewRequest("POST", "/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	runner.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp gates.RemoteExecuteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check command contains "go test"
	if !strings.Contains(resp.Command, "go test") {
		t.Errorf("expected command to contain 'go test', got %s", resp.Command)
	}

	// Check result fields
	if resp.Result.Level != contracts.GateLevelPR1 {
		t.Errorf("expected level PR1, got %s", resp.Result.Level)
	}
	if resp.Result.Name != "unit_tests" {
		t.Errorf("expected name unit_tests, got %s", resp.Result.Name)
	}
	if resp.Result.Executor != "remote:test-runner" {
		t.Errorf("expected executor remote:test-runner, got %s", resp.Result.Executor)
	}

	// Check that we have stdout (tests run)
	if resp.Stdout == "" {
		t.Error("expected non-empty stdout")
	}
}

func TestExecutePR2IntegrationSmoke(t *testing.T) {
	// Get current directory as repo root
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	// Navigate to repo root (two levels up from apps/gate-runner)
	repoRoot := cwd
	for i := 0; i < 2; i++ {
		idx := strings.LastIndex(repoRoot, "/")
		if idx > 0 {
			repoRoot = repoRoot[:idx]
		}
	}

	runner := NewRunner(Config{
		RunnerName: "test-runner",
		RepoRoot:   repoRoot,
	})

	execReq := gates.RemoteExecuteRequest{
		RunID:          "test-run-456",
		GateLevel:      contracts.GateLevelPR2,
		GateName:       "integration_smoke",
		TimeoutSeconds: 120,
	}

	body, _ := json.Marshal(execReq)
	req := httptest.NewRequest("POST", "/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	runner.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp gates.RemoteExecuteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Check result structure
	if resp.Result.Level != contracts.GateLevelPR2 {
		t.Errorf("expected level PR2, got %s", resp.Result.Level)
	}
	if resp.Result.Name != "integration_smoke" {
		t.Errorf("expected name integration_smoke, got %s", resp.Result.Name)
	}
	if resp.Result.Executor != "remote:test-runner" {
		t.Errorf("expected executor remote:test-runner, got %s", resp.Result.Executor)
	}

	// Check we have at least one check
	if len(resp.Result.Checks) == 0 {
		t.Error("expected at least one check")
	}

	// Command should mention integration test pattern
	if !strings.Contains(resp.Command, "TestIntegration") {
		t.Errorf("expected command to mention TestIntegration, got: %s", resp.Command)
	}
}

func TestExecuteUnknownGate(t *testing.T) {
	runner := NewRunner(Config{RunnerName: "test-runner"})

	execReq := gates.RemoteExecuteRequest{
		RunID:          "test-run-789",
		GateLevel:      "UNKNOWN",
		GateName:       "unknown_gate",
		TimeoutSeconds: 60,
	}

	body, _ := json.Marshal(execReq)
	req := httptest.NewRequest("POST", "/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	runner.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var resp gates.RemoteExecuteResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error == "" {
		t.Error("expected error message")
	}
	if !strings.Contains(resp.Error, "unsupported gate") {
		t.Errorf("expected error to mention unsupported gate, got: %s", resp.Error)
	}
}

func TestExecuteInvalidRequest(t *testing.T) {
	runner := NewRunner(Config{RunnerName: "test-runner"})

	req := httptest.NewRequest("POST", "/execute", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	runner.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestExecutorName(t *testing.T) {
	runner := NewRunner(Config{RunnerName: "my-runner"})
	expected := "remote:my-runner"
	if runner.ExecutorName() != expected {
		t.Errorf("expected %s, got %s", expected, runner.ExecutorName())
	}
}

func TestConfigDefaults(t *testing.T) {
	// Clear env vars
	os.Unsetenv("GATE_RUNNER_ADDR")
	os.Unsetenv("GATE_RUNNER_NAME")
	os.Unsetenv("GATE_RUNNER_REPO_ROOT")

	cfg := LoadConfig()

	if cfg.Addr != DefaultAddr {
		t.Errorf("expected default addr %s, got %s", DefaultAddr, cfg.Addr)
	}
	if cfg.RunnerName != DefaultRunnerName {
		t.Errorf("expected default runner name %s, got %s", DefaultRunnerName, cfg.RunnerName)
	}
}

func TestConfigFromEnv(t *testing.T) {
	os.Setenv("GATE_RUNNER_ADDR", ":9999")
	os.Setenv("GATE_RUNNER_NAME", "custom-runner")
	os.Setenv("GATE_RUNNER_REPO_ROOT", "/custom/path")
	defer func() {
		os.Unsetenv("GATE_RUNNER_ADDR")
		os.Unsetenv("GATE_RUNNER_NAME")
		os.Unsetenv("GATE_RUNNER_REPO_ROOT")
	}()

	cfg := LoadConfig()

	if cfg.Addr != ":9999" {
		t.Errorf("expected addr :9999, got %s", cfg.Addr)
	}
	if cfg.RunnerName != "custom-runner" {
		t.Errorf("expected runner name custom-runner, got %s", cfg.RunnerName)
	}
	if cfg.RepoRoot != "/custom/path" {
		t.Errorf("expected repo root /custom/path, got %s", cfg.RepoRoot)
	}
}
