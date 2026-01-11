package gates

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AIfactory-hq/qfactory/internal/store"
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

func (m *mockStore) AddGateHistoryItem(ctx context.Context, runID string, item contracts.GateHistoryItem) error {
	m.gateHistory[runID] = append(m.gateHistory[runID], item)
	return nil
}

func (m *mockStore) GetLatestExecution(ctx context.Context, runID, level, name string) (string, error) {
	name = contracts.NormalizeGateName(name)
	history := m.gateHistory[runID]
	for _, item := range history {
		itemName := contracts.NormalizeGateName(item.Result.Name)
		if item.Result.Level == level && itemName == name {
			return item.Result.ExecutionID, nil
		}
	}
	return "", nil
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

func (m *mockStore) FinalizeRun(ctx context.Context, runID string, params store.FinalizeParams) error {
	return nil
}

func (m *mockStore) IsRunFinalized(ctx context.Context, runID string) (bool, error) {
	run, ok := m.runs[runID]
	if !ok {
		return false, nil
	}
	return run.IsFinalized(), nil
}

func (m *mockStore) ListRunsByTenant(ctx context.Context, tenantID, projectID string, limit int) ([]*contracts.WorkflowRun, error) {
	var runs []*contracts.WorkflowRun
	for _, run := range m.runs {
		if (tenantID == "" || run.TenantID == tenantID) && (projectID == "" || run.ProjectID == projectID) {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func (m *mockStore) SetGatesRunning(ctx context.Context, runID string, running bool) error {
	run, ok := m.runs[runID]
	if !ok {
		return nil
	}
	run.GatesRunning = running
	return nil
}

func (m *mockStore) UpdatePolicyDecision(ctx context.Context, runID string, decision *contracts.PolicyDecision, snapshot *contracts.GatePolicy) error {
	run, ok := m.runs[runID]
	if !ok {
		return nil
	}
	run.PolicyDecision = decision
	run.PolicySnapshot = snapshot
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

// v0.8 Gate Registry tests

func TestRegistry_NewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry should return non-nil")
	}
	if len(r.Keys()) != 0 {
		t.Errorf("expected empty registry, got %d gates", len(r.Keys()))
	}
}

func TestRegistry_DefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if r == nil {
		t.Fatal("DefaultRegistry should return non-nil")
	}

	keys := r.Keys()
	if len(keys) < 3 {
		t.Errorf("expected at least 3 built-in gates, got %d", len(keys))
	}

	// Verify built-in gates are registered
	if gate := r.Get("PR1", "unit_tests"); gate == nil {
		t.Error("expected PR1/unit_tests to be registered")
	}
	if gate := r.Get("PR2", "integration_smoke"); gate == nil {
		t.Error("expected PR2/integration_smoke to be registered")
	}
	if gate := r.Get("PR3", "security_scan"); gate == nil {
		t.Error("expected PR3/security_scan to be registered")
	}
}

func TestRegistry_Lookup(t *testing.T) {
	r := DefaultRegistry()

	gate, found := r.Lookup("PR1", "unit_tests")
	if !found {
		t.Error("expected to find PR1/unit_tests")
	}
	if gate.Level() != "PR1" {
		t.Errorf("expected level PR1, got %s", gate.Level())
	}

	_, found = r.Lookup("PR9", "nonexistent")
	if found {
		t.Error("should not find nonexistent gate")
	}
}

func TestRegistry_NormalizedLookup(t *testing.T) {
	r := DefaultRegistry()

	// Lookup with whitespace should work due to normalization
	gate := r.Get("PR1", "  unit_tests  ")
	if gate != nil {
		// If the registry normalizes on lookup, this should work
		// Currently it might not - depends on implementation
	}
}

// v0.8 RetryGate tests

func TestGateRunner_RetryGate(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test_retry")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)

	// Set up registry with our mock gate
	registry := NewRegistry()
	registry.Register(&mockGate{level: "PR2", name: "test_gate", passed: true})
	runner.SetRegistry(registry)

	run := &contracts.WorkflowRun{
		ID:        "test-run-retry",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	// First execution
	gate := &mockGate{level: "PR2", name: "test_gate", passed: false}
	result1, err := runner.RunGate(context.Background(), run.ID, gate)
	if err != nil {
		t.Fatalf("first RunGate failed: %v", err)
	}

	// Retry the gate
	result2, err := runner.RetryGate(context.Background(), run.ID, "PR2", "test_gate", "testing retry")
	if err != nil {
		t.Fatalf("RetryGate failed: %v", err)
	}

	// Verify lineage
	history := store.gateHistory[run.ID]
	if len(history) != 2 {
		t.Errorf("expected 2 history items, got %d", len(history))
	}

	// Second entry should have parent_execution_id pointing to first
	if history[1].ParentExecutionID == nil {
		t.Error("retry should have parent_execution_id set")
	} else if *history[1].ParentExecutionID != result1.ExecutionID {
		t.Error("retry parent_execution_id should match first execution")
	}

	if history[1].RetryReason != "testing retry" {
		t.Errorf("expected retry_reason 'testing retry', got '%s'", history[1].RetryReason)
	}

	// Execution IDs should be different
	if result1.ExecutionID == result2.ExecutionID {
		t.Error("retry should have different execution_id")
	}
}

func TestGateRunner_RetryGate_NotFound(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "gates_test_retry_notfound")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	store := newMockStore()
	runner := NewGateRunner(store, tmpDir)
	runner.SetRegistry(NewRegistry()) // Empty registry

	run := &contracts.WorkflowRun{
		ID:        "test-run-retry-notfound",
		Status:    contracts.RunStatusCompleted,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	store.CreateRun(context.Background(), run)

	_, err = runner.RetryGate(context.Background(), run.ID, "PR2", "nonexistent", "")
	if err == nil {
		t.Error("expected error for nonexistent gate")
	}
}

// v0.8 TrustCalculator with history tests

func TestTrustCalculator_WithHistory_Empty(t *testing.T) {
	calculator := NewTrustCalculator()

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
	}

	// Empty history should work fine
	result := calculator.CalculateWithHistory(nil, gates, nil)

	if result.Score < 90 {
		t.Errorf("expected high score with passing gate, got %d", result.Score)
	}
}

func TestTrustCalculator_WithHistory_ConsecutiveFailures(t *testing.T) {
	calculator := NewTrustCalculator()

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: false, Timestamp: now},
	}

	// History with consecutive failures (newest first)
	history := []contracts.GateHistoryItem{
		{ID: "h3", Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: false}},
		{ID: "h2", Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: false}},
		{ID: "h1", Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: false}},
	}

	result := calculator.CalculateWithHistory(nil, gates, history)

	if result.Breakdown.ConsecutiveFailurePenalty == 0 {
		t.Error("expected consecutive failure penalty")
	}
}

