// Package contracts defines shared types for qfactory APIs.
package contracts

import (
	"errors"
	"time"
)

// GateLevel constants for quality gates.
const (
	GateLevelPR0 = "PR0"
	GateLevelPR1 = "PR1"
	GateLevelPR2 = "PR2"
	GateLevelPR3 = "PR3"
)

// ValidGateLevels contains all valid gate levels.
var ValidGateLevels = []string{GateLevelPR0, GateLevelPR1, GateLevelPR2, GateLevelPR3}

// RunStatus represents the status of a workflow run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
)

// StageState represents the state of a workflow stage.
type StageState string

const (
	StageStatePending   StageState = "pending"
	StageStateRunning   StageState = "running"
	StageStateCompleted StageState = "completed"
	StageStateFailed    StageState = "failed"
	StageStateSkipped   StageState = "skipped"
)

// WorkflowRequest is the request body for creating a new workflow.
type WorkflowRequest struct {
	Mode        string            `json:"mode,omitempty"`
	Blueprint   string            `json:"blueprint,omitempty"`
	Prompt      string            `json:"prompt,omitempty"`
	ProjectID   string            `json:"project_id,omitempty"`
	Type        string            `json:"type,omitempty"`
	Requirement string            `json:"requirement,omitempty"`
	Constraints map[string]string `json:"constraints,omitempty"`
	Budgets     *BudgetStatus     `json:"budgets,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Validate performs minimal validation on the request.
// For v0.1, we are permissive - only check that we have some input.
func (r *WorkflowRequest) Validate() error {
	// Permissive for v0.1: accept if mode+prompt OR type+requirement provided
	hasNew := r.Mode != "" || r.Prompt != ""
	hasLegacy := r.Type != "" || r.Requirement != ""
	if !hasNew && !hasLegacy {
		return errors.New("request must include mode/prompt or type/requirement")
	}
	return nil
}

// WorkflowRun represents a workflow execution instance.
type WorkflowRun struct {
	ID           string        `json:"id"`
	TemporalID   string        `json:"temporal_id,omitempty"`
	Status       RunStatus     `json:"status"`
	CurrentStage string        `json:"current_stage,omitempty"`
	Stages       []StageStatus `json:"stages"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	Error        string        `json:"error,omitempty"`
	Gates        []GateResult  `json:"gates,omitempty"`
}

// StageStatus represents the status of a single stage.
type StageStatus struct {
	Name        string     `json:"name"`
	State       StageState `json:"state"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// GateResult represents the result of a quality gate check.
type GateResult struct {
	Level        string    `json:"level"` // PR0, PR1, PR2, PR3
	Passed       bool      `json:"passed"`
	Checks       []Check   `json:"checks"`
	Timestamp    time.Time `json:"timestamp"`
	DurationMs   int64     `json:"duration_ms"`
	EvidencePath string    `json:"evidence_path,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// Check represents a single check within a gate.
type Check struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// GateEvidence holds raw evidence from a gate execution.
type GateEvidence struct {
	Level       string            `json:"level"`
	Command     string            `json:"command"`
	StartedAt   time.Time         `json:"started_at"`
	EndedAt     time.Time         `json:"ended_at"`
	ExitCode    int               `json:"exit_code"`
	DurationMs  int64             `json:"duration_ms"`
	Stdout      string            `json:"stdout"`
	Stderr      string            `json:"stderr"`
	GoVersion   string            `json:"go_version,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

// EvidenceDir is the base directory for evidence bundles.
const EvidenceDir = "evidence"

// ArtifactRef references an artifact produced by a workflow.
type ArtifactRef struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Path        string    `json:"path"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
}

// BudgetStatus tracks resource consumption.
type BudgetStatus struct {
	TokensUsed      int `json:"tokens_used"`
	TokensRemaining int `json:"tokens_remaining"`
	TimeUsedMs      int `json:"time_used_ms"`
	TimeRemainingMs int `json:"time_remaining_ms"`
}

// DemoWorkflowStages defines the stages for the demo workflow.
var DemoWorkflowStages = []string{
	"spec",
	"contract",
	"plan",
	"implement",
	"verify",
}

// EvidenceManifest describes an evidence bundle.
type EvidenceManifest struct {
	RunID       string       `json:"run_id"`
	TemporalID  string       `json:"temporal_id,omitempty"`
	CreatedAt   time.Time    `json:"created_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
	Status      RunStatus    `json:"status"`
	Gates       []GateResult `json:"gates,omitempty"`
	Files       []string     `json:"files"`
	GeneratedAt time.Time    `json:"generated_at"`
	Version     string       `json:"version"`
}
