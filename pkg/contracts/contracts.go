// Package contracts defines shared types for qfactory APIs.
package contracts

import (
	"errors"
	"strings"
	"time"
)

// MaxRetryReasonLength is the maximum length for retry reason strings.
const MaxRetryReasonLength = 256

// NormalizeGateName normalizes a gate name.
// Empty names become "default", and whitespace is trimmed.
func NormalizeGateName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "default"
	}
	return name
}

// TruncateRetryReason truncates a retry reason to MaxRetryReasonLength.
func TruncateRetryReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > MaxRetryReasonLength {
		return reason[:MaxRetryReasonLength]
	}
	return reason
}

// GateLevel constants for quality gates.
const (
	GateLevelPR0 = "PR0"
	GateLevelPR1 = "PR1"
	GateLevelPR2 = "PR2"
	GateLevelPR3 = "PR3"
)

// ValidGateLevels contains all valid gate levels.
var ValidGateLevels = []string{GateLevelPR0, GateLevelPR1, GateLevelPR2, GateLevelPR3}

// ModelProviderType represents the type of model provider.
type ModelProviderType string

const (
	ModelProviderOllama ModelProviderType = "ollama"
	ModelProviderCloud  ModelProviderType = "cloud"
	ModelProviderMock   ModelProviderType = "mock"
)

// RunStatus represents the status of a workflow run.
type RunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusFinalized RunStatus = "finalized"
)

// DefaultTrustThreshold is the minimum trust score required for finalization.
const DefaultTrustThreshold = 80

// v1.0: RBAC Roles
type Role string

const (
	RoleViewer   Role = "viewer"   // read runs, gates, evidence, capsules
	RoleOperator Role = "operator" // run gates (PR1-PR3)
	RoleApprover Role = "approver" // finalize runs
	RoleAdmin    Role = "admin"    // override trust + manage policies
)

// ValidRoles contains all valid roles.
var ValidRoles = []Role{RoleViewer, RoleOperator, RoleApprover, RoleAdmin}

// IsValidRole checks if a role string is valid.
func IsValidRole(r string) bool {
	for _, valid := range ValidRoles {
		if string(valid) == r {
			return true
		}
	}
	return false
}

// RoleHasPermission checks if a role has a specific permission.
func RoleHasPermission(role Role, permission Permission) bool {
	perms, ok := RolePermissions[role]
	if !ok {
		return false
	}
	for _, p := range perms {
		if p == permission {
			return true
		}
	}
	return false
}

// Permission represents an action that can be performed.
type Permission string

const (
	PermissionRead        Permission = "read"
	PermissionRunGates    Permission = "run_gates"
	PermissionFinalize    Permission = "finalize"
	PermissionOverride    Permission = "override"
	PermissionManagePolicy Permission = "manage_policy"
)

// RolePermissions maps roles to their permissions.
var RolePermissions = map[Role][]Permission{
	RoleViewer:   {PermissionRead},
	RoleOperator: {PermissionRead, PermissionRunGates},
	RoleApprover: {PermissionRead, PermissionRunGates, PermissionFinalize},
	RoleAdmin:    {PermissionRead, PermissionRunGates, PermissionFinalize, PermissionOverride, PermissionManagePolicy},
}

// v1.0: Structured Error Codes
type ErrorCode string

const (
	ErrCodeForbiddenRole     ErrorCode = "ERR_FORBIDDEN_ROLE"
	ErrCodePolicyViolation   ErrorCode = "ERR_POLICY_VIOLATION"
	ErrCodeAlreadyFinalized  ErrorCode = "ERR_ALREADY_FINALIZED"
	ErrCodeTenantMismatch    ErrorCode = "ERR_TENANT_MISMATCH"
	ErrCodeGatesRunning      ErrorCode = "ERR_GATES_RUNNING"
	ErrCodeLimitExceeded     ErrorCode = "ERR_LIMIT_EXCEEDED"
	ErrCodeInvalidRequest    ErrorCode = "ERR_INVALID_REQUEST"
)

// APIError represents a structured API error response.
type APIError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Details string    `json:"details,omitempty"`
}