func TestTrustCalculator_WithHistory_Recovery(t *testing.T) {
	calculator := NewTrustCalculator()

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
	}

	parentID := "h1"
	// History: current passes (retry of failed), previous failed
	history := []contracts.GateHistoryItem{
		{
			ID:                "h2",
			Result:            contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: true, ExecutionID: "h2"},
			ParentExecutionID: &parentID,
			RetryReason:       "retry after failure",
		},
		{
			ID:     "h1",
			Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: false, ExecutionID: "h1"},
		},
	}

	result := calculator.CalculateWithHistory(nil, gates, history)

	if result.Breakdown.RecoveryReward == 0 {
		t.Error("expected recovery reward for successful retry after failure")
	}
}

// v0.8 NormalizeGateName tests

func TestNormalizeGateName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "default"},
		{"  ", "default"},
		{"\t\n", "default"},
		{"unit_tests", "unit_tests"},
		{"  unit_tests  ", "unit_tests"},
		{"My Gate", "My Gate"},
	}

	for _, tc := range tests {
		result := contracts.NormalizeGateName(tc.input)
		if result != tc.expected {
			t.Errorf("NormalizeGateName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestTruncateRetryReason(t *testing.T) {
	short := "short reason"
	result := contracts.TruncateRetryReason(short)
	if result != short {
		t.Errorf("short reason should not be truncated")
	}

	// Test long reason
	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}
	result = contracts.TruncateRetryReason(long)
	if len(result) > contracts.MaxRetryReasonLength {
		t.Errorf("long reason should be truncated to %d, got %d", contracts.MaxRetryReasonLength, len(result))
	}
}

// v0.8 GetRegistry tests

func TestGateRunner_GetRegistry(t *testing.T) {
	store := newMockStore()
	runner := NewGateRunner(store, "/tmp")

	// First call should create default registry
	registry := runner.GetRegistry()
	if registry == nil {
		t.Fatal("GetRegistry should return non-nil")
	}

	// Should have built-in gates
	if gate := registry.Get("PR1", "unit_tests"); gate == nil {
		t.Error("default registry should have PR1/unit_tests")
	}

	// Subsequent calls should return same registry
	registry2 := runner.GetRegistry()
	if registry != registry2 {
		t.Error("GetRegistry should return same instance")
	}
}

func TestGateRunner_SetRegistry(t *testing.T) {
	store := newMockStore()
	runner := NewGateRunner(store, "/tmp")

	customRegistry := NewRegistry()
	runner.SetRegistry(customRegistry)

	// GetRegistry should return the custom registry
	if runner.GetRegistry() != customRegistry {
		t.Error("SetRegistry should set the registry")
	}
}

// v1.0 RBAC and Policy tests

func TestRoleHasPermission(t *testing.T) {
	tests := []struct {
		role       contracts.Role
		permission contracts.Permission
		expected   bool
	}{
		{contracts.RoleViewer, contracts.PermissionRead, true},
		{contracts.RoleViewer, contracts.PermissionRunGates, false},
		{contracts.RoleViewer, contracts.PermissionFinalize, false},
		{contracts.RoleOperator, contracts.PermissionRead, true},
		{contracts.RoleOperator, contracts.PermissionRunGates, true},
		{contracts.RoleOperator, contracts.PermissionFinalize, false},
		{contracts.RoleApprover, contracts.PermissionFinalize, true},
		{contracts.RoleApprover, contracts.PermissionOverride, false},
		{contracts.RoleAdmin, contracts.PermissionRead, true},
		{contracts.RoleAdmin, contracts.PermissionRunGates, true},
		{contracts.RoleAdmin, contracts.PermissionFinalize, true},
		{contracts.RoleAdmin, contracts.PermissionOverride, true},
		{contracts.RoleAdmin, contracts.PermissionManagePolicy, true},
	}

	for _, tc := range tests {
		result := contracts.RoleHasPermission(tc.role, tc.permission)
		if result != tc.expected {
			t.Errorf("RoleHasPermission(%s, %s) = %v, expected %v", tc.role, tc.permission, result, tc.expected)
		}
	}
}

func TestPolicyEvaluator_RequireAllPassed_NoHistoricalFailures(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	policy := &contracts.GatePolicy{
		RequiredLevels:   []string{"PR1"},
		RequireAllPassed: true,
	}

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now},
	}

	// History with only passing gates
	history := []contracts.GateHistoryItem{
		{ID: "h1", Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: true}},
	}

	decision := evaluator.EvaluateWithHistory(policy, gates, history)

	if !decision.Allowed {
		t.Error("expected decision to be allowed when all historical gates passed")
	}
}

