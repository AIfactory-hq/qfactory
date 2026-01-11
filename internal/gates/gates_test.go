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
	runs         map[string]*contracts.WorkflowRun
	gates        map[string][]contracts.GateResult
	gateHistory  map[string][]contracts.GateHistoryItem
	gatePolicies map[string]*contracts.GatePolicy
	events       []events.Event
}

func newMockStore() *mockStore {
	return &mockStore{
		runs:         make(map[string]*contracts.WorkflowRun),
		gates:        make(map[string][]contracts.GateResult),
		gateHistory:  make(map[string][]contracts.GateHistoryItem),
		gatePolicies: make(map[string]*contracts.GatePolicy),
		events:       []events.Event{},
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

func (m *mockStore) UpdateGatePolicy(ctx context.Context, runID string, policy *contracts.GatePolicy) error {
	m.gatePolicies[runID] = policy
	return nil
}

func (m *mockStore) GetGatePolicy(ctx context.Context, runID string) (*contracts.GatePolicy, error) {
	return m.gatePolicies[runID], nil
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

	// Verify evidence files using the evidence path from result (includes execution_id)
	if result.EvidencePath == "" {
		t.Fatal("expected evidence_path to be set")
	}
	resultPath := filepath.Join(result.EvidencePath, "result.json")
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

	cmdPath := filepath.Join(result.EvidencePath, "command.txt")
	if _, err := os.Stat(cmdPath); os.IsNotExist(err) {
		t.Error("command.txt should exist")
	}

	// Verify execution_id is set
	if result.ExecutionID == "" {
		t.Error("expected execution_id to be set")
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

// PolicyEvaluator tests

func TestPolicyEvaluator_NilPolicy(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	decision := evaluator.Evaluate(nil, nil)

	if !decision.Allowed {
		t.Error("expected decision to be allowed with nil policy")
	}
	if decision.Message != "no policy defined" {
		t.Errorf("expected message 'no policy defined', got '%s'", decision.Message)
	}
}

func TestPolicyEvaluator_RequiredLevels(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	policy := &contracts.GatePolicy{
		RequiredLevels: []string{"PR1", "PR2"},
	}

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
		{Level: "PR2", Name: "integration", Passed: true, Timestamp: now},
	}

	decision := evaluator.Evaluate(policy, gates)

	if !decision.Allowed {
		t.Errorf("expected decision to be allowed, got message: %s", decision.Message)
	}
}

func TestPolicyEvaluator_MissingGates(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	policy := &contracts.GatePolicy{
		RequiredLevels: []string{"PR1", "PR2"},
	}

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
	}

	decision := evaluator.Evaluate(policy, gates)

	if decision.Allowed {
		t.Error("expected decision to be denied due to missing PR2")
	}
	if len(decision.Missing) != 1 {
		t.Errorf("expected 1 missing gate, got %d", len(decision.Missing))
	}
}

func TestPolicyEvaluator_FailingGates(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	policy := &contracts.GatePolicy{
		RequiredLevels: []string{"PR1"},
	}

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: false, Timestamp: now},
	}

	decision := evaluator.Evaluate(policy, gates)

	if decision.Allowed {
		t.Error("expected decision to be denied due to failing gate")
	}
	if len(decision.Failing) != 1 {
		t.Errorf("expected 1 failing gate, got %d", len(decision.Failing))
	}
}

func TestPolicyEvaluator_FailOpen(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	policy := &contracts.GatePolicy{
		RequiredLevels: []string{"PR1", "PR2"},
		FailOpen:       true,
	}

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
	}

	decision := evaluator.Evaluate(policy, gates)

	if !decision.Allowed {
		t.Error("expected decision to be allowed with fail_open")
	}
	if decision.Message != "fail_open enabled; issues ignored" {
		t.Errorf("expected fail_open message, got '%s'", decision.Message)
	}
}

// TrustCalculator tests

func TestTrustCalculator_NoGates(t *testing.T) {
	calculator := NewTrustCalculator()

	result := calculator.Calculate(nil, nil)

	if result.Score != 0 {
		t.Errorf("expected score 0, got %d", result.Score)
	}
	if result.Grade != "F" {
		t.Errorf("expected grade F, got %s", result.Grade)
	}
}

