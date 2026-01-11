// Package gates provides the gate execution framework for quality gates.
package gates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/AIfactory-hq/qfactory/internal/store"
	"github.com/AIfactory-hq/qfactory/pkg/contracts"
	"github.com/AIfactory-hq/qfactory/pkg/events"
)

// EventPublisher defines the interface for publishing gate events.
type EventPublisher interface {
	// PublishEvent persists and broadcasts an event.
	PublishEvent(ctx context.Context, event events.Event) error
}

// Gate defines the interface for a quality gate.
type Gate interface {
	// Level returns the gate level (e.g., PR1, PR2).
	Level() string
	// Name returns the gate name.
	Name() string
	// Run executes the gate and returns the result plus captured output.
	Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error)
}

// GateOutput contains the gate result and captured command output.
type GateOutput struct {
	Result  contracts.GateResult
	Command string
	Stdout  string
	Stderr  string
}

// GateRunner manages gate execution and persistence.
type GateRunner struct {
	store           store.Store
	evidenceDir     string
	publisher       EventPublisher
	defaultExecutor GateExecutor
	registry        *Registry
}

// NewGateRunner creates a new gate runner with local executor as default.
func NewGateRunner(s store.Store, evidenceDir string) *GateRunner {
	return &GateRunner{
		store:           s,
		evidenceDir:     evidenceDir,
		defaultExecutor: NewLocalExecutor(),
	}
}

// SetEventPublisher sets the event publisher for SSE notifications.
func (r *GateRunner) SetEventPublisher(p EventPublisher) {
	r.publisher = p
}

// SetDefaultExecutor sets the default gate executor.
func (r *GateRunner) SetDefaultExecutor(executor GateExecutor) {
	r.defaultExecutor = executor
}

// SetRegistry sets the gate registry for lookups.
func (r *GateRunner) SetRegistry(registry *Registry) {
	r.registry = registry
}

// GetRegistry returns the gate registry (creates default if nil).
func (r *GateRunner) GetRegistry() *Registry {
	if r.registry == nil {
		r.registry = DefaultRegistry()
	}
	return r.registry
}

// RunGate executes a gate using the default executor and persists the result.
func (r *GateRunner) RunGate(ctx context.Context, runID string, gate Gate) (contracts.GateResult, error) {
	return r.RunGateWithExecutor(ctx, runID, gate, r.defaultExecutor)
}

// RunGateWithExecutor executes a gate using the specified executor and persists the result.
func (r *GateRunner) RunGateWithExecutor(ctx context.Context, runID string, gate Gate, executor GateExecutor) (contracts.GateResult, error) {
	run, found, err := r.store.GetRun(ctx, runID)
	if err != nil {
		return contracts.GateResult{}, fmt.Errorf("failed to get run: %w", err)
	}
	if !found {
		return contracts.GateResult{}, fmt.Errorf("run not found: %s", runID)
	}

	// Normalize gate name in one place
	gateName := gate.Name()
	if gateName == "" {
		gateName = "default"
	}

	// Determine executor name
	executorName := "local"
	if executor != nil {
		executorName = executor.Name()
	}

	// Generate unique execution ID for history
	executionID := events.NewID()
	startTime := time.Now().UTC()

	// Emit gate.started event
	if r.publisher != nil {
		startEvent := events.NewGateStartedEvent(runID, gate.Level(), gateName, executorName, executionID, startTime)
		if err := r.publisher.PublishEvent(ctx, startEvent); err != nil {
			fmt.Printf("Warning: failed to publish gate.started event: %v\n", err)
		}
	}

	// Execute gate using executor
	var output GateOutput
	var runErr error
	if executor != nil {
		output, runErr = executor.Execute(ctx, run, gate)
	} else {
		output, runErr = gate.Run(ctx, run)
	}
	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	if runErr != nil {
		// Gate execution crashed - still write evidence and persist failure
		failOutput := GateOutput{
			Result: contracts.GateResult{
				Level:       gate.Level(),
				Name:        gateName,
				ExecutionID: executionID,
				Passed:      false,
				Executor:    executorName,
				Timestamp:   endTime,
				StartedAt:   &startTime,
				CompletedAt: &endTime,
				DurationMs:  durationMs,
				Error:       runErr.Error(),
				Checks: []contracts.Check{
					{Name: "gate_execution", Passed: false, Message: runErr.Error()},
				},
			},
			Command: output.Command,
			Stdout:  output.Stdout,
			Stderr:  output.Stderr,
		}
		if failOutput.Stderr == "" {
			failOutput.Stderr = runErr.Error()
		}

		// Write evidence even on failure (keyed by executionID for immutability)
		evidencePath, _ := r.writeGateEvidence(runID, gate.Level(), gateName, executionID, failOutput)
		failOutput.Result.EvidencePath = evidencePath

		// Persist to history table (no parent for initial run)
		historyItem := contracts.GateHistoryItem{
			ID:     executionID,
			Result: failOutput.Result,
		}
		_ = r.store.AddGateHistoryItem(ctx, runID, historyItem)

		// Update latest view
		_ = r.store.AddGateResult(ctx, runID, failOutput.Result)

		// Emit gate.failed event
		if r.publisher != nil {
			failEvent := events.NewGateFailedEvent(runID, gate.Level(), gateName, executorName, executionID, runErr.Error(), startTime, durationMs)
			if err := r.publisher.PublishEvent(ctx, failEvent); err != nil {
				fmt.Printf("Warning: failed to publish gate.failed event: %v\n", err)
			}
		}

		return failOutput.Result, fmt.Errorf("gate execution failed: %w", runErr)
	}

	// Normalize name, executor, and execution ID in output result
	output.Result.Name = gateName
	output.Result.ExecutionID = executionID
	if output.Result.Executor == "" {
		output.Result.Executor = executorName
	}

	// Write all evidence files (keyed by executionID for immutability)
	evidencePath, writeErr := r.writeGateEvidence(runID, gate.Level(), gateName, executionID, output)
	if writeErr != nil {
		fmt.Printf("Warning: failed to write evidence: %v\n", writeErr)
	}
	output.Result.EvidencePath = evidencePath

	// Persist to history table (no parent for initial run)
	historyItem := contracts.GateHistoryItem{
		ID:     executionID,
		Result: output.Result,
	}
	if err := r.store.AddGateHistoryItem(ctx, runID, historyItem); err != nil {
		fmt.Printf("Warning: failed to persist gate history: %v\n", err)
	}

	// Update latest view (with replace semantics in store)
	if err := r.store.AddGateResult(ctx, runID, output.Result); err != nil {
		return output.Result, fmt.Errorf("failed to persist gate result: %w", err)
	}

	// Emit gate.completed event
	if r.publisher != nil {
		completeEvent := events.NewGateCompletedEvent(
			runID, gate.Level(), gateName, output.Result.Executor, executionID,
			output.Result.Passed, evidencePath,
			startTime, endTime, output.Result.DurationMs,
		)
		if err := r.publisher.PublishEvent(ctx, completeEvent); err != nil {
			fmt.Printf("Warning: failed to publish gate.completed event: %v\n", err)
		}
	}

	return output.Result, nil
}