// v1.0: Operational Limits
type OperationalLimits struct {
	MaxGateRuntimeSeconds int   `json:"max_gate_runtime_seconds"` // default 600 (10 min)
	MaxRetriesPerGate     int   `json:"max_retries_per_gate"`     // default 3
	MaxCapsuleSizeBytes   int64 `json:"max_capsule_size_bytes"`   // default 100MB
}

// DefaultOperationalLimits returns safe defaults.
func DefaultOperationalLimits() OperationalLimits {
	return OperationalLimits{
		MaxGateRuntimeSeconds: 600,
		MaxRetriesPerGate:     3,
		MaxCapsuleSizeBytes:   100 * 1024 * 1024, // 100MB
	}
}

// v1.0: Request Context carries tenant/role info
type RequestContext struct {
	TenantID  string `json:"tenant_id"`
	ProjectID string `json:"project_id"`
	Role      Role   `json:"role"`
	UserID    string `json:"user_id,omitempty"`
}

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
	ID           string             `json:"id"`
	TemporalID   string             `json:"temporal_id,omitempty"`
	Status       RunStatus          `json:"status"`
	CurrentStage string             `json:"current_stage,omitempty"`
	Stages       []StageStatus      `json:"stages"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	CompletedAt  *time.Time         `json:"completed_at,omitempty"`
	Error        string             `json:"error,omitempty"`
	Gates        []GateResult       `json:"gates,omitempty"`
	GateHistory  []GateHistoryItem  `json:"gate_history,omitempty"`
	GatePolicy   *GatePolicy        `json:"gate_policy,omitempty"`
	BudgetPolicy *BudgetPolicy      `json:"budget_policy,omitempty"`
	ModelCalls   []ModelCallSummary `json:"model_calls,omitempty"`
	// Tenancy fields (v1.0)
	TenantID  string `json:"tenant_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	// Policy tracking (v1.0)
	PolicyDecision *PolicyDecision `json:"policy_decision,omitempty"`
	PolicySnapshot *GatePolicy     `json:"policy_snapshot,omitempty"`
	GatesRunning   bool            `json:"gates_running,omitempty"`
	// Finalization fields (v0.9)
	FinalizedAt            *time.Time `json:"finalized_at,omitempty"`
	FinalizedBy            string     `json:"finalized_by,omitempty"`
	FinalizeReason         string     `json:"finalize_reason,omitempty"`
	FinalizeOverride       bool       `json:"finalize_override,omitempty"`
	FinalizeOverrideReason string     `json:"finalize_override_reason,omitempty"`
	CapsuleID              string     `json:"capsule_id,omitempty"`
	CapsulePath            string     `json:"capsule_path,omitempty"`
	CapsuleManifestSHA256  string     `json:"capsule_manifest_sha256,omitempty"`
}

// IsFinalized returns true if the run has been finalized.
func (r *WorkflowRun) IsFinalized() bool {
	return r.Status == RunStatusFinalized || r.FinalizedAt != nil
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
	Level        string     `json:"level"` // PR0, PR1, PR2, PR3
	Name         string     `json:"name,omitempty"`
	ExecutionID  string     `json:"execution_id,omitempty"` // unique ID for this execution
	Passed       bool       `json:"passed"`
	Checks       []Check    `json:"checks"`
	Timestamp    time.Time  `json:"timestamp"`
	DurationMs   int64      `json:"duration_ms"`
	Executor     string     `json:"executor,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	EvidencePath string     `json:"evidence_path,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// Check represents a single check within a gate.
type Check struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Message string `json:"message,omitempty"`
}

// GateHistoryItem wraps a gate result with its history ID and lineage.
type GateHistoryItem struct {
	ID                string     `json:"id"`
	Result            GateResult `json:"result"`
	ParentExecutionID *string    `json:"parent_execution_id,omitempty"`
	RetryReason       string     `json:"retry_reason,omitempty"`
}

// GateRef references a specific gate by level and name.
type GateRef struct {
	Level string `json:"level"`
	Name  string `json:"name"`
}

