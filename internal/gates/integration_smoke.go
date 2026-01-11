package gates

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

const (
	// IntegrationSmokeGateTimeout is the timeout for integration tests.
	IntegrationSmokeGateTimeout = 5 * time.Minute
	// IntegrationTestPattern is the pattern for integration tests.
	IntegrationTestPattern = "TestIntegration"
)

// IntegrationSmokeGate runs integration tests as a PR2 gate.
type IntegrationSmokeGate struct{}

// NewIntegrationSmokeGate creates a new integration smoke gate.
func NewIntegrationSmokeGate() *IntegrationSmokeGate {
	return &IntegrationSmokeGate{}
}

// Level returns the gate level.
func (g *IntegrationSmokeGate) Level() string {
	return contracts.GateLevelPR2
}

// Name returns the gate name.
func (g *IntegrationSmokeGate) Name() string {
	return "integration_smoke"
}

// Run executes the integration smoke gate.
func (g *IntegrationSmokeGate) Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error) {
	startTime := time.Now().UTC()

	// Create context with timeout for entire gate (preflight + run)
	gateCtx, cancel := context.WithTimeout(ctx, IntegrationSmokeGateTimeout)
	defer cancel()

	// Step 1: Preflight check - discover if any TestIntegration tests exist
	listCmd := exec.CommandContext(gateCtx, "go", "test", "./...", "-list", "^"+IntegrationTestPattern)
	var listOut bytes.Buffer
	listCmd.Stdout = &listOut
	listCmd.Stderr = &listOut
	listCmd.Run() // ignore error, we check output

	// Parse -list output line-by-line for deterministic detection
	listOutput := listOut.String()
	hasIntegrationTests := false
	for _, line := range strings.Split(listOutput, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, IntegrationTestPattern) {
			hasIntegrationTests = true
			break
		}
	}

	if !hasIntegrationTests {
		endTime := time.Now().UTC()
		return GateOutput{
			Result: contracts.GateResult{
				Level:       contracts.GateLevelPR2,
				Name:        g.Name(),
				Passed:      false,
				Executor:    "local",
				Timestamp:   endTime,
				DurationMs:  endTime.Sub(startTime).Milliseconds(),
				StartedAt:   &startTime,
				CompletedAt: &endTime,
				Checks: []contracts.Check{
					{
						Name:    "integration_tests",
						Passed:  false,
						Message: fmt.Sprintf("No integration tests found (%s)", IntegrationTestPattern),
					},
				},
				Error: fmt.Sprintf("No integration tests found (%s)", IntegrationTestPattern),
			},
			Command: fmt.Sprintf("go test ./... -list ^%s", IntegrationTestPattern),
			Stdout:  listOutput,
			Stderr:  "",
		}, nil
	}

	// Step 2: Run actual integration tests (still under gateCtx timeout)
	command := fmt.Sprintf("go test ./... -run %s -count=1 -v", IntegrationTestPattern)

	cmd := exec.CommandContext(gateCtx, "go", "test", "./...", "-run", IntegrationTestPattern, "-count=1", "-v")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmdErr := cmd.Run()
	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	passed := cmdErr == nil
	checkMsg := "integration tests passed"
	var gateError string

	if !passed {
		if gateCtx.Err() == context.DeadlineExceeded {
			checkMsg = "integration tests timed out"
			gateError = "timeout exceeded"
		} else {
			checkMsg = "integration tests failed"
			gateError = stderr.String()
		}
	}

	return GateOutput{
		Result: contracts.GateResult{
			Level:       contracts.GateLevelPR2,
			Name:        g.Name(),
			Passed:      passed,
			Executor:    "local",
			Timestamp:   endTime,
			DurationMs:  durationMs,
			StartedAt:   &startTime,
			CompletedAt: &endTime,
			Checks: []contracts.Check{
				{Name: "integration_tests", Passed: passed, Message: checkMsg},
			},
			Error: gateError,
		},
		Command: command,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
	}, nil
}
