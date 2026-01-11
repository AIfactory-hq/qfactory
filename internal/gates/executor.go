package gates

import (
	"context"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// ExecutorType identifies the type of gate executor.
type ExecutorType string

const (
	ExecutorTypeLocal  ExecutorType = "local"
	ExecutorTypeRemote ExecutorType = "remote"
)

// GateExecutor defines the interface for executing gates.
type GateExecutor interface {
	// Name returns the executor name (e.g., "local", "remote").
	Name() string
	// Execute runs the gate and returns the output.
	Execute(ctx context.Context, run *contracts.WorkflowRun, gate Gate) (GateOutput, error)
}

// LocalExecutor executes gates locally using the gate's Run method.
type LocalExecutor struct{}

// NewLocalExecutor creates a new local executor.
func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

// Name returns the executor name.
func (e *LocalExecutor) Name() string {
	return string(ExecutorTypeLocal)
}

// Execute runs the gate locally.
func (e *LocalExecutor) Execute(ctx context.Context, run *contracts.WorkflowRun, gate Gate) (GateOutput, error) {
	output, err := gate.Run(ctx, run)
	if err != nil {
		return output, err
	}

	// Set executor if not already set
	if output.Result.Executor == "" {
		output.Result.Executor = e.Name()
	}

	return output, nil
}