// GatePolicy defines requirements for gates to pass before a run is considered complete.
type GatePolicy struct {
	RequiredLevels    []string  `json:"required_levels,omitempty"`    // e.g. ["PR1","PR2","PR3"]
	RequiredGates     []GateRef `json:"required_gates,omitempty"`     // specific level+name pairs
	MaxAgeSeconds     int64     `json:"max_age_seconds,omitempty"`    // if >0, gates must be recent
	MaxRetries        int       `json:"max_retries,omitempty"`        // optional retry limit
	FailOpen          bool      `json:"fail_open,omitempty"`          // if true, missing/stale don't block
	// v1.0 policy hardening fields
	MinTrustScore     int  `json:"min_trust_score,omitempty"`     // minimum trust score for finalization (default 80)
	AllowOverride     bool `json:"allow_override,omitempty"`      // allow admin override of trust score
	RequireAllPassed  bool `json:"require_all_passed,omitempty"`  // all required gates must pass (not just latest)
}

// PolicyDecision represents the result of evaluating a gate policy.
type PolicyDecision struct {
	Allowed bool      `json:"allowed"`
	Missing []GateRef `json:"missing,omitempty"`
	Stale   []GateRef `json:"stale,omitempty"`
	Failing []GateRef `json:"failing,omitempty"`
	Message string    `json:"message,omitempty"`
}

// TrustIndex represents a quality score for a workflow run.
type TrustIndex struct {
	Score     int            `json:"score"`     // 0-100
	Grade     string         `json:"grade"`     // A-F
	Breakdown TrustBreakdown `json:"breakdown"`
}

// TrustBreakdown provides detail on trust index calculation.
type TrustBreakdown struct {
	PassedRequired            int `json:"passed_required"`
	Failed                    int `json:"failed"`
	Stale                     int `json:"stale"`
	Missing                   int `json:"missing"`
	ConsecutiveFailurePenalty int `json:"consecutive_failure_penalty,omitempty"`
	FlakyPenalty              int `json:"flaky_penalty,omitempty"`
	RecoveryReward            int `json:"recovery_reward,omitempty"`
}

// GateEvidence holds raw evidence from a gate execution.
type GateEvidence struct {
	Level       string            `json:"level"`
	Name        string            `json:"name,omitempty"`
	ExecutionID string            `json:"execution_id,omitempty"`
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
	TokensUsed        int `json:"tokens_used"`
	TokensRemaining   int `json:"tokens_remaining"`
	RequestsUsed      int `json:"requests_used"`
	RequestsRemaining int `json:"requests_remaining"`
	TimeUsedMs        int `json:"time_used_ms"`
	TimeRemainingMs   int `json:"time_remaining_ms"`
}

// BudgetPolicy defines limits for model usage.
type BudgetPolicy struct {
	MaxTotalTokensPerRun   int  `json:"max_total_tokens_per_run"`
	MaxTotalRequestsPerRun int  `json:"max_total_requests_per_run"`
	MaxRequestTokens       int  `json:"max_request_tokens"`
	MaxLatencyMs           int  `json:"max_latency_ms"`
	FailOpen               bool `json:"fail_open"`
}

// BudgetDecision represents the result of a budget check.
type BudgetDecision struct {
	Allowed           bool   `json:"allowed"`
	Reason            string `json:"reason"`
	RemainingTokens   int    `json:"remaining_tokens"`
	RemainingRequests int    `json:"remaining_requests"`
}

// CompletionRequest represents a request to a model.
type CompletionRequest struct {
	Model           string            `json:"model"`
	Prompt          string            `json:"prompt"`
	System          string            `json:"system,omitempty"`
	MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
	Temperature     float64           `json:"temperature,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// TokenUsage tracks token consumption.
type TokenUsage struct {
	InputTokens  int  `json:"input_tokens"`
	OutputTokens int  `json:"output_tokens"`
	TotalTokens  int  `json:"total_tokens"`
	Estimated    bool `json:"estimated,omitempty"`
}

// CompletionResponse represents a response from a model.
type CompletionResponse struct {
	Text      string     `json:"text"`
	Usage     TokenUsage `json:"usage"`
	Model     string     `json:"model"`
	Provider  string     `json:"provider"`
	LatencyMs int64      `json:"latency_ms"`
}

// ModelCallSummary records a single model call for auditing.
type ModelCallSummary struct {
	Timestamp    time.Time `json:"timestamp"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	TotalTokens  int       `json:"total_tokens"`
	LatencyMs    int64     `json:"latency_ms"`
	Stage        string    `json:"stage,omitempty"`
	Tool         string    `json:"tool,omitempty"`
	OK           bool      `json:"ok"`
	Error        string    `json:"error,omitempty"`
}

