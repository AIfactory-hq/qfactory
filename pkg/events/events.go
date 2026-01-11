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
	EventTypeGateStarted    EventType = "gate.started"
	EventTypeGateCompleted  EventType = "gate.completed"
	EventTypeGateFailed     EventType = "gate.failed"
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

// GateInfo contains gate metadata for events.
type GateInfo struct {
	Level    string `json:"level"`
	Name     string `json:"name"`
	Executor string `json:"executor"`
}

// GatePayload is the payload for gate events.
type GatePayload struct {
	Gate         GateInfo `json:"gate"`
	Passed       bool     `json:"passed,omitempty"`
	EvidencePath string   `json:"evidence_path,omitempty"`
	Error        string   `json:"error,omitempty"`
	StartedAt    string   `json:"started_at,omitempty"`
	CompletedAt  string   `json:"completed_at,omitempty"`
	DurationMs   int64    `json:"duration_ms,omitempty"`
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

// NewID generates a new unique ID (exported for use by other packages).
func NewID() string {
	return generateEventID()
}

// NewGateStartedEvent creates a gate.started event.
func NewGateStartedEvent(runID, level, name, executor string, startedAt time.Time) Event {
	payload, _ := json.Marshal(GatePayload{
		Gate: GateInfo{
			Level:    level,
			Name:     name,
			Executor: executor,
		},
		StartedAt: startedAt.Format(time.RFC3339),
	})
	return Event{
		ID:        generateEventID(),
		Type:      EventTypeGateStarted,
		RunID:     runID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// NewGateCompletedEvent creates a gate.completed event.
func NewGateCompletedEvent(runID, level, name, executor string, passed bool, evidencePath string, startedAt, completedAt time.Time, durationMs int64) Event {
	payload, _ := json.Marshal(GatePayload{
		Gate: GateInfo{
			Level:    level,
			Name:     name,
			Executor: executor,
		},
		Passed:       passed,
		EvidencePath: evidencePath,
		StartedAt:    startedAt.Format(time.RFC3339),
		CompletedAt:  completedAt.Format(time.RFC3339),
		DurationMs:   durationMs,
	})
	return Event{
		ID:        generateEventID(),
		Type:      EventTypeGateCompleted,
		RunID:     runID,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
}

// NewGateFailedEvent creates a gate.failed event.
func NewGateFailedEvent(runID, level, name, executor string, errMsg string, startedAt time.Time, durationMs int64) Event {
	completedAt := time.Now().UTC()
	payload, _ := json.Marshal(GatePayload{
		Gate: GateInfo{
			Level:    level,
			Name:     name,
			Executor: executor,
		},
		Passed:      false,
		Error:       errMsg,
		StartedAt:   startedAt.Format(time.RFC3339),
		CompletedAt: completedAt.Format(time.RFC3339),
		DurationMs:  durationMs,
	})
	return Event{
		ID:        generateEventID(),
		Type:      EventTypeGateFailed,
		RunID:     runID,
		Timestamp: completedAt,
		Payload:   payload,
	}
}
