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
	// SecurityScanGateTimeout is the timeout for security scans.
	SecurityScanGateTimeout = 5 * time.Minute
)

// SecurityScanGate runs security and static analysis tools as a PR3 gate.
type SecurityScanGate struct{}

// NewSecurityScanGate creates a new security scan gate.
func NewSecurityScanGate() *SecurityScanGate {
	return &SecurityScanGate{}
}

// Level returns the gate level.
func (g *SecurityScanGate) Level() string {
	return contracts.GateLevelPR3
}

// Name returns the gate name.
func (g *SecurityScanGate) Name() string {
	return "security_scan"
}

// toolInfo holds information about a security tool.
type toolInfo struct {
	name    string
	args    []string
	checkFn func(err error, stdout, stderr string) (passed bool, msg string)
}

// Run executes the security scan gate.
func (g *SecurityScanGate) Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error) {
	startTime := time.Now().UTC()

	// Create context with timeout for entire gate
	gateCtx, cancel := context.WithTimeout(ctx, SecurityScanGateTimeout)
	defer cancel()

	tools := []toolInfo{
		{
			name: "gosec",
			args: []string{"./..."},
			checkFn: func(err error, stdout, stderr string) (bool, string) {
				if err != nil {
					// gosec exits non-zero when it finds issues
					return false, "security issues found"
				}
				return true, "no security issues"
			},
		},
		{
			name: "govulncheck",
			args: []string{"./..."},
			checkFn: func(err error, stdout, stderr string) (bool, string) {
				if err != nil {
					return false, "vulnerabilities found"
				}
				return true, "no vulnerabilities"
			},
		},
		{
			name: "staticcheck",
			args: []string{"./..."},
			checkFn: func(err error, stdout, stderr string) (bool, string) {
				if err != nil {
					return false, "static analysis issues found"
				}
				return true, "no static analysis issues"
			},
		},
	}

	// Preflight: check tool availability
	var checks []contracts.Check
	var missingTools []string
	var combinedStdout, combinedStderr strings.Builder

	combinedStdout.WriteString("== Preflight: Tool Availability ==\n")
	for _, tool := range tools {
		_, err := exec.LookPath(tool.name)
		if err != nil {
			missingTools = append(missingTools, tool.name)
			checks = append(checks, contracts.Check{
				Name:    "tool:" + tool.name,
				Passed:  false,
				Message: fmt.Sprintf("missing binary: %s", tool.name),
			})
			combinedStdout.WriteString(fmt.Sprintf("%s: NOT FOUND\n", tool.name))
		} else {
			combinedStdout.WriteString(fmt.Sprintf("%s: OK\n", tool.name))
		}
	}
	combinedStdout.WriteString("\n")

	// If any tools are missing, fail-closed
	if len(missingTools) > 0 {
		endTime := time.Now().UTC()
		errMsg := fmt.Sprintf("missing tools: %s", strings.Join(missingTools, ", "))
		return GateOutput{
			Result: contracts.GateResult{
				Level:       contracts.GateLevelPR3,
				Name:        g.Name(),
				Passed:      false,
				Executor:    "local",
				Timestamp:   endTime,
				DurationMs:  endTime.Sub(startTime).Milliseconds(),
				StartedAt:   &startTime,
				CompletedAt: &endTime,
				Checks:      checks,
				Error:       errMsg,
			},
			Command: "gosec ./... && govulncheck ./... && staticcheck ./...",
			Stdout:  combinedStdout.String(),
			Stderr:  combinedStderr.String(),
		}, nil
	}

	// Run each tool sequentially
	allPassed := true
	for _, tool := range tools {
		combinedStdout.WriteString(fmt.Sprintf("== %s ==\n", tool.name))

		cmd := exec.CommandContext(gateCtx, tool.name, tool.args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		cmdErr := cmd.Run()
		combinedStdout.WriteString(stdout.String())
		if stderr.Len() > 0 {
			combinedStderr.WriteString(fmt.Sprintf("== %s stderr ==\n", tool.name))
			combinedStderr.WriteString(stderr.String())
		}
		combinedStdout.WriteString("\n")

		passed, msg := tool.checkFn(cmdErr, stdout.String(), stderr.String())
		if !passed {
			allPassed = false
		}

		exitMsg := "exit=0"
		if cmdErr != nil {
			if exitErr, ok := cmdErr.(*exec.ExitError); ok {
				exitMsg = fmt.Sprintf("exit=%d", exitErr.ExitCode())
			} else if gateCtx.Err() == context.DeadlineExceeded {
				exitMsg = "timeout"
			} else {
				exitMsg = cmdErr.Error()
			}
		}

		checks = append(checks, contracts.Check{
			Name:    tool.name,
			Passed:  passed,
			Message: fmt.Sprintf("%s (%s)", msg, exitMsg),
		})

		// Check for timeout
		if gateCtx.Err() == context.DeadlineExceeded {
			break
		}
	}

	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	var gateError string
	if !allPassed {
		gateError = "one or more security checks failed"
	}
	if gateCtx.Err() == context.DeadlineExceeded {
		gateError = "timeout exceeded"
	}

	return GateOutput{
		Result: contracts.GateResult{
			Level:       contracts.GateLevelPR3,
			Name:        g.Name(),
			Passed:      allPassed,
			Executor:    "local",
			Timestamp:   endTime,
			DurationMs:  durationMs,
			StartedAt:   &startTime,
			CompletedAt: &endTime,
			Checks:      checks,
			Error:       gateError,
		},
		Command: "gosec ./... && govulncheck ./... && staticcheck ./...",
		Stdout:  combinedStdout.String(),
		Stderr:  combinedStderr.String(),
	}, nil
}