// HealthStatus represents the health of a model runtime.
type HealthStatus struct {
	OK        bool              `json:"ok"`
	Provider  string            `json:"provider"`
	Details   map[string]string `json:"details,omitempty"`
	CheckedAt time.Time         `json:"checked_at"`
}

// DefaultBudgetPolicy returns the safe default budget policy.
func DefaultBudgetPolicy() *BudgetPolicy {
	return &BudgetPolicy{
		MaxTotalTokensPerRun:   20000,
		MaxTotalRequestsPerRun: 50,
		MaxRequestTokens:       2000,
		MaxLatencyMs:           60000,
		FailOpen:               false,
	}
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

// Capsule types (v0.9)

// CapsuleDescriptor is the top-level descriptor for a signed capsule.
// This is stored in capsule.json within the capsule ZIP.
type CapsuleDescriptor struct {
	CapsuleID         string     `json:"capsule_id"`
	RunID             string     `json:"run_id"`
	Version           string     `json:"version"`
	GeneratedAt       time.Time  `json:"generated_at"`
	ManifestSHA256    string     `json:"manifest_sha256"`
	TrustScore        int        `json:"trust_score"`
	TrustGrade        string     `json:"trust_grade"`
	FinalizedAt       time.Time  `json:"finalized_at"`
	FinalizedBy       string     `json:"finalized_by,omitempty"`
	Override          bool       `json:"override,omitempty"`
	OverrideReason    string     `json:"override_reason,omitempty"`
	PublicKeyFingerprint string  `json:"public_key_fingerprint"`
}

// CapsuleManifest lists all files in the capsule with their hashes.
// This is stored in manifest.json within the capsule ZIP.
type CapsuleManifest struct {
	CapsuleID   string         `json:"capsule_id"`
	RunID       string         `json:"run_id"`
	GeneratedAt time.Time      `json:"generated_at"`
	Files       []CapsuleFile  `json:"files"`
	TotalSize   int64          `json:"total_size"`
	FileCount   int            `json:"file_count"`
}

// CapsuleFile describes a single file in the capsule manifest.
type CapsuleFile struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
}

// FinalizeRequest is the request body for finalizing a run.
type FinalizeRequest struct {
	TrustThreshold int    `json:"trust_threshold,omitempty"` // default 80
	Override       bool   `json:"override,omitempty"`
	OverrideReason string `json:"override_reason,omitempty"`
	Reason         string `json:"reason,omitempty"`
	FinalizedBy    string `json:"finalized_by,omitempty"`
}

// FinalizeResponse is the response from finalizing a run.
type FinalizeResponse struct {
	RunID          string     `json:"run_id"`
	Finalized      bool       `json:"finalized"`
	CapsuleID      string     `json:"capsule_id,omitempty"`
	CapsulePath    string     `json:"capsule_path,omitempty"`
	ManifestSHA256 string     `json:"manifest_sha256,omitempty"`
	FinalizedAt    *time.Time `json:"finalized_at,omitempty"`
	FinalizedBy    string     `json:"finalized_by,omitempty"`
	TrustScore     int        `json:"trust_score,omitempty"`
	TrustGrade     string     `json:"trust_grade,omitempty"`
	Override       bool       `json:"override,omitempty"`
	OverrideReason string     `json:"override_reason,omitempty"`
}

// CapsuleVerifyResult is the result of verifying a capsule.
type CapsuleVerifyResult struct {
	Valid                bool     `json:"valid"`
	Errors               []string `json:"errors,omitempty"`
	ManifestSHA256       string   `json:"manifest_sha256,omitempty"`
	CapsuleID            string   `json:"capsule_id,omitempty"`
	RunID                string   `json:"run_id,omitempty"`
	PublicKeyFingerprint string   `json:"public_key_fingerprint,omitempty"`
	FilesVerified        int      `json:"files_verified,omitempty"`
}
