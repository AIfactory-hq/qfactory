package modelruntime

import (
	"context"
	"fmt"
	"sync"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// BudgetManager tracks and enforces budget limits for model calls.
type BudgetManager struct {
	mu     sync.RWMutex
	states map[string]*BudgetState
	store  BudgetStore
}

// BudgetStore defines persistence operations for budget state.
type BudgetStore interface {
	// UpdateBudgetStatus persists budget status for a run.
	UpdateBudgetStatus(ctx context.Context, runID string, status contracts.BudgetStatus) error
	// GetBudgetStatus retrieves persisted budget status.
	GetBudgetStatus(ctx context.Context, runID string) (*contracts.BudgetStatus, error)
}

// SetStore sets the persistence store for budget state.
// This allows budget state to survive API restarts.
func (b *BudgetManager) SetStore(store BudgetStore) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.store = store
}

// BudgetState tracks budget consumption for a single run.
type BudgetState struct {
	RunID          string
	Policy         *contracts.BudgetPolicy
	TokensUsed     int
	RequestsUsed   int
	TotalLatencyMs int64
}

// NewBudgetManager creates a new budget manager.
func NewBudgetManager() *BudgetManager {
	return &BudgetManager{
		states: make(map[string]*BudgetState),
	}
}

// GetOrCreateState gets or creates budget state for a run.
func (b *BudgetManager) GetOrCreateState(runID string, policy *contracts.BudgetPolicy) *BudgetState {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.states[runID]
	if !ok && b.store != nil {
		// Try to load from persistent store
		if status, err := b.store.GetBudgetStatus(context.Background(), runID); err == nil && status != nil {
			state = &BudgetState{
				RunID:          runID,
				Policy:         policy,
				TokensUsed:     status.TokensUsed,
				RequestsUsed:   status.RequestsUsed,
				TotalLatencyMs: int64(status.TimeUsedMs),
			}
			if policy == nil {
				state.Policy = contracts.DefaultBudgetPolicy()
			}
			b.states[runID] = state
		}
	}
	if state != nil {
		// Update policy if provided
		if policy != nil {
			state.Policy = policy
		}
		return state
	}

	// Use default policy if not provided
	if policy == nil {
		policy = contracts.DefaultBudgetPolicy()
	}

	state = &BudgetState{
		RunID:  runID,
		Policy: policy,
	}
	b.states[runID] = state
	return state
}

// SetPolicy sets the budget policy for a run.
func (b *BudgetManager) SetPolicy(runID string, policy *contracts.BudgetPolicy) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if state, ok := b.states[runID]; ok {
		state.Policy = policy
	} else {
		b.states[runID] = &BudgetState{
			RunID:  runID,
			Policy: policy,
		}
	}
}

// CheckBudget checks if a request is allowed under the current budget.
func (b *BudgetManager) CheckBudget(runID string, estimatedTokens int) contracts.BudgetDecision {
	b.mu.RLock()
	defer b.mu.RUnlock()

	state, ok := b.states[runID]
	if !ok {
		// No state yet - will be created on first call
		policy := contracts.DefaultBudgetPolicy()
		return contracts.BudgetDecision{
			Allowed:           true,
			Reason:            "budget available",
			RemainingTokens:   policy.MaxTotalTokensPerRun,
			RemainingRequests: policy.MaxTotalRequestsPerRun,
		}
	}

	policy := state.Policy
	remainingTokens := policy.MaxTotalTokensPerRun - state.TokensUsed
	remainingRequests := policy.MaxTotalRequestsPerRun - state.RequestsUsed

	// Check request limit
	if state.RequestsUsed >= policy.MaxTotalRequestsPerRun {
		return contracts.BudgetDecision{
			Allowed:           policy.FailOpen,
			Reason:            fmt.Sprintf("request limit exceeded: %d/%d", state.RequestsUsed, policy.MaxTotalRequestsPerRun),
			RemainingTokens:   remainingTokens,
			RemainingRequests: 0,
		}
	}

	// Check token limit
	if state.TokensUsed >= policy.MaxTotalTokensPerRun {
		return contracts.BudgetDecision{
			Allowed:           policy.FailOpen,
			Reason:            fmt.Sprintf("token limit exceeded: %d/%d", state.TokensUsed, policy.MaxTotalTokensPerRun),
			RemainingTokens:   0,
			RemainingRequests: remainingRequests,
		}
	}

	// Check per-request token limit
	if estimatedTokens > policy.MaxRequestTokens {
		return contracts.BudgetDecision{
			Allowed:           policy.FailOpen,
			Reason:            fmt.Sprintf("request tokens exceed limit: %d > %d", estimatedTokens, policy.MaxRequestTokens),
			RemainingTokens:   remainingTokens,
			RemainingRequests: remainingRequests,
		}
	}

	return contracts.BudgetDecision{
		Allowed:           true,
		Reason:            "budget available",
		RemainingTokens:   remainingTokens,
		RemainingRequests: remainingRequests,
	}
}

