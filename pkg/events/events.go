// Package events defines event types and helpers for qfactory.
package events

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// EventType identifies the type of event.
type EventType string

const (
	EventTypeStageStarted   EventType = "stage.started"
	EventTypeStageCompleted EventType = "stage.completed"
	EventTypeStageFailed    EventType = "stage.failed"
	EventTypeRunStarted     EventType = "run.started"
	EventTypeRunCompleted   EventType = "run.completed"
	EventTypeRunFailed      EventType = "run.failed"
)

// Event is the envelope for all events.
type Event struct {
	ID        string          `json:"id"`
	Type      EventType       `json:"type"`
	RunID     string          `json:"run_id"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// StagePayload is the payload for stage events.
type StagePayload struct {
	StageName  string `json:"stage_name"`
	StageIndex int    `json:"stage_index"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

// RunPayload is the payload for run-level events.
type RunPayload struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// NewStageStartedEvent creates a stage.started event.
func NewStageStartedEvent(runID, stageName string, stageIndex int) Event {
	payload, _ := json.Marshal(StagePayload{
		StageName:  stageName,
		StageIndex: stageIndex,
	})
	return Event{
		ID:        generateEventID(),
		Type:      EventTypeStageStarted,
		RunID:     runID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// NewStageCompletedEvent creates a stage.completed event.
func NewStageCompletedEvent(runID, stageName string, stageIndex int) Event {
	payload, _ := json.Marshal(StagePayload{
		StageName:  stageName,
		StageIndex: stageIndex,
	})
	return Event{
		ID:        generateEventID(),
		Type:      EventTypeStageCompleted,
		RunID:     runID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

func generateEventID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return time.Now().UTC().Format("20060102150405.000000000")
	}
	return time.Now().UTC().Format("20060102150405") + "-" + hex.EncodeToString(b)
}
