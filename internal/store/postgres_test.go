package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// TestStagesJSONSerialization tests stages JSON round-trip.
func TestStagesJSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	stages := []contracts.StageStatus{
		{Name: "spec", State: contracts.StageStateCompleted, StartedAt: &now, CompletedAt: &now},
		{Name: "contract", State: contracts.StageStateRunning, StartedAt: &now},
		{Name: "plan", State: contracts.StageStatePending},
	}

	data, err := json.Marshal(stages)
	if err != nil {
		t.Fatalf("failed to marshal stages: %v", err)
	}

	var decoded []contracts.StageStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal stages: %v", err)
	}

	if len(decoded) != len(stages) {
		t.Errorf("expected %d stages, got %d", len(stages), len(decoded))
	}

	if decoded[0].Name != "spec" || decoded[0].State != contracts.StageStateCompleted {
		t.Errorf("stage 0 mismatch: %+v", decoded[0])
	}
	if decoded[1].Name != "contract" || decoded[1].State != contracts.StageStateRunning {
		t.Errorf("stage 1 mismatch: %+v", decoded[1])
	}
}

// TestGatesJSONSerialization tests gates JSON round-trip.
func TestGatesJSONSerialization(t *testing.T) {
	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{
			Level:      contracts.GateLevelPR1,
			Passed:     true,
			Timestamp:  now,
			DurationMs: 1234,
			Checks: []contracts.Check{
				{Name: "unit_tests", Passed: true, Message: "all tests passed"},
			},
		},
	}

	data, err := json.Marshal(gates)
	if err != nil {
		t.Fatalf("failed to marshal gates: %v", err)
	}

	var decoded []contracts.GateResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal gates: %v", err)
	}

	if len(decoded) != 1 {
		t.Errorf("expected 1 gate, got %d", len(decoded))
	}
	if decoded[0].Level != contracts.GateLevelPR1 || !decoded[0].Passed {
		t.Errorf("gate mismatch: %+v", decoded[0])
	}
	if len(decoded[0].Checks) != 1 || decoded[0].Checks[0].Name != "unit_tests" {
		t.Errorf("checks mismatch: %+v", decoded[0].Checks)
	}
}

// TestBudgetPolicyJSONSerialization tests budget policy JSON round-trip.
func TestBudgetPolicyJSONSerialization(t *testing.T) {
	policy := contracts.BudgetPolicy{
		MaxTotalTokensPerRun:   20000,
		MaxTotalRequestsPerRun: 50,
		MaxRequestTokens:       2000,
		MaxLatencyMs:           60000,
		FailOpen:               true,
	}

	data, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("failed to marshal budget policy: %v", err)
	}

	var decoded contracts.BudgetPolicy
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal budget policy: %v", err)
	}

	if decoded.MaxTotalTokensPerRun != 20000 {
		t.Errorf("MaxTotalTokensPerRun mismatch: %d", decoded.MaxTotalTokensPerRun)
	}
	if !decoded.FailOpen {
		t.Error("FailOpen should be true")
	}
}

// TestBudgetStatusJSONSerialization tests budget status JSON round-trip.
func TestBudgetStatusJSONSerialization(t *testing.T) {
	status := contracts.BudgetStatus{
		TokensUsed:        5000,
		TokensRemaining:   15000,
		RequestsUsed:      10,
		RequestsRemaining: 40,
		TimeUsedMs:        30000,
		TimeRemainingMs:   30000,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("failed to marshal budget status: %v", err)
	}

	var decoded contracts.BudgetStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal budget status: %v", err)
	}

	if decoded.TokensUsed != 5000 || decoded.TokensRemaining != 15000 {
		t.Errorf("token fields mismatch: %+v", decoded)
	}
	if decoded.RequestsUsed != 10 || decoded.RequestsRemaining != 40 {
		t.Errorf("request fields mismatch: %+v", decoded)
	}
}

// NOTE: Integration tests require a live PostgreSQL database.
// To run integration tests locally:
//
//   1. Start PostgreSQL: docker-compose up -d postgres
//   2. Set environment variable: export DATABASE_URL="postgres://qfactory:qfactory@localhost:5432/qfactory?sslmode=disable"
//   3. Run tests: go test -v -tags=integration ./internal/store/...
//
// Integration tests are skipped in CI until we add a test database service.
