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
	runs        map[string]*contracts.WorkflowRun
	gates       map[string][]contracts.GateResult
	gateHistory map[string][]contracts.GateHistoryItem
	events      []events.Event
}

func newMockStore() *mockStore {
	return &mockStore{
		runs:        make(map[string]*contracts.WorkflowRun),
		gates:       make(map[string][]contracts.GateResult),
		gateHistory: make(map[string][]contracts.GateHistoryItem),
		events:      []events.Event{},
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

func (m *mockStore) AddGateResultHistory(ctx context.Context, runID string, id string, result contracts.GateResult) error {
	m.gateHistory[runID] = append(m.gateHistory[runID], contracts.GateHistoryItem{
		ID:     id,
		Result: result,
	})
	return nil
}

func (m *mockStore) ListGateHistory(ctx context.Context, runID string, limit int) ([]contracts.GateHistoryItem, error) {
	return m.gateHistory[runID], nil
}

func (m *mockStore) GetLatestGates(ctx context.Context, runID string) ([]contracts.GateResult, error) {
	return m.gates[runID], nil
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

// mockEventPublisher implements EventPublisher for testing.
type mockEventPublisher struct {
	events []events.Event
}

func newMockEventPublisher() *mockEventPublisher {
	return &mockEventPublisher{events: []events.Event{}}
}

func (p *mockEventPublisher) PublishEvent(ctx context.Context, event events.Event) error {
	p.events = append(p.events, event)
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

func TestGateRunnerHistoryPersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	run := &contracts.WorkflowRun{
		ID:        "test-run-history",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	// Run gate
	gate := &mockGate{level: "PR2", name: "test_gate", passed: true}
	_, err = runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("RunGate failed: %v", err)
	}

	// Verify history was persisted
	history := store.gateHistory[run.ID]
	if len(history) != 1 {
		t.Errorf("expected 1 history item, got %d", len(history))
	}
	if history[0].Result.Level != "PR2" {
		t.Errorf("expected level PR2, got %s", history[0].Result.Level)
	}

	// Run gate again - should add to history (not replace)
	_, err = runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("RunGate failed: %v", err)
	}

	history = store.gateHistory[run.ID]
	if len(history) != 2 {
		t.Errorf("expected 2 history items (append-only), got %d", len(history))
	}

	// But latest view should still have only 1 (replaced)
	gates := store.gates[run.ID]
	if len(gates) != 1 {
		t.Errorf("expected 1 gate in latest view, got %d", len(gates))
	}
}

func TestGateRunnerSSEEvents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	publisher := newMockEventPublisher()
	runner := NewGateRunner(store, tmpDir)
	runner.SetEventPublisher(publisher)

	run := &contracts.WorkflowRun{
		ID:        "test-run-sse",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	// Run passing gate
	gate := &mockGate{level: "PR2", name: "test_gate", passed: true}
	_, err = runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("RunGate failed: %v", err)
	}

	// Verify events were emitted
	if len(publisher.events) != 2 {
		t.Errorf("expected 2 events (started + completed), got %d", len(publisher.events))
	}

	if publisher.events[0].Type != events.EventTypeGateStarted {
		t.Errorf("expected first event gate.started, got %s", publisher.events[0].Type)
	}
	if publisher.events[1].Type != events.EventTypeGateCompleted {
		t.Errorf("expected second event gate.completed, got %s", publisher.events[1].Type)
	}
}

func TestSecurityScanGateMetadata(t *testing.T) {
	gate := NewSecurityScanGate()

	if gate.Level() != contracts.GateLevelPR3 {
		t.Errorf("expected level PR3, got %s", gate.Level())
	}
	if gate.Name() != "security_scan" {
		t.Errorf("expected name security_scan, got %s", gate.Name())
	}
}

func TestGateNameNormalization(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	run := &contracts.WorkflowRun{
		ID:        "test-run-normalize",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	// Run gate with empty name
	gate := &mockGate{level: "PR1", name: "", passed: true}
	result, err := runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("RunGate failed: %v", err)
	}

	// Result should have normalized name
	if result.Name != "default" {
		t.Errorf("expected name 'default', got '%s'", result.Name)
	}

	// History should have normalized name
	history := store.gateHistory[run.ID]
	if len(history) > 0 && history[0].Result.Name != "default" {
		t.Errorf("expected history name 'default', got '%s'", history[0].Result.Name)
	}
}
