// Package workflow defines the DemoWorkflow for qfactory v0.1.
package workflow

import (
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/AIfactory-hq/qfactory/pkg/contracts"
)

// DemoWorkflow executes 5 stages: Spec -> Contract -> Plan -> Implement -> Verify
func DemoWorkflow(ctx workflow.Context, runID string) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("DemoWorkflow started", "runID", runID)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	stages := contracts.DemoWorkflowStages

	for i, stage := range stages {
		// Emit stage.started
		err := workflow.ExecuteActivity(ctx, EmitStageStarted, runID, stage, i).Get(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to emit stage.started for %s: %w", stage, err)
		}

		// Execute stage work
		err = workflow.ExecuteActivity(ctx, ExecuteStage, runID, stage, i).Get(ctx, nil)
		if err != nil {
			return fmt.Errorf("stage %s failed: %w", stage, err)
		}

		// Emit stage.completed
		err = workflow.ExecuteActivity(ctx, EmitStageCompleted, runID, stage, i).Get(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to emit stage.completed for %s: %w", stage, err)
		}
	}

	logger.Info("DemoWorkflow completed", "runID", runID)
	return nil
}
