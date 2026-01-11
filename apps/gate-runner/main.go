// Package main implements the remote gate runner service for qfactory.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/AIfactory-hq/qfactory/internal/gates"
	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

const (
	// DefaultAddr is the default listen address.
	DefaultAddr = ":8095"
	// DefaultRunnerName is the default runner name.
	DefaultRunnerName = "gate-runner"
	// DefaultTimeoutSeconds is the default timeout for gate execution.
	DefaultTimeoutSeconds = 300
	// IntegrationTestPattern is the pattern for integration tests.
	IntegrationTestPattern = "TestIntegration"
)

// Config holds the runner configuration.
type Config struct {
	Addr       string
	RunnerName string
	RepoRoot   string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() Config {
	cfg := Config{
		Addr:       DefaultAddr,
		RunnerName: DefaultRunnerName,
	}

	if addr := os.Getenv("GATE_RUNNER_ADDR"); addr != "" {
		cfg.Addr = addr
	}
	if name := os.Getenv("GATE_RUNNER_NAME"); name != "" {
		cfg.RunnerName = name
	}
	if root := os.Getenv("GATE_RUNNER_REPO_ROOT"); root != "" {
		cfg.RepoRoot = root
	}

	return cfg
}

// Runner is the gate runner service.
type Runner struct {
	config Config
}

// NewRunner creates a new gate runner.
func NewRunner(cfg Config) *Runner {
	return &Runner{config: cfg}
}

// ExecutorName returns the executor name for results.
func (r *Runner) ExecutorName() string {
	return fmt.Sprintf("remote:%s", r.config.RunnerName)
}

// ServeHTTP implements http.Handler.
func (r *Runner) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == "GET" && req.URL.Path == "/health" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     true,
			"runner": r.config.RunnerName,
		})
		return
	}

	if req.Method != "POST" || req.URL.Path != "/execute" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	r.handleExecute(w, req)
}

