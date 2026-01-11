package gates

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

const (
	// SecurityScanGateTimeout is the timeout for security scans.
	SecurityScanGateTimeout = 5 * time.Minute
)

// findGoBinary searches for a Go tool binary in common locations.
// It checks: 1) PATH, 2) GOBIN, 3) GOPATH/bin, 4) ~/go/bin
// Returns the full path to the binary or empty string if not found.
func findGoBinary(name string) string {
	// First try PATH
	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	// Check GOBIN
	if gobin := os.Getenv("GOBIN"); gobin != "" {
		candidate := filepath.Join(gobin, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Check GOPATH/bin
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		candidate := filepath.Join(gopath, "bin", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Check ~/go/bin (default GOPATH location)
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, "go", "bin", name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// Check /usr/local/go/bin
	candidate := filepath.Join("/usr/local/go/bin", name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	return ""
}

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
	path    string // full path to binary (populated during preflight)
	args    []string
	checkFn func(err error, stdout, stderr string) (passed bool, msg string)
}

// Run executes the security scan gate.
func (g *SecurityScanGate) Run(ctx context.Context, run *contracts.WorkflowRun) (GateOutput, error) {
	startTime := time.Now().UTC()

	// Determine workspace directory for this run
	// Priority: 1) evidence/{id}/workspace, 2) QF_WORKSPACE_ROOT/{id}, 3) current directory
	workspaceDir := filepath.Join(contracts.EvidenceDir, run.ID, "workspace")
	if envRoot := os.Getenv("QF_WORKSPACE_ROOT"); envRoot != "" {
		workspaceDir = filepath.Join(envRoot, run.ID)
	}

	// Check if workspace exists and has Go files
	hasGoFiles := false
	if info, err := os.Stat(workspaceDir); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(workspaceDir)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
				hasGoFiles = true
				break
			}
		}
		if !hasGoFiles {
			if _, err := os.Stat(filepath.Join(workspaceDir, "go.mod")); err == nil {
				hasGoFiles = true
			}
		}
	}

	// If no workspace or Go files, pass with informational message
	if !hasGoFiles {
		endTime := time.Now().UTC()
		return GateOutput{
			Result: contracts.GateResult{
				Level:       contracts.GateLevelPR3,
				Name:        g.Name(),
				Passed:      true,
				Executor:    "local",
				Timestamp:   endTime,
				DurationMs:  endTime.Sub(startTime).Milliseconds(),
				StartedAt:   &startTime,
				CompletedAt: &endTime,
				Checks: []contracts.Check{
					{
						Name:    "workspace",
						Passed:  true,
						Message: fmt.Sprintf("no Go files found in workspace (%s) - skipped", workspaceDir),
					},
				},
			},
			Command: "no scan (no workspace)",
			Stdout:  fmt.Sprintf("PR3 gate: No workspace directory or Go files found.\nExpected workspace at: %s\nThis is normal for stub workflows that don't generate code.\nGate passed (no files to scan).\n", workspaceDir),
			Stderr:  "",
		}, nil
	}

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

	// Preflight: check tool availability (search PATH, GOBIN, GOPATH/bin, ~/go/bin)
	var checks []contracts.Check
	var missingTools []string
	var combinedStdout, combinedStderr strings.Builder

	combinedStdout.WriteString("== Preflight: Tool Availability ==\n")
	for i := range tools {
		toolPath := findGoBinary(tools[i].name)
		if toolPath == "" {
			missingTools = append(missingTools, tools[i].name)
			checks = append(checks, contracts.Check{
				Name:    "tool:" + tools[i].name,
				Passed:  false,
				Message: fmt.Sprintf("missing binary: %s", tools[i].name),
			})
			combinedStdout.WriteString(fmt.Sprintf("%s: NOT FOUND (searched PATH, GOBIN, GOPATH/bin, ~/go/bin)\n", tools[i].name))
		} else {
			tools[i].path = toolPath
			combinedStdout.WriteString(fmt.Sprintf("%s: OK (%s)\n", tools[i].name, toolPath))
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

	// Run each tool sequentially in the workspace directory
	combinedStdout.WriteString(fmt.Sprintf("== Workspace: %s ==\n\n", workspaceDir))
	allPassed := true
	for _, tool := range tools {
		combinedStdout.WriteString(fmt.Sprintf("== %s ==\n", tool.name))

		// Use full path to tool (resolved during preflight)
		cmd := exec.CommandContext(gateCtx, tool.path, tool.args...)
		cmd.Dir = workspaceDir // Run in workspace directory
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
		Command: fmt.Sprintf("gosec ./... && govulncheck ./... && staticcheck ./... (in %s)", workspaceDir),
		Stdout:  combinedStdout.String(),
		Stderr:  combinedStderr.String(),
	}, nil
}
