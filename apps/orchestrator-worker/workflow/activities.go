// Package workflow contains activities for qfactory workflows.
package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.temporal.io/sdk/activity"

	"github.com/AIfactory-hq/qfactory/pkg/events"
)

// ControlPlaneURL is the base URL for the control-plane API.
// TODO: Make configurable via environment variable.
const ControlPlaneURL = "http://localhost:8090"

// EmitStageStarted sends a stage.started event to control-plane-api.
func EmitStageStarted(ctx context.Context, runID, stageName string, stageIndex int) error {
	event := events.NewStageStartedEvent(runID, stageName, stageIndex)
	return sendEvent(ctx, event)
}

// EmitStageCompleted sends a stage.completed event to control-plane-api.
func EmitStageCompleted(ctx context.Context, runID, stageName string, stageIndex int) error {
	event := events.NewStageCompletedEvent(runID, stageName, stageIndex)
	return sendEvent(ctx, event)
}

// ExecuteStage performs the work for a stage (dummy for v0.1).
func ExecuteStage(ctx context.Context, runID, stageName string, stageIndex int) error {
	logger := activity.GetLogger(ctx)
	logger.Info("Executing stage", "runID", runID, "stage", stageName)

	// Simulate work with a short sleep
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	logger.Info("Stage completed", "runID", runID, "stage", stageName)
	return nil
}

func sendEvent(ctx context.Context, event events.Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ControlPlaneURL+"/internal/events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("event rejected with status %d", resp.StatusCode)
	}
	return nil
}
