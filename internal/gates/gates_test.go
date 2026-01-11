package gates

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

// mockStore implements store.Store for testing.
type mockStore struct {
	runs  map[string]*contracts.WorkflowRun
	gates map[string][]contracts.GateResult
}

func newMockStore() *mockStore {
	return &mockStore{
		runs:  make(map[string]*contracts.WorkflowRun),
		gates: make(map[string][]contracts.GateResult),
	}
}

func (m *mockStore) CreateRun(ctx context.Context, run *contracts.WorkflowRun) error {
	m.runs[run.ID] = run
	return nil
}

func (m *mockStore) GetRun(ctx context.Context, id string) (*contracts.WorkflowRun, bool, error) {
	run, ok := m.runs[id]
	return run, ok, nil
}

func (m *mockStore) ListRuns(ctx context.Context, limit int) ([]*contracts.WorkflowRun, error) {
	return nil, nil
}

func (m *mockStore) UpdateRun(ctx context.Context, run *contracts.WorkflowRun) error {
	m.runs[run.ID] = run
	return nil
}

func (m *mockStore) AddEvent(ctx context.Context, event events.Event) error {
	return nil
}

func (m *mockStore) ListEvents(ctx context.Context, runID string, limit int) ([]events.Event, error) {
	return nil, nil
}

func (m *mockStore) AddGateResult(ctx context.Context, runID string, result contracts.GateResult) error {
	name := result.Name
	if name == "" {
		name = "default"
	}
	gates := m.gates[runID]
	replaced := false
	for i, g := range gates {
		gName := g.Name
		if gName == "" {
			gName = "default"
		}
		if g.Level == result.Level && gName == name {
			gates[i] = result
			replaced = true
			break
		}
	}
	if !replaced {
		gates = append(gates, result)
	}
	m.gates[runID] = gates
	return nil
}

func (m *mockStore) AddModelCall(ctx context.Context, runID string, call contracts.ModelCallSummary) error {
	return nil
}

func (m *mockStore) UpdateBudgetStatus(ctx context.Context, runID string, status contracts.BudgetStatus) error {
	return nil
}

func (m *mockStore) GetBudgetStatus(ctx context.Context, runID string) (*contracts.BudgetStatus, error) {
	return nil, nil
}

func (m *mockStore) Close() error {
	return nil
}

// mockGate implements Gate for testing.
type mockGate struct {
	level  string
	name   string
	passed bool
}

func (g *mockGate) Level() string { return g.level }
func (g *mockGate) Name() string  { return g.name }
func (g *mockGate) Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error) {
	now := time.Now().UTC()
	return GateOutput{
		Result: contracts.GateResult{
			Level:       g.level,
			Name:        g.name,
			Passed:      g.passed,
			Executor:    "mock",
			Timestamp:   now,
			DurationMs:  100,
			StartedAt:   &now,
			CompletedAt: &now,
			Checks:      []contracts.Check{{Name: "mock_check", Passed: g.passed, Message: "mock"}},
		},
		Command: "mock command",
		Stdout:  "mock stdout",
		Stderr:  "",
	}, nil
}

func TestGateRunnerPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	run := &contracts.WorkflowRun{
		ID:        "test-run-123",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	gate := &mockGate{level: "PR2", name: "test_gate", passed: true}
	result, err := runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("RunGate failed: %v", err)
	}

	if !result.Passed {
		t.Error("expected gate to pass")
	}
	if result.Level != "PR2" {
		t.Errorf("expected level PR2, got %s", result.Level)
	}

	gates := store.gates[run.ID]
	if len(gates) != 1 {
		t.Errorf("expected 1 gate, got %d", len(gates))
	}

	// Verify evidence files
	resultPath := filepath.Join(tmpDir, run.ID, "gates", "PR2", "test_gate", "result.json")
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("failed to read result.json: %v", err)
	}
	var resultJSON map[string]interface{}
	if err := json.Unmarshal(data, &resultJSON); err != nil {
		t.Fatalf("failed to parse result.json: %v", err)
	}
	if resultJSON["passed"] != true {
		t.Error("result.json passed should be true")
	}

	cmdPath := filepath.Join(tmpDir, run.ID, "gates", "PR2", "test_gate", "command.txt")
	if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
		t.Error("command.txt should exist")
	}
}

func TestGateRunnerReplaceSemantics(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	run := &contracts.WorkflowRun{
		ID:        "test-run-456",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	// Run gate first time - fails
	gate1 := &mockGate{level: "PR2", name: "test_gate", passed: false}
	runner.RunGate(context.Background(), run.ID, gate1)

	// Run gate second time - passes
	gate2 := &mockGate{level: "PR2", name: "test_gate", passed: true}
	runner.RunGate(context.Background(), run.ID, gate2)

	gates := store.gates[run.ID]
	if len(gates) != 1 {
		t.Errorf("expected 1 gate (replaced), got %d", len(gates))
	}
	if !gates[0].Passed {
		t.Error("expected replaced gate to be passed")
	}
}

func TestIntegrationSmokeGateMetadata(t *testing.T) {
	gate := NewIntegrationSmokeGate()

	if gate.Level() != contracts.GateLevelPR2 {
		t.Errorf("expected level PR2, got %s", gate.Level())
	}
	if gate.Name() != "integration_smoke" {
		t.Errorf("expected name integration_smoke, got %s", gate.Name())
	}
}
