package tui

import (
	"encoding/json"
	"strings"

	"tunneledge/internal/agent"
)

// EventLogWriter implements io.Writer and is intended to replace (or wrap) the
// zerolog output writer while the TUI is active.  Every JSON log line emitted
// by zerolog is parsed and forwarded to uiEvents as an agent.LogEvent.
//
// The writer never blocks: if the event channel buffer is full the line is
// silently dropped so the logger goroutine is never stalled.
type EventLogWriter struct {
	ch chan<- agent.AgentEvent
}

// NewEventLogWriter creates an EventLogWriter backed by ch.
// ch must be the same buffered channel passed to agent.Options.EventCh and
// tui.New so that all events flow through a single ordered pipeline.
func NewEventLogWriter(ch chan<- agent.AgentEvent) *EventLogWriter {
	return &EventLogWriter{ch: ch}
}

// Write parses a single zerolog JSON log line and emits a LogEvent.
// Non-JSON lines are emitted with a best-effort level and the raw text as the
// message so that plain output is never silently swallowed.
func (w *EventLogWriter) Write(p []byte) (int, error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}

	var ev agent.LogEvent
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		// Not valid JSON — treat as a raw info message.
		ev = agent.LogEvent{Level: "info", Message: line, Fields: map[string]string{}}
	} else {
		ev = parseZerologJSON(raw)
	}

	select {
	case w.ch <- ev:
	default:
		// Channel full — drop to avoid blocking the caller.
	}
	return len(p), nil
}

// parseZerologJSON extracts level, message, and extra fields from a zerolog
// JSON object. Unknown value types are coerced to their JSON representation.
func parseZerologJSON(raw map[string]json.RawMessage) agent.LogEvent {
	ev := agent.LogEvent{
		Level:  "info",
		Fields: make(map[string]string),
	}

	// Standard zerolog field names.
	if v, ok := raw["level"]; ok {
		var s string
		if json.Unmarshal(v, &s) == nil {
			ev.Level = s
		}
	}

	// zerolog uses "message" as the default message field name.
	for _, key := range []string{"message", "msg"} {
		if v, ok := raw[key]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				ev.Message = s
				break
			}
		}
	}

	skipKeys := map[string]bool{
		"level": true, "message": true, "msg": true, "time": true,
	}
	for k, v := range raw {
		if skipKeys[k] {
			continue
		}
		var s string
		if json.Unmarshal(v, &s) == nil {
			ev.Fields[k] = s
		} else {
			ev.Fields[k] = string(v)
		}
	}
	return ev
}
