package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

// MaxModelCalls is the maximum number of model calls to store per run.
const MaxModelCalls = 100

// PostgresStore implements Store using PostgreSQL.
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore creates a new PostgreSQL-backed store.
func NewPostgresStore(ctx context.Context, connStr string) (*PostgresStore, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &PostgresStore{db: db}

	// Run migrations
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// migrate runs database migrations.
func (s *PostgresStore) migrate(ctx context.Context) error {
	// Run migration 001
	migrationSQL, err := os.ReadFile("infra/migrations/001_initial.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration 001: %w", err)
	}

	_, err = s.db.ExecContext(ctx, string(migrationSQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration 001: %w", err)
	}

	// Run migration 002
	migration002SQL, err := os.ReadFile("infra/migrations/002_gate_history.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration 002: %w", err)
	}

	_, err = s.db.ExecContext(ctx, string(migration002SQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration 002: %w", err)
	}

	// Run migration 003
	migration003SQL, err := os.ReadFile("infra/migrations/003_gate_policy.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration 003: %w", err)
	}

	_, err = s.db.ExecContext(ctx, string(migration003SQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration 003: %w", err)
	}

	// Run migration 004
	migration004SQL, err := os.ReadFile("infra/migrations/004_gate_lineage.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration 004: %w", err)
	}

	_, err = s.db.ExecContext(ctx, string(migration004SQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration 004: %w", err)
	}

	// Run migration 005
	migration005SQL, err := os.ReadFile("infra/migrations/005_finalize_capsule.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration 005: %w", err)
	}

	_, err = s.db.ExecContext(ctx, string(migration005SQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration 005: %w", err)
	}

	// Run migration 006
	migration006SQL, err := os.ReadFile("infra/migrations/006_tenancy_rbac.sql")
	if err != nil {
		return fmt.Errorf("failed to read migration 006: %w", err)
	}

	_, err = s.db.ExecContext(ctx, string(migration006SQL))
	if err != nil {
		return fmt.Errorf("failed to execute migration 006: %w", err)
	}

	log.Println("Database migrations applied successfully")
	return nil
}

// Close closes the database connection.
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// CreateRun persists a new workflow run.
func (s *PostgresStore) CreateRun(ctx context.Context, run *contracts.WorkflowRun) error {
	stagesJSON, err := json.Marshal(run.Stages)
	if err != nil {
		return fmt.Errorf("failed to marshal stages: %w", err)
	}

	gatesJSON, err := json.Marshal(run.Gates)
	if err != nil {
		return fmt.Errorf("failed to marshal gates: %w", err)
	}

	var budgetPolicyJSON []byte
	if run.BudgetPolicy != nil {
		budgetPolicyJSON, err = json.Marshal(run.BudgetPolicy)
		if err != nil {
			return fmt.Errorf("failed to marshal budget policy: %w", err)
		}
	}

	var gatePolicyJSON []byte
	if run.GatePolicy != nil {
		gatePolicyJSON, err = json.Marshal(run.GatePolicy)
		if err != nil {
			return fmt.Errorf("failed to marshal gate policy: %w", err)
		}
	}

	modelCallsJSON, err := json.Marshal(run.ModelCalls)
	if err != nil {
		return fmt.Errorf("failed to marshal model calls: %w", err)
	}

	query := `
		INSERT INTO runs (id, temporal_id, status, current_stage, created_at, updated_at,
		                  completed_at, error, stages_json, gates_json, budget_policy_json, gate_policy_json, model_calls_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err = s.db.ExecContext(ctx, query,
		run.ID,
		run.TemporalID,
		string(run.Status),
		run.CurrentStage,
		run.CreatedAt,
		run.UpdatedAt,
		run.CompletedAt,
		nullString(run.Error),
		stagesJSON,
		gatesJSON,
		nullBytes(budgetPolicyJSON),
		nullBytes(gatePolicyJSON),
		modelCallsJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to insert run: %w", err)
	}

	return nil
}

// GetRun retrieves a run by ID.
func (s *PostgresStore) GetRun(ctx context.Context, id string) (*contracts.WorkflowRun, bool, error) {
	query := `
		SELECT id, temporal_id, status, current_stage, created_at, updated_at,
		       completed_at, error, stages_json, gates_json, budget_policy_json,
		       gate_policy_json, budget_status_json, model_calls_json,
		       finalized_at, finalized_by, finalize_reason, finalize_override,
		       finalize_override_reason, capsule_id, capsule_path, capsule_manifest_sha256,
		       tenant_id, project_id, gates_running, policy_decision, policy_snapshot
		FROM runs WHERE id = $1
	`

	run := &contracts.WorkflowRun{}
	var temporalID, currentStage, errorStr sql.NullString
	var completedAt, finalizedAt sql.NullTime
	var stagesJSON, gatesJSON, modelCallsJSON []byte
	var budgetPolicyJSON, gatePolicyJSON, budgetStatusJSON sql.NullString
	var finalizedBy, finalizeReason, finalizeOverrideReason sql.NullString
	var finalizeOverride sql.NullBool
	var capsuleID, capsulePath, capsuleManifestSHA256 sql.NullString
	var tenantID, projectID sql.NullString
	var gatesRunning sql.NullBool
	var policyDecisionJSON, policySnapshotJSON sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&run.ID,
		&temporalID,
		&run.Status,
		&currentStage,
		&run.CreatedAt,
		&run.UpdatedAt,
		&completedAt,
		&errorStr,
		&stagesJSON,
		&gatesJSON,
		&budgetPolicyJSON,
		&gatePolicyJSON,
		&budgetStatusJSON,
		&modelCallsJSON,
		&finalizedAt,
		&finalizedBy,
		&finalizeReason,
		&finalizeOverride,
		&finalizeOverrideReason,
		&capsuleID,
		&capsulePath,
		&capsuleManifestSHA256,
		&tenantID,
		&projectID,
		&gatesRunning,
		&policyDecisionJSON,
		&policySnapshotJSON,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("failed to query run: %w", err)
	}

	run.TemporalID = temporalID.String
	run.CurrentStage = currentStage.String
	run.Error = errorStr.String
	run.TenantID = tenantID.String
	run.ProjectID = projectID.String
	if gatesRunning.Valid {
		run.GatesRunning = gatesRunning.Bool
	}
	if completedAt.Valid {
		run.CompletedAt = &completedAt.Time
	}
	if finalizedAt.Valid {
		run.FinalizedAt = &finalizedAt.Time
	}
	run.FinalizedBy = finalizedBy.String
	run.FinalizeReason = finalizeReason.String
	if finalizeOverride.Valid {
		run.FinalizeOverride = finalizeOverride.Bool
	}
	run.FinalizeOverrideReason = finalizeOverrideReason.String
	run.CapsuleID = capsuleID.String
	run.CapsulePath = capsulePath.String
	run.CapsuleManifestSHA256 = capsuleManifestSHA256.String

	if err := json.Unmarshal(stagesJSON, &run.Stages); err != nil {
		return nil, false, fmt.Errorf("failed to unmarshal stages: %w", err)
	}

	if len(gatesJSON) > 0 {
		if err := json.Unmarshal(gatesJSON, &run.Gates); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal gates: %w", err)
		}
	}

	if budgetPolicyJSON.Valid && budgetPolicyJSON.String != "" {
		run.BudgetPolicy = &contracts.BudgetPolicy{}
		if err := json.Unmarshal([]byte(budgetPolicyJSON.String), run.BudgetPolicy); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal budget policy: %w", err)
		}
	}

	if gatePolicyJSON.Valid && gatePolicyJSON.String != "" && gatePolicyJSON.String != "{}" {
		run.GatePolicy = &contracts.GatePolicy{}
		if err := json.Unmarshal([]byte(gatePolicyJSON.String), run.GatePolicy); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal gate policy: %w", err)
		}
	}

	if len(modelCallsJSON) > 0 {
		if err := json.Unmarshal(modelCallsJSON, &run.ModelCalls); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal model calls: %w", err)
		}
	}

	if policyDecisionJSON.Valid && policyDecisionJSON.String != "" {
		run.PolicyDecision = &contracts.PolicyDecision{}
		if err := json.Unmarshal([]byte(policyDecisionJSON.String), run.PolicyDecision); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal policy decision: %w", err)
		}
	}

	if policySnapshotJSON.Valid && policySnapshotJSON.String != "" {
		run.PolicySnapshot = &contracts.GatePolicy{}
		if err := json.Unmarshal([]byte(policySnapshotJSON.String), run.PolicySnapshot); err != nil {
			return nil, false, fmt.Errorf("failed to unmarshal policy snapshot: %w", err)
		}
	}

	return run, true, nil
}

// ListRuns returns runs ordered by updated_at desc.
func (s *PostgresStore) ListRuns(ctx context.Context, limit int) ([]*contracts.WorkflowRun, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, temporal_id, status, current_stage, created_at, updated_at,
		       completed_at, error, stages_json, gates_json, budget_policy_json, gate_policy_json, model_calls_json
		FROM runs
		ORDER BY updated_at DESC
		LIMIT $1
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs: %w", err)
	}
	defer rows.Close()

	var runs []*contracts.WorkflowRun
	for rows.Next() {
		run := &contracts.WorkflowRun{}
		var temporalID, currentStage, errorStr sql.NullString
		var completedAt sql.NullTime
		var stagesJSON, gatesJSON, modelCallsJSON []byte
		var budgetPolicyJSON, gatePolicyJSON sql.NullString

		err := rows.Scan(
			&run.ID,
			&temporalID,
			&run.Status,
			&currentStage,
			&run.CreatedAt,
			&run.UpdatedAt,
			&completedAt,
			&errorStr,
			&stagesJSON,
			&gatesJSON,
			&budgetPolicyJSON,
			&gatePolicyJSON,
			&modelCallsJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan run: %w", err)
		}

		run.TemporalID = temporalID.String
		run.CurrentStage = currentStage.String
		run.Error = errorStr.String
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}

		if err := json.Unmarshal(stagesJSON, &run.Stages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal stages: %w", err)
		}
		if len(gatesJSON) > 0 {
			json.Unmarshal(gatesJSON, &run.Gates)
		}
		if budgetPolicyJSON.Valid {
			run.BudgetPolicy = &contracts.BudgetPolicy{}
			json.Unmarshal([]byte(budgetPolicyJSON.String), run.BudgetPolicy)
		}
		if gatePolicyJSON.Valid && gatePolicyJSON.String != "" && gatePolicyJSON.String != "{}" {
			run.GatePolicy = &contracts.GatePolicy{}
			json.Unmarshal([]byte(gatePolicyJSON.String), run.GatePolicy)
		}
		if len(modelCallsJSON) > 0 {
			json.Unmarshal(modelCallsJSON, &run.ModelCalls)
		}

		runs = append(runs, run)
	}

	return runs, rows.Err()
}

// UpdateRun updates an existing run.
func (s *PostgresStore) UpdateRun(ctx context.Context, run *contracts.WorkflowRun) error {
	stagesJSON, _ := json.Marshal(run.Stages)
	gatesJSON, _ := json.Marshal(run.Gates)
	modelCallsJSON, _ := json.Marshal(run.ModelCalls)

	var budgetPolicyJSON []byte
	if run.BudgetPolicy != nil {
		budgetPolicyJSON, _ = json.Marshal(run.BudgetPolicy)
	}

	var gatePolicyJSON []byte
	if run.GatePolicy != nil {
		gatePolicyJSON, _ = json.Marshal(run.GatePolicy)
	}

	query := `
		UPDATE runs SET
			temporal_id = $2,
			status = $3,
			current_stage = $4,
			updated_at = $5,
			completed_at = $6,
			error = $7,
			stages_json = $8,
			gates_json = $9,
			budget_policy_json = $10,
			gate_policy_json = $11,
			model_calls_json = $12
		WHERE id = $1
	`

	result, err := s.db.ExecContext(ctx, query,
		run.ID,
		run.TemporalID,
		string(run.Status),
		run.CurrentStage,
		run.UpdatedAt,
		run.CompletedAt,
		nullString(run.Error),
		stagesJSON,
		gatesJSON,
		nullBytes(budgetPolicyJSON),
		nullBytes(gatePolicyJSON),
		modelCallsJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to update run: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run not found: %s", run.ID)
	}

	return nil
}

// AddEvent persists an event and updates the run state.
func (s *PostgresStore) AddEvent(ctx context.Context, event events.Event) error {
	var payload events.StagePayload
	json.Unmarshal(event.Payload, &payload)

	query := `
		INSERT INTO run_events (id, run_id, type, timestamp, stage_name, stage_index, payload_json)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err := s.db.ExecContext(ctx, query,
		event.ID,
		event.RunID,
		string(event.Type),
		event.Timestamp,
		nullString(payload.StageName),
		nullInt(payload.StageIndex),
		event.Payload,
	)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	// Update run state based on event
	return s.updateRunFromEvent(ctx, event)
}

// updateRunFromEvent updates run state based on an event.
func (s *PostgresStore) updateRunFromEvent(ctx context.Context, event events.Event) error {
	run, found, err := s.GetRun(ctx, event.RunID)
	if err != nil || !found {
		return err
	}

	var payload events.StagePayload
	json.Unmarshal(event.Payload, &payload)

	run.UpdatedAt = event.Timestamp

	switch event.Type {
	case events.EventTypeStageStarted:
		run.CurrentStage = payload.StageName
		if payload.StageIndex < len(run.Stages) {
			run.Stages[payload.StageIndex].State = contracts.StageStateRunning
			run.Stages[payload.StageIndex].StartedAt = &event.Timestamp
		}
	case events.EventTypeStageCompleted:
		if payload.StageIndex < len(run.Stages) {
			run.Stages[payload.StageIndex].State = contracts.StageStateCompleted
			run.Stages[payload.StageIndex].CompletedAt = &event.Timestamp
		}
		// Check if all stages completed
		allDone := true
		for _, stage := range run.Stages {
			if stage.State != contracts.StageStateCompleted {
				allDone = false
				break
			}
		}
		if allDone {
			run.Status = contracts.RunStatusCompleted
			run.CompletedAt = &event.Timestamp
			run.CurrentStage = ""
		}
	case events.EventTypeStageFailed:
		if payload.StageIndex < len(run.Stages) {
			run.Stages[payload.StageIndex].State = contracts.StageStateFailed
			run.Stages[payload.StageIndex].Error = payload.Error
		}
		run.Status = contracts.RunStatusFailed
		run.Error = payload.Error
	}

	return s.UpdateRun(ctx, run)
}

// ListEvents returns events for a run.
func (s *PostgresStore) ListEvents(ctx context.Context, runID string, limit int) ([]events.Event, error) {
	if limit <= 0 {
		limit = 1000
	}

	query := `
		SELECT id, run_id, type, timestamp, payload_json
		FROM run_events
		WHERE run_id = $1
		ORDER BY timestamp ASC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var evts []events.Event
	for rows.Next() {
		var evt events.Event
		err := rows.Scan(&evt.ID, &evt.RunID, &evt.Type, &evt.Timestamp, &evt.Payload)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		evts = append(evts, evt)
	}

	return evts, rows.Err()
}

// AddGateResult adds or replaces a gate result for a run.
// Uses replace semantics: if a gate with the same level+name exists, it is replaced.
func (s *PostgresStore) AddGateResult(ctx context.Context, runID string, result contracts.GateResult) error {
	run, found, err := s.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("run not found: %s", runID)
	}

	// Normalize gate name using helper
	result.Name = contracts.NormalizeGateName(result.Name)

	// Replace existing gate with same level+name, or append if new
	replaced := false
	for i, g := range run.Gates {
		gName := contracts.NormalizeGateName(g.Name)
		if g.Level == result.Level && gName == result.Name {
			run.Gates[i] = result
			replaced = true
			break
		}
	}
	if !replaced {
		run.Gates = append(run.Gates, result)
	}

	run.UpdatedAt = time.Now().UTC()
	return s.UpdateRun(ctx, run)
}

// AddGateResultHistory appends a gate result to the history table.
// Deprecated: Use AddGateHistoryItem for full lineage support.
func (s *PostgresStore) AddGateResultHistory(ctx context.Context, runID string, id string, result contracts.GateResult) error {
	item := contracts.GateHistoryItem{
		ID:     id,
		Result: result,
	}
	return s.AddGateHistoryItem(ctx, runID, item)
}

// AddGateHistoryItem appends a gate history item with full lineage support.
func (s *PostgresStore) AddGateHistoryItem(ctx context.Context, runID string, item contracts.GateHistoryItem) error {
	// Normalize empty gate name
	name := contracts.NormalizeGateName(item.Result.Name)
	item.Result.Name = name

	// Serialize full result to JSON
	resultJSON, err := json.Marshal(item.Result)
	if err != nil {
		return fmt.Errorf("failed to marshal gate result: %w", err)
	}

	query := `
		INSERT INTO run_gate_results (
			id, run_id, level, name, passed, executor,
			started_at, completed_at, timestamp, duration_ms,
			evidence_path, error, result, parent_execution_id, retry_reason
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`

	var parentExecID sql.NullString
	if item.ParentExecutionID != nil && *item.ParentExecutionID != "" {
		parentExecID = sql.NullString{String: *item.ParentExecutionID, Valid: true}
	}

	_, err = s.db.ExecContext(ctx, query,
		item.ID,
		runID,
		item.Result.Level,
		name,
		item.Result.Passed,
		item.Result.Executor,
		item.Result.StartedAt,
		item.Result.CompletedAt,
		item.Result.Timestamp,
		item.Result.DurationMs,
		item.Result.EvidencePath,
		item.Result.Error,
		resultJSON,
		parentExecID,
		nullString(item.RetryReason),
	)
	if err != nil {
		return fmt.Errorf("failed to insert gate history: %w", err)
	}

	return nil
}

// ListGateHistory returns gate history for a run ordered by timestamp desc.
func (s *PostgresStore) ListGateHistory(ctx context.Context, runID string, limit int) ([]contracts.GateHistoryItem, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, result, parent_execution_id, retry_reason
		FROM run_gate_results
		WHERE run_id = $1
		ORDER BY timestamp DESC
		LIMIT $2
	`

	rows, err := s.db.QueryContext(ctx, query, runID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query gate history: %w", err)
	}
	defer rows.Close()

	var items []contracts.GateHistoryItem
	for rows.Next() {
		var item contracts.GateHistoryItem
		var resultJSON []byte
		var parentExecID, retryReason sql.NullString

		if err := rows.Scan(&item.ID, &resultJSON, &parentExecID, &retryReason); err != nil {
			return nil, fmt.Errorf("failed to scan gate history: %w", err)
		}

		if err := json.Unmarshal(resultJSON, &item.Result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal gate result: %w", err)
		}

		// Normalize gate name for backward compatibility
		item.Result.Name = contracts.NormalizeGateName(item.Result.Name)

		if parentExecID.Valid && parentExecID.String != "" {
			item.ParentExecutionID = &parentExecID.String
		}
		item.RetryReason = retryReason.String

		items = append(items, item)
	}

	return items, rows.Err()
}

// GetLatestGates returns the latest gate result per (level, name).
func (s *PostgresStore) GetLatestGates(ctx context.Context, runID string) ([]contracts.GateResult, error) {
	query := `
		SELECT DISTINCT ON (level, name) result
		FROM run_gate_results
		WHERE run_id = $1
		ORDER BY level, name, timestamp DESC
	`

	rows, err := s.db.QueryContext(ctx, query, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to query latest gates: %w", err)
	}
	defer rows.Close()

	var results []contracts.GateResult
	for rows.Next() {
		var resultJSON []byte
		if err := rows.Scan(&resultJSON); err != nil {
			return nil, fmt.Errorf("failed to scan latest gate: %w", err)
		}

		var result contracts.GateResult
		if err := json.Unmarshal(resultJSON, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal gate result: %w", err)
		}

		results = append(results, result)
	}

	return results, rows.Err()
}

// GetLatestExecution returns the latest execution_id for a specific gate (level+name).
func (s *PostgresStore) GetLatestExecution(ctx context.Context, runID, level, name string) (string, error) {
	name = contracts.NormalizeGateName(name)

	query := `
		SELECT result->>'execution_id'
		FROM run_gate_results
		WHERE run_id = $1 AND level = $2 AND name = $3
		ORDER BY timestamp DESC
		LIMIT 1
	`

	var execID sql.NullString
	err := s.db.QueryRowContext(ctx, query, runID, level, name).Scan(&execID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to query latest execution: %w", err)
	}

	return execID.String, nil
}

// AddModelCall appends a model call summary to a run.
func (s *PostgresStore) AddModelCall(ctx context.Context, runID string, call contracts.ModelCallSummary) error {
	run, found, err := s.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("run not found: %s", runID)
	}

	run.ModelCalls = append(run.ModelCalls, call)
	// Truncate oldest if exceeding max
	if len(run.ModelCalls) > MaxModelCalls {
		run.ModelCalls = run.ModelCalls[len(run.ModelCalls)-MaxModelCalls:]
	}
	run.UpdatedAt = time.Now().UTC()
	return s.UpdateRun(ctx, run)
}

// UpdateBudgetStatus updates the budget status for a run.
func (s *PostgresStore) UpdateBudgetStatus(ctx context.Context, runID string, status contracts.BudgetStatus) error {
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal budget status: %w", err)
	}

	query := `UPDATE runs SET budget_status_json = $2, updated_at = $3 WHERE id = $1`
	_, err = s.db.ExecContext(ctx, query, runID, statusJSON, time.Now().UTC())
	return err
}

