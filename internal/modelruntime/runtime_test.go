package modelruntime

import (
	"context"
	"testing"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

func TestMockRuntime_Complete(t *testing.T) {
	runtime := NewMockRuntime()

	req := contracts.CompletionRequest{
		Model:  "test-model",
		Prompt: "Hello, world!",
		System: "You are a test assistant.",
	}

	resp, err := runtime.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Text != "Mock response: Hello, world!" {
		t.Errorf("unexpected response text: %s", resp.Text)
	}

	if resp.Provider != "mock" {
		t.Errorf("unexpected provider: %s", resp.Provider)
	}

	if resp.Usage.TotalTokens <= 0 {
		t.Errorf("expected positive token count, got %d", resp.Usage.TotalTokens)
	}

	if !resp.Usage.Estimated {
		t.Error("mock runtime should mark usage as estimated")
	}
}

func TestMockRuntime_Health(t *testing.T) {
	runtime := NewMockRuntime()

	status, err := runtime.Health(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !status.OK {
		t.Error("mock runtime should always be healthy")
	}

	if status.Provider != "mock" {
		t.Errorf("unexpected provider: %s", status.Provider)
	}
}

func TestBudgetManager_CheckBudget(t *testing.T) {
	bm := NewBudgetManager()

	// Test with default policy
	runID := "test-run-1"
	bm.GetOrCreateState(runID, nil)

	decision := bm.CheckBudget(runID, 100)
	if !decision.Allowed {
		t.Errorf("expected allowed, got denied: %s", decision.Reason)
	}

	// Record some usage
	bm.RecordUsage(runID, 5000, 100)
	bm.RecordUsage(runID, 5000, 100)
	bm.RecordUsage(runID, 5000, 100)
	bm.RecordUsage(runID, 5000, 100)

	// Should still be allowed (20000 tokens used = limit)
	decision = bm.CheckBudget(runID, 100)
	if decision.Allowed {
		t.Error("expected denied after reaching token limit")
	}
	if decision.RemainingTokens != 0 {
		t.Errorf("expected 0 remaining tokens, got %d", decision.RemainingTokens)
	}
}

func TestBudgetManager_RequestLimit(t *testing.T) {
	bm := NewBudgetManager()

	runID := "test-run-2"
	policy := &contracts.BudgetPolicy{
		MaxTotalTokensPerRun:   100000,
		MaxTotalRequestsPerRun: 5,
		MaxRequestTokens:       1000,
		MaxLatencyMs:           30000,
		FailOpen:               false,
	}
	bm.GetOrCreateState(runID, policy)

	// Make 5 requests
	for i := 0; i < 5; i++ {
		bm.RecordUsage(runID, 100, 50)
	}

	decision := bm.CheckBudget(runID, 100)
	if decision.Allowed {
		t.Error("expected denied after reaching request limit")
	}
	if decision.RemainingRequests != 0 {
		t.Errorf("expected 0 remaining requests, got %d", decision.RemainingRequests)
	}
}

func TestBudgetManager_MaxRequestTokens(t *testing.T) {
	bm := NewBudgetManager()

	runID := "test-run-3"
	policy := &contracts.BudgetPolicy{
		MaxTotalTokensPerRun:   100000,
		MaxTotalRequestsPerRun: 50,
		MaxRequestTokens:       500,
		MaxLatencyMs:           30000,
		FailOpen:               false,
	}
	bm.GetOrCreateState(runID, policy)

	// Try a request that exceeds per-request limit
	decision := bm.CheckBudget(runID, 1000)
	if decision.Allowed {
		t.Error("expected denied for request exceeding per-request token limit")
	}
}

func TestBudgetManager_FailOpen(t *testing.T) {
	bm := NewBudgetManager()

	runID := "test-run-4"
	policy := &contracts.BudgetPolicy{
		MaxTotalTokensPerRun:   100,
		MaxTotalRequestsPerRun: 1,
		MaxRequestTokens:       50,
		MaxLatencyMs:           30000,
		FailOpen:               true, // Enable fail-open
	}
	bm.GetOrCreateState(runID, policy)

	// Exhaust requests
	bm.RecordUsage(runID, 50, 10)

	// With fail-open, should still be allowed
	decision := bm.CheckBudget(runID, 50)
	if !decision.Allowed {
		t.Error("expected allowed with fail_open=true even after limit")
	}
}