func (r *Runner) handleExecute(w http.ResponseWriter, req *http.Request) {
	var execReq gates.RemoteExecuteRequest
	if err := json.NewDecoder(req.Body).Decode(&execReq); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Normalize gate name
	if execReq.GateName == "" {
		execReq.GateName = "default"
	}

	// Determine timeout
	timeoutSeconds := execReq.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeoutSeconds
	}

	// Determine working directory
	workDir := execReq.RepoRoot
	if workDir == "" {
		workDir = r.config.RepoRoot
	}
	if workDir == "" {
		// Default to current working directory
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to get working directory")
			return
		}
	}

	ctx, cancel := context.WithTimeout(req.Context(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	// Route to appropriate gate handler
	var response gates.RemoteExecuteResponse
	switch {
	case execReq.GateLevel == contracts.GateLevelPR1 && execReq.GateName == "unit_tests":
		response = r.executePR1UnitTests(ctx, workDir)
	case execReq.GateLevel == contracts.GateLevelPR2 && execReq.GateName == "integration_smoke":
		response = r.executePR2IntegrationSmoke(ctx, workDir)
	case execReq.GateLevel == contracts.GateLevelPR3 && execReq.GateName == "security_scan":
		response = r.executePR3SecurityScan(ctx, workDir)
	default:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("unsupported gate: %s/%s", execReq.GateLevel, execReq.GateName))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (r *Runner) executePR1UnitTests(ctx context.Context, workDir string) gates.RemoteExecuteResponse {
	startTime := time.Now().UTC()

	command := "go test ./... -count=1"
	cmd := exec.CommandContext(ctx, "go", "test", "./...", "-count=1")
	cmd.Dir = workDir

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
		if ctx.Err() == context.DeadlineExceeded {
			checkMsg = "tests timed out"
			gateError = "timeout exceeded"
		} else if exitErr, ok := cmdErr.(*exec.ExitError); ok {
			checkMsg = fmt.Sprintf("tests failed with exit code %d", exitErr.ExitCode())
			gateError = stderr.String()
		} else {
			checkMsg = "tests failed: " + cmdErr.Error()
			gateError = cmdErr.Error()
		}
	}

	return gates.RemoteExecuteResponse{
		Result: contracts.GateResult{
			Level:       contracts.GateLevelPR1,
			Name:        "unit_tests",
			Passed:      passed,
			Executor:    r.ExecutorName(),
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
	}
}

func (r *Runner) executePR2IntegrationSmoke(ctx context.Context, workDir string) gates.RemoteExecuteResponse {
	startTime := time.Now().UTC()

	// Step 1: Preflight check - discover if any TestIntegration tests exist
	listCmd := exec.CommandContext(ctx, "go", "test", "./...", "-list", "^"+IntegrationTestPattern)
	listCmd.Dir = workDir
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
		return gates.RemoteExecuteResponse{
			Result: contracts.GateResult{
				Level:       contracts.GateLevelPR2,
				Name:        "integration_smoke",
				Passed:      false,
				Executor:    r.ExecutorName(),
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
		}
	}

	// Step 2: Run actual integration tests
	command := fmt.Sprintf("go test ./... -run %s -count=1 -v", IntegrationTestPattern)
	cmd := exec.CommandContext(ctx, "go", "test", "./...", "-run", IntegrationTestPattern, "-count=1", "-v")
	cmd.Dir = workDir

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
		if ctx.Err() == context.DeadlineExceeded {
			checkMsg = "integration tests timed out"
			gateError = "timeout exceeded"
		} else {
			checkMsg = "integration tests failed"
			gateError = stderr.String()
		}
	}

	return gates.RemoteExecuteResponse{
		Result: contracts.GateResult{
			Level:       contracts.GateLevelPR2,
			Name:        "integration_smoke",
			Passed:      passed,
			Executor:    r.ExecutorName(),
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
	}
}

func (r *Runner) executePR3SecurityScan(ctx context.Context, workDir string) gates.RemoteExecuteResponse {
	startTime := time.Now().UTC()

	type toolInfo struct {
		name    string
		args    []string
		checkFn func(err error, stdout, stderr string) (passed bool, msg string)
	}

	tools := []toolInfo{
		{
			name: "gosec",
			args: []string{"./..."},
			checkFn: func(err error, stdout, stderr string) (bool, string) {
				if err != nil {
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
		return gates.RemoteExecuteResponse{
			Result: contracts.GateResult{
				Level:       contracts.GateLevelPR3,
				Name:        "security_scan",
				Passed:      false,
				Executor:    r.ExecutorName(),
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
		}
	}

	// Run each tool sequentially
	allPassed := true
	for _, tool := range tools {
		combinedStdout.WriteString(fmt.Sprintf("== %s ==\n", tool.name))

		cmd := exec.CommandContext(ctx, tool.name, tool.args...)
		cmd.Dir = workDir
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
			} else if ctx.Err() == context.DeadlineExceeded {
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
		if ctx.Err() == context.DeadlineExceeded {
			break
		}
	}

	endTime := time.Now().UTC()
	durationMs := endTime.Sub(startTime).Milliseconds()

	var gateError string
	if !allPassed {
		gateError = "one or more security checks failed"
	}
	if ctx.Err() == context.DeadlineExceeded {
		gateError = "timeout exceeded"
	}

	return gates.RemoteExecuteResponse{
		Result: contracts.GateResult{
			Level:       contracts.GateLevelPR3,
			Name:        "security_scan",
			Passed:      allPassed,
			Executor:    r.ExecutorName(),
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
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(gates.RemoteExecuteResponse{
		Error: msg,
	})
}

func main() {
	cfg := LoadConfig()
	runner := NewRunner(cfg)

	log.Printf("Gate runner '%s' listening on %s", cfg.RunnerName, cfg.Addr)
	if cfg.RepoRoot != "" {
		log.Printf("Using repo root: %s", cfg.RepoRoot)
	}

	if err := http.ListenAndServe(cfg.Addr, runner); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