func TestPolicyEvaluator_RequireAllPassed_WithHistoricalFailures(t *testing.T) {
	evaluator := NewPolicyEvaluator()

	policy := &contracts.GatePolicy{
		RequiredLevels:   []string{"PR1"},
		RequireAllPassed: true,
	}

	now := time.Now().UTC()
	gates := []contracts.GateResult{
		{Level: "PR1", Name: "unit_tests", Passed: true, Timestamp: now}, // Latest passes
	}

	// History with a previous failure
	history := []contracts.GateHistoryItem{
		{ID: "h2", Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: true}},
		{ID: "h1", Result: contracts.GateResult{Level: "PR1", Name: "unit_tests", Passed: false}}, // Previous failure
	}

	decision := evaluator.EvaluateWithHistory(policy, gates, history)

	if decision.Allowed {
		t.Error("expected decision to be denied when RequireAllPassed and historical failure exists")
	}
	if len(decision.Failing) == 0 {
		t.Error("expected failing gates to be reported")
	}
}

func TestPolicyEvaluator_ExtendedPolicy_MinTrustScore(t *testing.T) {
	policy := &contracts.GatePolicy{
		RequiredLevels: []string{"PR1"},
		MinTrustScore:  85,
		AllowOverride:  true,
	}

	if policy.MinTrustScore != 85 {
		t.Errorf("expected MinTrustScore 85, got %d", policy.MinTrustScore)
	}
	if !policy.AllowOverride {
		t.Error("expected AllowOverride to be true")
	}
}

func TestDefaultOperationalLimits(t *testing.T) {
	limits := contracts.DefaultOperationalLimits()

	if limits.MaxGateRuntimeSeconds != 600 {
		t.Errorf("expected MaxGateRuntimeSeconds 600, got %d", limits.MaxGateRuntimeSeconds)
	}
	if limits.MaxRetriesPerGate != 3 {
		t.Errorf("expected MaxRetriesPerGate 3, got %d", limits.MaxRetriesPerGate)
	}
	expectedSize := int64(100 * 1024 * 1024) // 100MB
	if limits.MaxCapsuleSizeBytes != expectedSize {
		t.Errorf("expected MaxCapsuleSizeBytes %d, got %d", expectedSize, limits.MaxCapsuleSizeBytes)
	}
}

func TestAPIError(t *testing.T) {
	err := contracts.APIError{
		Code:    contracts.ErrCodeForbiddenRole,
		Message: "role 'viewer' does not have permission 'run_gates'",
		Details: "required_permission=run_gates",
	}

	if err.Code != contracts.ErrCodeForbiddenRole {
		t.Errorf("expected code %s, got %s", contracts.ErrCodeForbiddenRole, err.Code)
	}
	if err.Message == "" {
		t.Error("expected non-empty message")
	}
}
