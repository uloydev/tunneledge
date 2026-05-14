// Package tui implements the Bubble Tea TUI for the TunnelEdge agent.
// It provides a real-time view of the QUIC connection state, active multiplexed
// streams, structured log tail, and bandwidth sparkline.
//
// Concurrency contract: all TUI state mutations happen inside the Bubble Tea
// Update() loop (single goroutine). The only shared-memory crossing is the
// uiEvents channel, which is written by the agent transport goroutines and
// read by a blocking tea.Cmd. The activeStreams map is protected by mu for
// reads performed outside of Update (none currently), but all writes happen
// in Update via handleAgentEvent — keeping the mutex as a defensive boundary.
package tui

import (
	"context"
	"sync"
	"time"

	"tunneledge/internal/agent"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
)

// Connection status constants mirroring agent.StatusUpdateEvent.Status values.
const (
	ConnectionConnecting   = "Connecting"
	ConnectionConnected    = "Connected"
	ConnectionReconnecting = "Reconnecting"
	ConnectionDisconnected = "Disconnected"
)

const (
	maxLogs      = 100 // log ring-buffer capacity
	sparklineLen = 60  // bandwidth history samples (one per 500 ms tick → 30 s window)
)

// StreamStats holds live telemetry for a single multiplexed QUIC stream.
type StreamStats struct {
	StreamID  string
	Label     string
	LocalAddr string
	RxBytes   int64
	TxBytes   int64
	OpenedAt  time.Time
}

// MainModel is the root Bubble Tea model for the agent TUI.
// All fields are owned by the Update goroutine except where noted.
type MainModel struct {
	// agentCtx / cancel implement the graceful-shutdown contract:
	// pressing q or Ctrl+C calls cancel(), which propagates through the
	// errgroup to every QUIC stream and the transport layer before tea.Quit.
	agentCtx context.Context
	cancel   context.CancelFunc

	connectionStatus string
	tunnelEndpoint   string

	// mu guards activeStreams for any reads that might occur outside Update.
	mu            sync.RWMutex
	activeStreams map[string]StreamStats

	// logs is a ring buffer of rendered log lines capped at maxLogs.
	logs []string

	// Bandwidth totals accumulated from TelemetryTickEvents.
	rxBytes uint64
	txBytes uint64

	// bandwidthHistory is a ring buffer for the footer sparkline.
	// Each slot holds combined RxDelta+TxDelta bytes for one 500 ms tick.
	bandwidthHistory []int

	// uiEvents is a read-only channel of events pushed by agent goroutines.
	uiEvents <-chan agent.AgentEvent

	// Bubble Tea sub-components.
	streamList list.Model
	logVP      viewport.Model

	// logLines tracks rendered lines for viewport content rebuilds.
	logLines []string

	width    int
	height   int
	quitting bool
}

// New constructs a MainModel ready for tea.NewProgram.
// ctx/cancel must come from context.WithCancel(context.Background()) in main.
// uiEvents must be a buffered channel owned by main; it is never closed by TUI.
func New(ctx context.Context, cancel context.CancelFunc, uiEvents <-chan agent.AgentEvent) *MainModel {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true

	l := list.New(nil, delegate, 0, 0)
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.Title = "Active QUIC Streams"

	vp := viewport.New(0, 0)
	vp.SetContent("")

	return &MainModel{
		agentCtx:         ctx,
		cancel:           cancel,
		connectionStatus: ConnectionDisconnected,
		activeStreams:    make(map[string]StreamStats),
		logs:             make([]string, 0, maxLogs),
		bandwidthHistory: make([]int, 0, sparklineLen),
		uiEvents:         uiEvents,
		streamList:       l,
		logVP:            vp,
		logLines:         make([]string, 0, maxLogs),
	}
}