func TestTrustCalculator_AllPassed(t *testing.T) {
	calculator := NewTrustCalculator()

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
		{Level: "PR2", Name: "integration", Passed: true, Timestamp: now},
		{Level: "PR3", Name: "security", Passed: true, Timestamp: now},
	}

	result := calculator.Calculate(nil, gates)

	if result.Score < 90 {
		t.Errorf("expected score >= 90, got %d", result.Score)
	}
	if result.Grade != "A" {
		t.Errorf("expected grade A, got %s", result.Grade)
	}
	if result.Breakdown.PassedRequired != 3 {
		t.Errorf("expected 3 passed, got %d", result.Breakdown.PassedRequired)
	}
}

func TestTrustCalculator_WithFailures(t *testing.T) {
	calculator := NewTrustCalculator()

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
		{Level: "PR2", Name: "integration", Passed: false, Timestamp: now},
	}

	result := calculator.Calculate(nil, gates)

	if result.Breakdown.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", result.Breakdown.Failed)
	}
	if result.Grade == "A" {
		t.Error("expected grade less than A with failures")
	}
}

func TestTrustCalculator_Grades(t *testing.T) {
	calculator := NewTrustCalculator()

	tests := []struct {
		score int
		grade string
	}{
		{95, "A"},
		{85, "B"},
		{75, "C"},
		{65, "D"},
		{55, "F"},
	}

	for _, tc := range tests {
		grade := calculator.scoreToGrade(tc.score)
		if grade != tc.grade {
			t.Errorf("score %d: expected grade %s, got %s", tc.score, tc.grade, grade)
		}
	}
}

// Executor tests

func TestLocalExecutor_Name(t *testing.T) {
	executor := NewLocalExecutor()
	if executor.Name() != "local" {
		t.Errorf("expected name 'local', got '%s'", executor.Name())
	}
}

func TestLocalExecutor_Execute(t *testing.T) {
	executor := NewLocalExecutor()

	run := &contracts.WorkflowRun{
		ID:     "test-run",
		Status: contracts.RunStatusCompleted,
	}

	gate := &mockGate{level: "PR1", name: "test", passed: true}
	output, err := executor.Execute(context.Background(), run, gate)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !output.Result.Passed {
		t.Error("expected gate to pass")
	}
	// The mockGate already sets executor to "mock", so LocalExecutor preserves it
	// (only sets to "local" if executor is empty)
	if output.Result.Executor != "mock" {
		t.Errorf("expected executor 'mock' (from mock gate), got '%s'", output.Result.Executor)
	}
}

func TestRemoteExecutor_Name(t *testing.T) {
	executor := NewRemoteExecutor("http://localhost:9090")
	if executor.Name() != "remote" {
		t.Errorf("expected name 'remote', got '%s'", executor.Name())
	}
}

func TestUnitTestGateMetadata(t *testing.T) {
	gate := NewUnitTestGate()

	if gate.Level() != contracts.GateLevelPR1 {
		t.Errorf("expected level PR1, got %s", gate.Level())
	}
	if gate.Name() != "unit_tests" {
		t.Errorf("expected name unit_tests, got %s", gate.Name())
	}
}

