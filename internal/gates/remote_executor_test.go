package gates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// mockRemoteGate implements Gate for testing remote executor.
type mockRemoteGate struct {
	level string
	name  string
}

func (g *mockRemoteGate) Level() string { return g.level }
func (g *mockRemoteGate) Name() string  { return g.name }
func (g *mockRemoteGate) Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error) {
	return GateOutput{}, nil
}

func TestRemoteExecutor_Success(t *testing.T) {
	// Create a mock remote runner server
	startTime := time.Now().UTC()
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/execute" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var req RemoteExecuteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		endTime := time.Now().UTC()
		response := RemoteExecuteResponse{
			Result: contracts.GateResult{
				Level:       req.GateLevel,
				Name:        req.GateName,
				Passed:      true,
				Executor:    "remote:mock-runner",
				Timestamp:   endTime,
				DurationMs:  100,
				StartedAt:   &startTime,
				CompletedAt: &endTime,
				Checks: []contracts.Check{
					{Name: "test_check", Passed: true, Message: "all good"},
				},
			},
			Command: "mock command",
			Stdout:  "mock stdout output",
			Stderr:  "",
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	executor := NewRemoteExecutor(mockServer.URL)

	run := &contracts.WorkflowRun{
		ID:     "test-run-remote",
		Status: contracts.RunStatusCompleted,
	}

	gate := &mockRemoteGate{level: contracts.GateLevelPR1, name: "unit_tests"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := executor.Execute(ctx, run, gate)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !output.Result.Passed {
		t.Error("expected gate to pass")
	}

	if output.Result.Level != contracts.GateLevelPR1 {
		t.Errorf("expected level PR1, got %s", output.Result.Level)
	}

	if output.Result.Name != "unit_tests" {
		t.Errorf("expected name unit_tests, got %s", output.Result.Name)
	}

	// RemoteExecutor should override executor name
	if output.Result.Executor != "remote" {
		t.Errorf("expected executor 'remote', got %s", output.Result.Executor)
	}

	if output.Command != "mock command" {
		t.Errorf("expected command 'mock command', got %s", output.Command)
	}

	if output.Stdout != "mock stdout output" {
		t.Errorf("expected stdout 'mock stdout output', got %s", output.Stdout)
	}
}

func TestRemoteExecutor_ServerError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer mockServer.Close()

	executor := NewRemoteExecutor(mockServer.URL)

	run := &contracts.WorkflowRun{
		ID:     "test-run-error",
		Status: contracts.RunStatusCompleted,
	}

	gate := &mockRemoteGate{level: contracts.GateLevelPR1, name: "unit_tests"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := executor.Execute(ctx, run, gate)

	if err == nil {
		t.Error("expected error for server error response")
	}
}

func TestRemoteExecutor_RemoteError(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RemoteExecuteResponse{
			Error: "gate execution failed",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	executor := NewRemoteExecutor(mockServer.URL)

	run := &contracts.WorkflowRun{
		ID:     "test-run-remote-error",
		Status: contracts.RunStatusCompleted,
	}

	gate := &mockRemoteGate{level: contracts.GateLevelPR1, name: "unit_tests"}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := executor.Execute(ctx, run, gate)

	if err == nil {
		t.Error("expected error for remote error response")
	}
}

func TestRemoteExecutor_EmptyGateName(t *testing.T) {
	var receivedReq RemoteExecuteRequest

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		startTime := time.Now().UTC()
		response := RemoteExecuteResponse{
			Result: contracts.GateResult{
				Level:       receivedReq.GateLevel,
				Name:        receivedReq.GateName,
				Passed:      true,
				Executor:    "remote:mock",
				Timestamp:   startTime,
				StartedAt:   &startTime,
				CompletedAt: &startTime,
			},
			Command: "test",
			Stdout:  "",
			Stderr:  "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	executor := NewRemoteExecutor(mockServer.URL)

	run := &contracts.WorkflowRun{ID: "test"}

	// Gate with empty name
	gate := &mockRemoteGate{level: contracts.GateLevelPR1, name: ""}

	ctx := context.Background()
	executor.Execute(ctx, run, gate)

	// Should normalize to "default"
	if receivedReq.GateName != "default" {
		t.Errorf("expected gate name to be normalized to 'default', got '%s'", receivedReq.GateName)
	}
}

func TestRemoteExecutor_TimeoutFromContext(t *testing.T) {
	var receivedReq RemoteExecuteRequest

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)

		startTime := time.Now().UTC()
		response := RemoteExecuteResponse{
			Result: contracts.GateResult{
				Level:     receivedReq.GateLevel,
				Name:      receivedReq.GateName,
				Passed:    true,
				Timestamp: startTime,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer mockServer.Close()

	executor := NewRemoteExecutor(mockServer.URL)

	run := &contracts.WorkflowRun{ID: "test"}
	gate := &mockRemoteGate{level: contracts.GateLevelPR1, name: "test"}

	// Set context with specific deadline
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	executor.Execute(ctx, run, gate)

	// Should pick up timeout from context (approximately 60 seconds)
	if receivedReq.TimeoutSeconds < 55 || receivedReq.TimeoutSeconds > 65 {
		t.Errorf("expected timeout around 60s, got %d", receivedReq.TimeoutSeconds)
	}
}
