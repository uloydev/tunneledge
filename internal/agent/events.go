package agent

// AgentEvent is the sealed interface for all events emitted by the agent to the TUI.
// The unexported marker method restricts implementations to this package.
type AgentEvent interface {
	agentEvent()
}

// StatusUpdateEvent is emitted whenever the QUIC connection state changes.
// Status is one of the ConnectionXxx constants defined in the TUI package.
type StatusUpdateEvent struct {
	// Status is one of: Connecting, Connected, Reconnecting, Disconnected.
	Status string
	// Endpoint is the primary public TCP endpoint assigned by the gateway (may be empty).
	Endpoint string
}

// StreamOpenedEvent is emitted when the gateway opens a new multiplexed QUIC stream.
type StreamOpenedEvent struct {
	StreamID  string
	Label     string
	LocalAddr string
}

// StreamClosedEvent is emitted when a relayed QUIC stream closes.
type StreamClosedEvent struct {
	StreamID      string
	Label         string
	Reason        string
	SentBytes     int64
	ReceivedBytes int64
}

// TelemetryTickEvent is emitted every 500 ms with incremental bandwidth counters.
// The receiver is responsible for accumulating totals.
type TelemetryTickEvent struct {
	RxDelta uint64
	TxDelta uint64
}

// LogEvent carries a single structured log entry for real-time display in the TUI.
type LogEvent struct {
	Level   string
	Message string
	Fields  map[string]string
}

// Marker methods — make AgentEvent a sealed interface.
func (StatusUpdateEvent) agentEvent()  {}
func (StreamOpenedEvent) agentEvent()  {}
func (StreamClosedEvent) agentEvent()  {}
func (TelemetryTickEvent) agentEvent() {}
func (LogEvent) agentEvent()           {}
