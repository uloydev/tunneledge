package agentui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the complete TUI frame.
func (m *MainModel) View() string {
	if m.quitting {
		return "\n  " + styleBrand.Render("TunnelEdge") + "  agent stopped.\n\n"
	}
	if m.width == 0 {
		return "" // terminal size not yet received
	}

	var sb strings.Builder
	sb.WriteString(m.renderHeader())
	sb.WriteString("\n")
	sb.WriteString(m.renderBody())
	sb.WriteString("\n")
	sb.WriteString(m.renderFooter())
	return sb.String()
}

// renderHeader renders the full-width status bar:
//
//	TunnelEdge  ◉ Connected   tcp://…:7890   ↓ 1.2 MB  ↑ 800 KB
func (m *MainModel) renderHeader() string {
	brand := styleBrand.Render("TunnelEdge") + "  "
	badge := statusBadge(m.connectionStatus)

	endpoint := ""
	if m.tunnelEndpoint != "" {
		endpoint = "  " + styleEndpoint.Render(m.tunnelEndpoint)
	}

	rxTx := styleRxTx.Render(fmt.Sprintf(
		"  ↓ %-8s  ↑ %s",
		formatBytes(int64(m.rxBytes)),
		formatBytes(int64(m.txBytes)),
	))

	left := brand + badge + endpoint
	right := rxTx

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	pad := m.width - leftW - rightW - 2 // -2 for outer padding
	if pad < 0 {
		pad = 0
	}

	line := left + strings.Repeat(" ", pad) + right
	return styleHeader.Width(m.width).Render(line)
}

// renderBody renders the side-by-side split view.
// Left pane: stream list  |  Right pane: log viewport.
func (m *MainModel) renderBody() string {
	leftW := m.width/3 - 2
	if leftW < 10 {
		leftW = 10
	}

	leftContent := m.streamList.View()
	rightContent := m.logVP.View()

	leftPane := stylePane.
		Width(leftW).
		Height(m.logVP.Height + 2). // +2 for border
		Render(leftContent)

	rightPane := stylePane.
		Width(m.width - leftW - 5). // 5 = border(2) + divider gap(3)
		Height(m.logVP.Height + 2).
		Render(rightContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, "  ", rightPane)
}

// renderFooter renders the bandwidth sparkline and key hints.
func (m *MainModel) renderFooter() string {
	spark := renderSparkline(m.bandwidthHistory, m.width/2)
	sparkLabel := styleSparklineLabel.Render(" BW ")
	quitHint := styleMuted.Render("  q/ctrl+c quit")

	right := quitHint
	rightW := lipgloss.Width(right)
	leftPart := sparkLabel + styleSparklineBar.Render(spark)
	leftW := lipgloss.Width(leftPart)
	pad := m.width - leftW - rightW - 2
	if pad < 0 {
		pad = 0
	}

	line := leftPart + strings.Repeat(" ", pad) + right
	return styleFooter.Width(m.width).Render(line)
}
