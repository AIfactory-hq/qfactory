package gates

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// RemoteExecuteRequest is the request body for remote gate execution.
type RemoteExecuteRequest struct {
	RunID          string `json:"run_id"`
	GateLevel      string `json:"gate_level"`
	GateName       string `json:"gate_name"`
	RepoRoot       string `json:"repo_root,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// RemoteExecuteResponse is the response from remote gate execution.
type RemoteExecuteResponse struct {
	Result  contracts.GateResult `json:"result"`
	Command string               `json:"command"`
	Stdout  string               `json:"stdout"`
	Stderr  string               `json:"stderr"`
	Error   string               `json:"error,omitempty"`
}

// RemoteExecutor executes gates via a remote HTTP runner.
type RemoteExecutor struct {
	runnerURL  string
	httpClient *http.Client
}

// NewRemoteExecutor creates a new remote executor.
func NewRemoteExecutor(runnerURL string) *RemoteExecutor {
	return &RemoteExecutor{
		runnerURL: runnerURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Minute, // Max total timeout; context controls actual
		},
	}
}

// Name returns the executor name.
func (e *RemoteExecutor) Name() string {
	return string(ExecutorTypeRemote)
}

// Execute runs the gate via remote HTTP call.
func (e *RemoteExecutor) Execute(ctx context.Context, run *contracts.WorkflowRun, gate Gate) (GateOutput, error) {
	// Normalize gate name
	gateName := gate.Name()
	if gateName == "" {
		gateName = "default"
	}

	// Determine timeout from context or default
	timeoutSeconds := 300 // 5 minutes default
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 {
			timeoutSeconds = int(remaining.Seconds())
		}
	}

	req := RemoteExecuteRequest{
		RunID:          run.ID,
		GateLevel:      gate.Level(),
		GateName:       gateName,
		TimeoutSeconds: timeoutSeconds,
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return GateOutput{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", e.runnerURL+"/execute", bytes.NewReader(reqBody))
	if err != nil {
		return GateOutput{}, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(httpReq)
	if err != nil {
		return GateOutput{}, fmt.Errorf("remote execution failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return GateOutput{}, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return GateOutput{}, fmt.Errorf("remote runner returned %d: %s", resp.StatusCode, string(body))
	}

	var response RemoteExecuteResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return GateOutput{}, fmt.Errorf("failed to parse response: %w", err)
	}

	if response.Error != "" {
		return GateOutput{}, fmt.Errorf("remote execution error: %s", response.Error)
	}

	// Ensure executor is set to remote
	response.Result.Executor = e.Name()

	return GateOutput{
		Result:  response.Result,
		Command: response.Command,
		Stdout:  response.Stdout,
		Stderr:  response.Stderr,
	}, nil
}
