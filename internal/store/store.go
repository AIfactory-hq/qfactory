// Package store provides persistence abstractions for qfactory.
package store

import (
	"context"

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

	// AddGateResult appends a gate result to a run.
	AddGateResult(ctx context.Context, runID string, result contracts.GateResult) error

	// AddModelCall appends a model call summary to a run (bounded, truncates oldest).
	AddModelCall(ctx context.Context, runID string, call contracts.ModelCallSummary) error

	// UpdateBudgetStatus updates the budget status for a run.
	UpdateBudgetStatus(ctx context.Context, runID string, status contracts.BudgetStatus) error

	// GetBudgetStatus retrieves the budget status for a run.
	GetBudgetStatus(ctx context.Context, runID string) (*contracts.BudgetStatus, error)

	// Close releases any resources held by the store.
	Close() error
}

// SSEHub manages Server-Sent Events subscriptions in-memory.
// Events are persisted to DB but SSE notifications use this hub.
type SSEHub struct {
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
	ch := make(chan events.Event, 100)
	h.subscribers[runID] = append(h.subscribers[runID], ch)
	return ch
}

// Publish sends an event to all subscribers for a run.
func (h *SSEHub) Publish(runID string, event events.Event) {
	for _, ch := range h.subscribers[runID] {
		select {
		case ch <- event:
		default:
			// Drop if channel is full
		}
	}
}

// Unsubscribe removes a channel from subscribers.
func (h *SSEHub) Unsubscribe(runID string, ch chan events.Event) {
	chans := h.subscribers[runID]
	for i, c := range chans {
		if c == ch {
			h.subscribers[runID] = append(chans[:i], chans[i+1:]...)
			close(ch)
			return
		}
	}
}
