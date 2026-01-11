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
)

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
	store       store.Store
	evidenceDir string
}

// NewGateRunner creates a new gate runner.
func NewGateRunner(s store.Store, evidenceDir string) *GateRunner {
	return &GateRunner{
		store:       s,
		evidenceDir: evidenceDir,
	}
}

// RunGate executes a gate and persists the result.
func (r *GateRunner) RunGate(ctx context.Context, runID string, gate Gate) (contracts.GateResult, error) {
	run, found, err := r.store.GetRun(ctx, runID)
	if err != nil {
		return contracts.GateResult{}, fmt.Errorf("failed to get run: %w", err)
	}
	if !found {
		return contracts.GateResult{}, fmt.Errorf("run not found: %s", runID)
	}

	// Execute gate
	output, err := gate.Run(ctx, run)
	if err != nil {
		// Gate execution crashed - still write evidence and persist failure
		now := time.Now().UTC()
		failOutput := GateOutput{
			Result: contracts.GateResult{
				Level:       gate.Level(),
				Name:        gate.Name(),
				Passed:      false,
				Executor:    "local",
				Timestamp:   now,
				StartedAt:   &now,
				CompletedAt: &now,
				DurationMs:  0,
				Error:       err.Error(),
				Checks: []contracts.Check{
					{Name: "gate_execution", Passed: false, Message: err.Error()},
				},
			},
			Command: "",
			Stdout:  "",
			Stderr:  err.Error(),
		}
		evidencePath, _ := r.writeGateEvidence(runID, gate.Level(), gate.Name(), failOutput)
		failOutput.Result.EvidencePath = evidencePath
		_ = r.store.AddGateResult(ctx, runID, failOutput.Result)
		return failOutput.Result, fmt.Errorf("gate execution failed: %w", err)
	}

	// Write all evidence files
	evidencePath, writeErr := r.writeGateEvidence(runID, gate.Level(), gate.Name(), output)
	if writeErr != nil {
		fmt.Printf("Warning: failed to write evidence: %v\n", writeErr)
	}
	output.Result.EvidencePath = evidencePath

	// Persist gate result (with replace semantics in store)
	if err := r.store.AddGateResult(ctx, runID, output.Result); err != nil {
		return output.Result, fmt.Errorf("failed to persist gate result: %w", err)
	}

	return output.Result, nil
}

// writeGateEvidence writes all evidence files for a gate execution.
func (r *GateRunner) writeGateEvidence(runID, level, gateName string, output GateOutput) (string, error) {
	gateDir := filepath.Join(r.evidenceDir, runID, "gates", level, gateName)
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
