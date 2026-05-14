package tui

import (
	"fmt"
	"strings"
	"time"

	"tunneledge/internal/agent"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// agentEventMsg wraps an AgentEvent for dispatch through the Bubble Tea runtime.
type agentEventMsg struct{ ev agent.AgentEvent }

// listenForEvents returns a blocking tea.Cmd that reads exactly one event from
// the channel. It is re-queued after every delivery to keep the loop alive.
// If the channel is closed, the command returns nil (no-op).
func listenForEvents(ch <-chan agent.AgentEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return agentEventMsg{ev: ev}
	}
}

// Init seeds the Bubble Tea runtime with the event listener.
func (m *MainModel) Init() tea.Cmd {
	return listenForEvents(m.uiEvents)
}

// Update is the central Bubble Tea update loop.
func (m *MainModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			// Cancel root context to trigger graceful shutdown of all QUIC
			// streams before the TUI exits.
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		}
		// Forward remaining keys to the stream list for scrolling.
		var cmd tea.Cmd
		m.streamList, cmd = m.streamList.Update(msg)
		return m, cmd

	case agentEventMsg:
		m.handleAgentEvent(msg.ev)
		// Re-subscribe immediately so the next event is never missed.
		return m, listenForEvents(m.uiEvents)
	}

	// Pass unhandled messages to sub-components.
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.logVP, cmd = m.logVP.Update(msg)
	cmds = append(cmds, cmd)
	m.streamList, cmd = m.streamList.Update(msg)
	cmds = append(cmds, cmd)
	return m, tea.Batch(cmds...)
}

// handleAgentEvent maps each AgentEvent variant to a state mutation.
// Called exclusively from Update — no locking required for TUI-owned state.
func (m *MainModel) handleAgentEvent(ev agent.AgentEvent) {
	switch ev := ev.(type) {
	case agent.StatusUpdateEvent:
		m.connectionStatus = ev.Status
		if ev.Endpoint != "" {
			m.tunnelEndpoint = ev.Endpoint
		}
		m.appendLog(renderStatusLine(ev))

	case agent.StreamOpenedEvent:
		m.mu.Lock()
		m.activeStreams[ev.StreamID] = StreamStats{
			StreamID:  ev.StreamID,
			Label:     ev.Label,
			LocalAddr: ev.LocalAddr,
			OpenedAt:  time.Now(),
		}
		m.mu.Unlock()
		m.syncStreamList()
		m.appendLog(styleStreamOpen.Render(fmt.Sprintf(
			"OPEN  %-10s %-12s → %s", ev.StreamID, ev.Label, ev.LocalAddr,
		)))

	case agent.StreamClosedEvent:
		m.mu.Lock()
		delete(m.activeStreams, ev.StreamID)
		m.mu.Unlock()
		m.syncStreamList()
		m.appendLog(styleStreamClose.Render(fmt.Sprintf(
			"CLOSE %-10s reason=%-20s  ↓%s ↑%s",
			ev.StreamID, truncate(ev.Reason, 20),
			formatBytes(ev.SentBytes), formatBytes(ev.ReceivedBytes),
		)))

	case agent.TelemetryTickEvent:
		m.rxBytes += ev.RxDelta
		m.txBytes += ev.TxDelta
		sample := int(ev.RxDelta + ev.TxDelta)
		if len(m.bandwidthHistory) >= sparklineLen {
			m.bandwidthHistory = m.bandwidthHistory[1:]
		}
		m.bandwidthHistory = append(m.bandwidthHistory, sample)

	case agent.LogEvent:
		m.appendLog(renderLogEvent(ev))
	}
}

// appendLog adds a rendered line to the ring buffer and refreshes the viewport.
func (m *MainModel) appendLog(line string) {
	if len(m.logs) >= maxLogs {
		m.logs = m.logs[1:]
	}
	m.logs = append(m.logs, line)
	m.logVP.SetContent(strings.Join(m.logs, "\n"))
	m.logVP.GotoBottom()
}

// syncStreamList rebuilds the bubbles/list items from activeStreams.
func (m *MainModel) syncStreamList() {
	m.mu.RLock()
	items := make([]list.Item, 0, len(m.activeStreams))
	for _, s := range m.activeStreams {
		items = append(items, streamItem{stats: s})
	}
	m.mu.RUnlock()
	m.streamList.SetItems(items)
}

// resize recalculates component dimensions from the current terminal size.
func (m *MainModel) resize() {
	// Reserve: header(3) + divider(1) + footer(3) + margin(1) = 8 rows.
	bodyH := m.height - 8
	if bodyH < 5 {
		bodyH = 5
	}
	leftW := m.width / 3
	rightW := m.width - leftW - 3 // 3 for divider + padding
	if rightW < 10 {
		rightW = 10
	}
	m.streamList.SetSize(leftW-2, bodyH)
	m.logVP.Width = rightW
	m.logVP.Height = bodyH
}

// renderStatusLine produces a log line for a connection status change.
func renderStatusLine(ev agent.StatusUpdateEvent) string {
	badge := statusBadge(ev.Status)
	if ev.Endpoint != "" {
		return badge + "  endpoint=" + styleValue.Render(ev.Endpoint)
	}
	return badge
}

// renderLogEvent formats a LogEvent with level-based colour coding.
func renderLogEvent(ev agent.LogEvent) string {
	var levelTag string
	switch strings.ToLower(ev.Level) {
	case "error", "fatal", "panic":
		levelTag = styleErr.Render("ERR")
	case "warn", "warning":
		levelTag = styleWarn.Render("WRN")
	case "debug", "trace":
		levelTag = styleMuted.Render("DBG")
	default:
		levelTag = styleInfo.Render("INF")
	}
	msg := ev.Message
	if len(ev.Fields) > 0 {
		parts := make([]string, 0, len(ev.Fields))
		for k, v := range ev.Fields {
			parts = append(parts, styleMuted.Render(k+"=")+v)
		}
		msg += "  " + strings.Join(parts, " ")
	}
	return levelTag + " " + msg
}

// truncate shortens s to at most n runes, appending "…" when trimmed.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
