// Package agentic implements PrAImate's managed single-agent runtime.
//
// It deliberately knows nothing about CLI adapters, the database, Wails, or
// local-LLM routing. Callers provide a Model implementation and an optional
// event sink. This keeps the runtime testable and prevents it from reaching
// around Core's execution and credential boundaries.
package agentic

import (
	"context"
	"encoding/json"
	"time"
)

type State string

const (
	StateRunning   State = "running"
	StateWaiting   State = "waiting"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateStopped   State = "stopped"
	StateStalled   State = "stalled"
)

type Event struct {
	RunID   string         `json:"runId"`
	AgentID string         `json:"agentId"`
	Type    string         `json:"type"`
	Time    time.Time      `json:"time"`
	Turn    int            `json:"turn,omitempty"`
	Tool    string         `json:"tool,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	OK      bool           `json:"ok,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

type EventSink func(Event)

type ModelInput struct {
	SystemPrompt string
	Message      string
}

type ModelOutput struct {
	Text      string
	SessionID string
}

type Model interface {
	Turn(ctx context.Context, input ModelInput, emit EventSink) (*ModelOutput, error)
}

type Limits struct {
	MaxTurns        int `json:"maxTurns"`
	MaxContextChars int `json:"maxContextChars"`
	MaxOutputChars  int `json:"maxOutputChars"`
	MaxArtifactSize int `json:"maxArtifactSize"`
}

func (l Limits) withDefaults() Limits {
	if l.MaxTurns == 0 {
		l.MaxTurns = 12
	}
	if l.MaxContextChars == 0 {
		l.MaxContextChars = 48_000
	}
	if l.MaxOutputChars == 0 {
		l.MaxOutputChars = 8_000
	}
	if l.MaxArtifactSize == 0 {
		l.MaxArtifactSize = 8 << 20
	}
	return l
}

type Config struct {
	RootDir      string
	AgentID      string
	AgentName    string
	Instructions string
	Task         string
	Limits       Limits
	Model        Model
	OnEvent      EventSink
}

type Instance struct {
	ID          string     `json:"id"`
	AgentID     string     `json:"agentId"`
	AgentName   string     `json:"agentName"`
	State       State      `json:"state"`
	Turns       int        `json:"turns"`
	SessionID   string     `json:"sessionId,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Error       string     `json:"error,omitempty"`
	Final       string     `json:"final,omitempty"`
	Artifacts   []Artifact `json:"artifacts,omitempty"`
}

type Result struct {
	Instance *Instance `json:"instance"`
	Memory   Memory    `json:"memory"`
}

type Decision struct {
	Action    string          `json:"action"`
	Tool      string          `json:"tool,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
	Message   string          `json:"message,omitempty"`
}