func TestGateRunnerWithExecutor(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	run := &contracts.WorkflowRun{
		ID:        "test-run-executor",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	gate := &mockGate{level: "PR2", name: "test_gate", passed: true}
	executor := NewLocalExecutor()

	result, err := runner.RunGateWithExecutor(context.Background(), run.ID, gate, executor)
	if err != nil {
		t.Fatalf("RunGateWithExecutor failed: %v", err)
	}

	if !result.Passed {
		t.Error("expected gate to pass")
	}
	// The mock gate sets executor to "mock" in its Run(), but LocalExecutor sets it to "local"
	// Since the mockGate returns "mock", the executor preserves that unless empty
	if result.Executor != "mock" {
		t.Errorf("expected executor 'mock', got '%s'", result.Executor)
	}
}

func TestEvidenceImmutabilityPerExecution(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test_immutability")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	run := &contracts.WorkflowRun{
		ID:        "test-run-immutability",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	gate := &mockGate{level: "PR2", name: "test_gate", passed: true}

	// Execute gate multiple times
	result1, err := runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("first RunGate failed: %v", err)
	}

	result2, err := runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("second RunGate failed: %v", err)
	}

	result3, err := runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("third RunGate failed: %v", err)
	}

	// Verify each execution has a unique execution_id
	if result1.ExecutionID == "" {
		t.Error("first result should have execution_id")
	}
	if result2.ExecutionID == "" {
		t.Error("second result should have execution_id")
	}
	if result3.ExecutionID == "" {
		t.Error("third result should have execution_id")
	}

	if result1.ExecutionID == result2.ExecutionID {
		t.Error("execution IDs should be unique between runs")
	}
	if result2.ExecutionID == result3.ExecutionID {
		t.Error("execution IDs should be unique between runs")
	}

	// Verify each execution has a separate evidence directory
	if result1.EvidencePath == result2.EvidencePath {
		t.Error("evidence paths should be unique between runs")
	}
	if result2.EvidencePath == result3.EvidencePath {
		t.Error("evidence paths should be unique between runs")
	}

	// Verify all evidence directories exist
	for i, path := range []string{result1.EvidencePath, result2.EvidencePath, result3.EvidencePath} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("evidence directory %d does not exist: %s", i+1, path)
		}
	}

	// Verify history has 3 separate entries
	history := store.gateHistory[run.ID]
	if len(history) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(history))
	}

	// Verify each history entry has unique ID and execution_id
	ids := make(map[string]bool)
	execIds := make(map[string]bool)
	for _, item := range history {
		if ids[item.ID] {
			t.Errorf("duplicate history ID: %s", item.ID)
		}
		ids[item.ID] = true

		if execIds[item.Result.ExecutionID] {
			t.Errorf("duplicate execution_id in history: %s", item.Result.ExecutionID)
		}
		execIds[item.Result.ExecutionID] = true
	}

	// Verify latest view still has only 1 gate (replace semantics)
	gates := store.gates[run.ID]
	if len(gates) != 1 {
		t.Errorf("expected 1 gate in latest view (replaced), got %d", len(gates))
	}

	// Verify all 3 evidence directories contain result.json with unique execution_ids
	for _, path := range []string{result1.EvidencePath, result2.EvidencePath, result3.EvidencePath} {
		resultPath := filepath.Join(path, "result.json")
		data, err := os.ReadFile(resultPath)
		if err != nil {
			t.Errorf("failed to read result.json at %s: %v", path, err)
			continue
		}

		var resultJSON map[string]interface{}
		if err := json.Unmarshal(data, &resultJSON); err != nil {
			t.Errorf("failed to parse result.json at %s: %v", path, err)
			continue
		}

		if resultJSON["execution_id"] == nil || resultJSON["execution_id"] == "" {
			t.Errorf("result.json at %s missing execution_id", path)
		}
	}
}

func TestExecutionIDInEvents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test_events")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	publisher := newMockEventPublisher()
	runner := NewGateRunner(store, tmpDir)
	runner.SetEventPublisher(publisher)

	run := &contracts.WorkflowRun{
		ID:        "test-run-event-execid",
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

	// Verify events contain execution_id
	if len(publisher.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(publisher.events))
	}

	// Parse started event payload
	var startPayload events.GatePayload
	if err := json.Unmarshal(publisher.events[0].Payload, &startPayload); err != nil {
		t.Fatalf("failed to parse started event payload: %v", err)
	}
	if startPayload.ExecutionID == "" {
		t.Error("started event should contain execution_id")
	}
	if startPayload.ExecutionID != result.ExecutionID {
		t.Error("started event execution_id should match result execution_id")
	}

	// Parse completed event payload
	var completePayload events.GatePayload
	if err := json.Unmarshal(publisher.events[1].Payload, &completePayload); err != nil {
		t.Fatalf("failed to parse completed event payload: %v", err)
	}
	if completePayload.ExecutionID == "" {
		t.Error("completed event should contain execution_id")
	}
	if completePayload.ExecutionID != result.ExecutionID {
		t.Error("completed event execution_id should match result execution_id")
	}
}