// RetryGate retries a gate execution with lineage tracking.
// It looks up the gate from the registry, gets the latest execution ID as parent,
// and creates a new execution with parent_execution_id and retry_reason.
func (r *GateRunner) RetryGate(ctx context.Context, runID, level, name, reason string) (contracts.GateResult, error) {
	return r.RetryGateWithExecutor(ctx, runID, level, name, reason, r.defaultExecutor)
}

// RetryGateWithExecutor retries a gate with a specific executor.
func (r *GateRunner) RetryGateWithExecutor(ctx context.Context, runID, level, name, reason string, executor GateExecutor) (contracts.GateResult, error) {
	// Normalize gate name
	name = contracts.NormalizeGateName(name)
	reason = contracts.TruncateRetryReason(reason)

	// Get the run
	run, found, err := r.store.GetRun(ctx, runID)
	if err != nil {
		return contracts.GateResult{}, fmt.Errorf("failed to get run: %w", err)
	}
	if !found {
		return contracts.GateResult{}, fmt.Errorf("run not found: %s", runID)
	}

	// Look up the gate from the registry
	registry := r.GetRegistry()
	gate, found := registry.Lookup(level, name)
	if !found {
		return contracts.GateResult{}, fmt.Errorf("gate not found in registry: %s/%s", level, name)
	}

	// Get the latest execution ID to use as parent
	parentExecID, err := r.store.GetLatestExecution(ctx, runID, level, name)
	if err != nil {
		// If no previous execution, parentExecID will be empty string
		parentExecID = ""
	}

	// Determine executor name
	executorName := "local"
	if executor != nil {
		executorName = executor.Name()
	}

	// Generate new execution ID
	executionID := events.NewID()
	startTime := time.Now().UTC()

	// Build retry info for events
	retryInfo := events.GateRetryInfo{
		ParentExecutionID: parentExecID,
		RetryReason:       reason,
	}

	// Emit gate.started event with retry info
	if r.publisher != nil {
		startEvent := events.NewGateStartedRetryEvent(runID, level, name, executorName, executionID, startTime, retryInfo)
		if err := r.publisher.PublishEvent(ctx, startEvent); err != nil {
			fmt.Printf("Warning: failed to publish gate.started event: %v\n", err)
		}
	}

	// Execute gate using executor
	var output GateOutput
	var runErr error
	if executor != nil {
		output, runErr = executor.Execute(ctx, run, gate)
	} else {
		output, runErr = gate.Run(ctx, run)
	}
	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	// Build the gate result
	var result contracts.GateResult
	if runErr != nil {
		result = contracts.GateResult{
			Level:       level,
			Name:        name,
			ExecutionID: executionID,
			Passed:      false,
			Executor:    executorName,
			Timestamp:   endTime,
			StartedAt:   &startTime,
			CompletedAt: &endTime,
			DurationMs:  durationMs,
			Error:       runErr.Error(),
			Checks: []contracts.Check{
				{Name: "gate_execution", Passed: false, Message: runErr.Error()},
			},
		}
		if output.Stderr == "" {
			output.Stderr = runErr.Error()
		}
	} else {
		result = output.Result
		result.Name = name
		result.ExecutionID = executionID
		if result.Executor == "" {
			result.Executor = executorName
		}
	}

	// Write evidence (keyed by executionID for immutability)
	evidencePath, writeErr := r.writeGateEvidence(runID, level, name, executionID, GateOutput{
		Result:  result,
		Command: output.Command,
		Stdout:  output.Stdout,
		Stderr:  output.Stderr,
	})
	if writeErr != nil {
		fmt.Printf("Warning: failed to write evidence: %v\n", writeErr)
	}
	result.EvidencePath = evidencePath

	// Build history item with lineage
	historyItem := contracts.GateHistoryItem{
		ID:          executionID,
		Result:      result,
		RetryReason: reason,
	}
	if parentExecID != "" {
		historyItem.ParentExecutionID = &parentExecID
	}

	// Persist to history table with lineage
	if err := r.store.AddGateHistoryItem(ctx, runID, historyItem); err != nil {
		fmt.Printf("Warning: failed to persist gate history: %v\n", err)
	}

	// Update latest view
	if err := r.store.AddGateResult(ctx, runID, result); err != nil {
		return result, fmt.Errorf("failed to persist gate result: %w", err)
	}

	// Emit appropriate event with retry info
	if r.publisher != nil {
		if runErr != nil {
			failEvent := events.NewGateFailedRetryEvent(runID, level, name, executorName, executionID, runErr.Error(), startTime, durationMs, retryInfo)
			if err := r.publisher.PublishEvent(ctx, failEvent); err != nil {
				fmt.Printf("Warning: failed to publish gate.failed event: %v\n", err)
			}
		} else {
			completeEvent := events.NewGateCompletedRetryEvent(
				runID, level, name, result.Executor, executionID,
				result.Passed, evidencePath,
				startTime, endTime, result.DurationMs, retryInfo,
			)
			if err := r.publisher.PublishEvent(ctx, completeEvent); err != nil {
				fmt.Printf("Warning: failed to publish gate.completed event: %v\n", err)
			}
		}
	}

	if runErr != nil {
		return result, fmt.Errorf("gate execution failed: %w", runErr)
	}

	return result, nil
}

