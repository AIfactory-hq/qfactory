package gates

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// UnitTestGate runs go test ./... as a PR1 gate.
type UnitTestGate struct{}

// NewUnitTestGate creates a new unit test gate.
func NewUnitTestGate() *UnitTestGate {
	return &UnitTestGate{}
}

// Level returns the gate level.
func (g *UnitTestGate) Level() string {
	return contracts.GateLevelPR1
}

// Name returns the gate name.
func (g *UnitTestGate) Name() string {
	return "unit_tests"
}

// Run executes the unit test gate.
func (g *UnitTestGate) Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error) {
	startTime := time.Now().UTC()

	// Get timeout from env, default 60s
	timeoutSec := 60
	if envTimeout := os.Getenv("QF_PR1_TIMEOUT_SECONDS"); envTimeout != "" {
		if t, err := strconv.Atoi(envTimeout); err == nil && t > 0 {
			timeoutSec = t
		}
	}

	// Create context with timeout
	gateCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	// Execute go test ./...
	command := "go test ./..."
	cmd := exec.CommandContext(gateCtx, "go", "test", "./...")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	passed := cmdErr == nil
	checkMsg := "all tests passed"
	var gateError string

	if !passed {
		if gateCtx.Err() == context.DeadlineExceeded {
			checkMsg = "tests timed out"
			gateError = "timeout exceeded"
		} else if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			checkMsg = "tests failed with exit code " + strconv.Itoa(exitErr.ExitCode())
			gateError = stderr.String()
		} else {
			checkMsg = "tests failed: " + cmdErr.Error()
			gateError = cmdErr.Error()
		}
	}

	return GateOutput{
		Result: contracts.GateResult{
			Level:       contracts.GateLevelPR1,
			Name:        g.Name(),
			Passed:      passed,
			Executor:    "local",
			Timestamp:   endTime,
			DurationMs:  durationMs,
			StartedAt:   &startTime,
			CompletedAt: &endTime,
			Checks: []contracts.Check{
				{Name: "unit_tests", Passed: passed, Message: checkMsg},
			},
			Error: gateError,
		},
		Command: command,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
	}, nil
}