// GetBudgetStatus retrieves the budget status for a run.
func (s *PostgresStore) GetBudgetStatus(ctx context.Context, runID string) (*contracts.BudgetStatus, error) {
	query := `SELECT budget_status_json FROM runs WHERE id = $1`

	var budgetStatusJSON sql.NullString
	err := s.db.QueryRowContext(ctx, query, runID).Scan(&budgetStatusJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if !budgetStatusJSON.Valid || budgetStatusJSON.String == "" {
		return nil, nil
	}

	var status contracts.BudgetStatus
	if err := json.Unmarshal([]byte(budgetStatusJSON.String), &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// UpdateGatePolicy updates the gate policy for a run.
func (s *PostgresStore) UpdateGatePolicy(ctx context.Context, runID string, policy *contracts.GatePolicy) error {
	var policyJSON []byte
	var err error
	if policy != nil {
		policyJSON, err = json.Marshal(policy)
		if err != nil {
			return fmt.Errorf("failed to marshal gate policy: %w", err)
		}
	}

	query := `UPDATE runs SET gate_policy_json = $2, updated_at = $3 WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, runID, nullBytes(policyJSON), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to update gate policy: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}

	return nil
}

// GetGatePolicy retrieves the gate policy for a run.
func (s *PostgresStore) GetGatePolicy(ctx context.Context, runID string) (*contracts.GatePolicy, error) {
	query := `SELECT gate_policy_json FROM runs WHERE id = $1`

	var gatePolicyJSON sql.NullString
	err := s.db.QueryRowContext(ctx, query, runID).Scan(&gatePolicyJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if !gatePolicyJSON.Valid || gatePolicyJSON.String == "" || gatePolicyJSON.String == "{}" {
		return nil, nil
	}

	var policy contracts.GatePolicy
	if err := json.Unmarshal([]byte(gatePolicyJSON.String), &policy); err != nil {
		return nil, err
	}

	return &policy, nil
}

// FinalizeRun marks a run as finalized with capsule metadata.
func (s *PostgresStore) FinalizeRun(ctx context.Context, runID string, params FinalizeParams) error {
	// First check current state
	run, found, err := s.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("run not found: %s", runID)
	}

	// Check if already finalized (idempotent)
	if run.IsFinalized() {
		// Already finalized - return success for idempotency
		return nil
	}

	// Check if run is completed
	if run.Status != contracts.RunStatusCompleted {
		return fmt.Errorf("run must be completed to finalize (current status: %s)", run.Status)
	}

	query := `
		UPDATE runs SET
			status = $2,
			finalized_at = $3,
			finalized_by = $4,
			finalize_reason = $5,
			finalize_override = $6,
			finalize_override_reason = $7,
			capsule_id = $8,
			capsule_path = $9,
			capsule_manifest_sha256 = $10,
			updated_at = $11
		WHERE id = $1 AND status = 'completed'
	`

	result, err := s.db.ExecContext(ctx, query,
		runID,
		string(contracts.RunStatusFinalized),
		params.FinalizedAt,
		nullString(params.FinalizedBy),
		nullString(params.FinalizeReason),
		params.FinalizeOverride,
		nullString(params.FinalizeOverrideReason),
		nullString(params.CapsuleID),
		nullString(params.CapsulePath),
		nullString(params.CapsuleManifestSHA256),
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to finalize run: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		// Re-check state - might have been finalized by another request
		run, _, err = s.GetRun(ctx, runID)
		if err != nil {
			return err
		}
		if run.IsFinalized() {
			return nil // Idempotent success
		}
		return fmt.Errorf("run is no longer in completed state")
	}

	return nil
}

// IsRunFinalized checks if a run is finalized.
func (s *PostgresStore) IsRunFinalized(ctx context.Context, runID string) (bool, error) {
	query := `SELECT status, finalized_at FROM runs WHERE id = $1`

	var status string
	var finalizedAt sql.NullTime
	err := s.db.QueryRowContext(ctx, query, runID).Scan(&status, &finalizedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return status == string(contracts.RunStatusFinalized) || finalizedAt.Valid, nil
}

// ListRunsByTenant returns runs filtered by tenant/project, ordered by updated_at desc.
func (s *PostgresStore) ListRunsByTenant(ctx context.Context, tenantID, projectID string, limit int) ([]*contracts.WorkflowRun, error) {
	if limit <= 0 {
		limit = 100
	}

	// Build query based on provided filters
	query := `
		SELECT id, temporal_id, status, current_stage, created_at, updated_at,
		       completed_at, error, stages_json, gates_json, budget_policy_json,
		       gate_policy_json, model_calls_json, tenant_id, project_id
		FROM runs
		WHERE ($1 = '' OR tenant_id = $1)
		  AND ($2 = '' OR project_id = $2)
		ORDER BY updated_at DESC
		LIMIT $3
	`

	rows, err := s.db.QueryContext(ctx, query, tenantID, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query runs by tenant: %w", err)
	}
	defer rows.Close()

	var runs []*contracts.WorkflowRun
	for rows.Next() {
		run := &contracts.WorkflowRun{}
		var temporalID, currentStage, errorStr sql.NullString
		var completedAt sql.NullTime
		var stagesJSON, gatesJSON, modelCallsJSON []byte
		var budgetPolicyJSON, gatePolicyJSON sql.NullString
		var tenantIDVal, projectIDVal sql.NullString

		err := rows.Scan(
			&run.ID,
			&temporalID,
			&run.Status,
			&currentStage,
			&run.CreatedAt,
			&run.UpdatedAt,
			&completedAt,
			&errorStr,
			&stagesJSON,
			&gatesJSON,
			&budgetPolicyJSON,
			&gatePolicyJSON,
			&modelCallsJSON,
			&tenantIDVal,
			&projectIDVal,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan run: %w", err)
		}

		run.TemporalID = temporalID.String
		run.CurrentStage = currentStage.String
		run.Error = errorStr.String
		run.TenantID = tenantIDVal.String
		run.ProjectID = projectIDVal.String
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}

		if err := json.Unmarshal(stagesJSON, &run.Stages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal stages: %w", err)
		}
		if len(gatesJSON) > 0 {
			json.Unmarshal(gatesJSON, &run.Gates)
		}
		if budgetPolicyJSON.Valid {
			run.BudgetPolicy = &contracts.BudgetPolicy{}
			json.Unmarshal([]byte(budgetPolicyJSON.String), run.BudgetPolicy)
		}
		if gatePolicyJSON.Valid && gatePolicyJSON.String != "" && gatePolicyJSON.String != "{}" {
			run.GatePolicy = &contracts.GatePolicy{}
			json.Unmarshal([]byte(gatePolicyJSON.String), run.GatePolicy)
		}
		if len(modelCallsJSON) > 0 {
			json.Unmarshal(modelCallsJSON, &run.ModelCalls)
		}

		runs = append(runs, run)
	}

	return runs, rows.Err()
}

// SetGatesRunning sets the gates_running flag on a run (for finalization safety).
func (s *PostgresStore) SetGatesRunning(ctx context.Context, runID string, running bool) error {
	query := `UPDATE runs SET gates_running = $2, updated_at = $3 WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, runID, running, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to set gates_running: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}

	return nil
}

// UpdatePolicyDecision updates policy decision and snapshots the policy at evaluation time.
func (s *PostgresStore) UpdatePolicyDecision(ctx context.Context, runID string, decision *contracts.PolicyDecision, snapshot *contracts.GatePolicy) error {
	var decisionJSON, snapshotJSON []byte
	var err error

	if decision != nil {
		decisionJSON, err = json.Marshal(decision)
		if err != nil {
			return fmt.Errorf("failed to marshal policy decision: %w", err)
		}
	}

	if snapshot != nil {
		snapshotJSON, err = json.Marshal(snapshot)
		if err != nil {
			return fmt.Errorf("failed to marshal policy snapshot: %w", err)
		}
	}

	query := `UPDATE runs SET policy_decision = $2, policy_snapshot = $3, updated_at = $4 WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, runID, nullBytes(decisionJSON), nullBytes(snapshotJSON), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to update policy decision: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("run not found: %s", runID)
	}

	return nil
}

// Helper functions for nullable types
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i int) sql.NullInt32 {
	return sql.NullInt32{Int32: int32(i), Valid: true}
}

func nullBytes(b []byte) sql.NullString {
	if len(b) == 0 {
		return sql.NullString{}
	}
	return sql.NullString{String: string(b), Valid: true}
}
