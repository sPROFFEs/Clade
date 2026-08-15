package agentic

import "time"

type eventBus struct {
	runID   string
	agentID string
	sink    EventSink
}

func (b eventBus) emit(eventType string, turn int, tool, detail string, ok bool, payload map[string]any) {
	if b.sink == nil {
		return
	}
	b.sink(Event{
		RunID: b.runID, AgentID: b.agentID, Type: eventType,
		Time: time.Now().UTC(), Turn: turn, Tool: tool, Detail: detail,
		OK: ok, Payload: payload,
	})
}
