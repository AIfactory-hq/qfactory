// Package store provides persistence abstractions for qfactory.
package store

import (
	"context"
	"sync"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

// Store defines the persistence interface for workflow runs and events.
type Store interface {
	// CreateRun persists a new workflow run.
	CreateRun(ctx context.Context, run *contracts.WorkflowRun) error

	// GetRun retrieves a run by ID. Returns (nil, false, nil) if not found.
	GetRun(ctx context.Context, id string) (*contracts.WorkflowRun, bool, error)

	// ListRuns returns runs ordered by updated_at desc, limited to limit rows.
	ListRuns(ctx context.Context, limit int) ([]*contracts.WorkflowRun, error)

	// UpdateRun updates an existing run. Returns error if run doesn't exist.
	UpdateRun(ctx context.Context, run *contracts.WorkflowRun) error

	// AddEvent persists an event and updates the run state.
	AddEvent(ctx context.Context, event events.Event) error

	// ListEvents returns events for a run ordered by timestamp, limited to limit rows.
	ListEvents(ctx context.Context, runID string, limit int) ([]events.Event, error)

	// AddGateResult updates the latest view of a gate result (replace by level+name).
	AddGateResult(ctx context.Context, runID string, result contracts.GateResult) error

	// AddGateResultHistory appends a gate result to the history table.
	// Deprecated: Use AddGateHistoryItem for full lineage support.
	AddGateResultHistory(ctx context.Context, runID string, id string, result contracts.GateResult) error

	// AddGateHistoryItem appends a gate history item with full lineage support.
	AddGateHistoryItem(ctx context.Context, runID string, item contracts.GateHistoryItem) error

	// ListGateHistory returns gate history for a run ordered by timestamp desc.
	ListGateHistory(ctx context.Context, runID string, limit int) ([]contracts.GateHistoryItem, error)

	// GetLatestExecution returns the latest execution_id for a specific gate (level+name).
	GetLatestExecution(ctx context.Context, runID, level, name string) (string, error)

	// GetLatestGates returns the latest gate result per (level, name).
	GetLatestGates(ctx context.Context, runID string) ([]contracts.GateResult, error)

	// AddModelCall appends a model call summary to a run (bounded, truncates oldest).
	AddModelCall(ctx context.Context, runID string, call contracts.ModelCallSummary) error

	// UpdateBudgetStatus updates the budget status for a run.
	UpdateBudgetStatus(ctx context.Context, runID string, status contracts.BudgetStatus) error

	// GetBudgetStatus retrieves the budget status for a run.
	GetBudgetStatus(ctx context.Context, runID string) (*contracts.BudgetStatus, error)

	// UpdateGatePolicy updates the gate policy for a run.
	UpdateGatePolicy(ctx context.Context, runID string, policy *contracts.GatePolicy) error

	// GetGatePolicy retrieves the gate policy for a run.
	GetGatePolicy(ctx context.Context, runID string) (*contracts.GatePolicy, error)

	// FinalizeRun marks a run as finalized with capsule metadata.
	// Returns error if run is not completed or already finalized.
	FinalizeRun(ctx context.Context, runID string, params FinalizeParams) error

	// IsRunFinalized checks if a run is finalized.
	IsRunFinalized(ctx context.Context, runID string) (bool, error)

	// v1.0: Tenant isolation methods

	// ListRunsByTenant returns runs filtered by tenant/project, ordered by updated_at desc.
	ListRunsByTenant(ctx context.Context, tenantID, projectID string, limit int) ([]*contracts.WorkflowRun, error)

	// SetGatesRunning sets the gates_running flag on a run (for finalization safety).
	SetGatesRunning(ctx context.Context, runID string, running bool) error

	// UpdatePolicyDecision updates policy decision and snapshots the policy at evaluation time.
	UpdatePolicyDecision(ctx context.Context, runID string, decision *contracts.PolicyDecision, snapshot *contracts.GatePolicy) error

	// Close releases any resources held by the store.
	Close() error
}

// FinalizeParams contains parameters for finalizing a run.
type FinalizeParams struct {
	FinalizedAt           time.Time
	FinalizedBy           string
	FinalizeReason        string
	FinalizeOverride      bool
	FinalizeOverrideReason string
	CapsuleID             string
	CapsulePath           string
	CapsuleManifestSHA256 string
}

// SSEHub manages Server-Sent Events subscriptions in-memory.
// Events are persisted to DB but SSE notifications use this hub.
type SSEHub struct {
	mu          sync.RWMutex
	subscribers map[string][]chan events.Event
}

// NewSSEHub creates a new SSE hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{
		subscribers: make(map[string][]chan events.Event),
	}
}

// Subscribe creates a channel for receiving events for a run.
func (h *SSEHub) Subscribe(runID string) chan events.Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan events.Event, 100)
	h.subscribers[runID] = append(h.subscribers[runID], ch)
	return ch
}

// Publish sends an event to all subscribers for a run.
func (h *SSEHub) Publish(runID string, event events.Event) {
	h.mu.RLock()
	chans := h.subscribers[runID]
	h.mu.RUnlock()

	for _, ch := range chans {
		select {
		case ch <- event:
		default:
			// Drop if channel is full
		}
	}
}

// Unsubscribe removes a channel from subscribers.
func (h *SSEHub) Unsubscribe(runID string, ch chan events.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	chans := h.subscribers[runID]
	for i, c := range chans {
		if c == ch {
			h.subscribers[runID] = append(chans[:i], chans[i+1:]...)
			close(ch)
			return
		}
	}
}