// writeGateEvidence writes all evidence files for a gate execution.
// Evidence is keyed by executionID to ensure immutability on re-runs.
func (r *GateRunner) writeGateEvidence(runID, level, gateName, executionID string, output GateOutput) (string, error) {
	// Path: evidence/<runID>/gates/<level>/<name>/<executionID>/
	gateDir := filepath.Join(r.evidenceDir, runID, "gates", level, gateName, executionID)
	if err := os.MkdirAll(gateDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create gate dir: %w", err)
	}

	// Write command.txt
	if err := os.WriteFile(filepath.Join(gateDir, "command.txt"), []byte(output.Command+"\n"), 0644); err != nil {
		return "", fmt.Errorf("failed to write command.txt: %w", err)
	}

	// Write stdout.log
	if err := os.WriteFile(filepath.Join(gateDir, "stdout.log"), []byte(output.Stdout), 0644); err != nil {
		return "", fmt.Errorf("failed to write stdout.log: %w", err)
	}

	// Write stderr.log
	if err := os.WriteFile(filepath.Join(gateDir, "stderr.log"), []byte(output.Stderr), 0644); err != nil {
		return "", fmt.Errorf("failed to write stderr.log: %w", err)
	}

	// Write result.json
	resultData := map[string]interface{}{
		"level":        output.Result.Level,
		"name":         output.Result.Name,
		"execution_id": executionID,
		"passed":       output.Result.Passed,
		"executor":     output.Result.Executor,
		"duration_ms":  output.Result.DurationMs,
		"timestamp":    output.Result.Timestamp,
		"started_at":   output.Result.StartedAt,
		"completed_at": output.Result.CompletedAt,
		"checks":       output.Result.Checks,
		"error":        output.Result.Error,
	}

	resultJSON, err := json.MarshalIndent(resultData, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	if err := os.WriteFile(filepath.Join(gateDir, "result.json"), resultJSON, 0644); err != nil {
		return "", fmt.Errorf("failed to write result.json: %w", err)
	}

	return gateDir, nil
}
