// Package main implements the orchestrator worker for qfactory.
package main

import (
	"log"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"

	wf "github.com/AIfactory-hq/qfactory/apps/orchestrator-worker/workflow"
)

const TaskQueue = "qfactory-v0"

func main() {
	temporalAddr := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddr == "" {
		temporalAddr = "localhost:7233"
	}

	c, err := client.Dial(client.Options{
		HostPort: temporalAddr,
	})
	if err != nil {
		log.Fatalf("Failed to create Temporal client: %v", err)
	}
	defer c.Close()

	w := worker.New(c, TaskQueue, worker.Options{})

	w.RegisterWorkflow(wf.DemoWorkflow)
	w.RegisterActivity(wf.EmitStageStarted)
	w.RegisterActivity(wf.EmitStageCompleted)
	w.RegisterActivity(wf.ExecuteStage)

	log.Printf("Starting worker on task queue: %s", TaskQueue)
	if err := w.Run(worker.InterruptCh()); err != nil {
		log.Fatalf("Worker failed: %v", err)
	}
}