// RecordUsage records token and request usage.
func (b *BudgetManager) RecordUsage(runID string, tokens int, latencyMs int64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	state, ok := b.states[runID]
	if !ok {
		state = &BudgetState{
			RunID:  runID,
			Policy: contracts.DefaultBudgetPolicy(),
		}
		b.states[runID] = state
	}

	state.TokensUsed += tokens
	state.RequestsUsed++
	state.TotalLatencyMs += latencyMs

	// Persist to store if available
	if b.store != nil {
		status := b.buildBudgetStatus(state)
		go b.store.UpdateBudgetStatus(context.Background(), runID, status)
	}
}

func (b *BudgetManager) buildBudgetStatus(state *BudgetState) contracts.BudgetStatus {
	return contracts.BudgetStatus{
		TokensUsed:        state.TokensUsed,
		TokensRemaining:   state.Policy.MaxTotalTokensPerRun - state.TokensUsed,
		RequestsUsed:      state.RequestsUsed,
		RequestsRemaining: state.Policy.MaxTotalRequestsPerRun - state.RequestsUsed,
		TimeUsedMs:        int(state.TotalLatencyMs),
		TimeRemainingMs:   state.Policy.MaxLatencyMs - int(state.TotalLatencyMs),
	}
}

// GetBudgetStatus returns the current budget status for a run.
func (b *BudgetManager) GetBudgetStatus(runID string) contracts.BudgetStatus {
	b.mu.RLock()
	defer b.mu.RUnlock()

	state, ok := b.states[runID]
	if !ok {
		policy := contracts.DefaultBudgetPolicy()
		return contracts.BudgetStatus{
			TokensUsed:        0,
			TokensRemaining:   policy.MaxTotalTokensPerRun,
			RequestsUsed:      0,
			RequestsRemaining: policy.MaxTotalRequestsPerRun,
			TimeUsedMs:        0,
			TimeRemainingMs:   policy.MaxLatencyMs,
		}
	}

	return contracts.BudgetStatus{
		TokensUsed:        state.TokensUsed,
		TokensRemaining:   state.Policy.MaxTotalTokensPerRun - state.TokensUsed,
		RequestsUsed:      state.RequestsUsed,
		RequestsRemaining: state.Policy.MaxTotalRequestsPerRun - state.RequestsUsed,
		TimeUsedMs:        int(state.TotalLatencyMs),
		TimeRemainingMs:   state.Policy.MaxLatencyMs - int(state.TotalLatencyMs),
	}
}

// GetState returns the budget state for a run, or nil if not found.
func (b *BudgetManager) GetState(runID string) *BudgetState {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.states[runID]
}

// GetMaxLatencyMs returns the max latency for a run.
func (b *BudgetManager) GetMaxLatencyMs(runID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if state, ok := b.states[runID]; ok {
		return state.Policy.MaxLatencyMs
	}
	return contracts.DefaultBudgetPolicy().MaxLatencyMs
}
