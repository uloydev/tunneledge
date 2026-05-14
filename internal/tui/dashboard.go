package tui

import (
	"fmt"
	"strings"
	"time"

	"tunneledge/internal/tui/screen"
	tea "github.com/charmbracelet/bubbletea"
)

type DashboardScreen struct {
	bridge  *AgentBridge
	cursor  int
	width   int
	height  int
	tunnels []TunnelStatus
	status  StatusUpdate
}

func NewDashboardScreen(bridge *AgentBridge) *DashboardScreen {
	return &DashboardScreen{
		bridge: bridge,
	}
}

func (d *DashboardScreen) Init() tea.Cmd {
	return nil
}

func (d *DashboardScreen) Update(msg tea.Msg) (screen.Screen, tea.Cmd) {
	switch msg.(type) {
	case statusTickMsg:
		d.status = d.bridge.GetStatus()
		d.tunnels = d.bridge.GetTunnelStatuses()
		if len(d.tunnels) > 0 {
			if d.cursor >= len(d.tunnels) {
				d.cursor = len(d.tunnels) - 1
			}
		}
		return d, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return statusTickMsg{}
		})
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keyMatch(msg, keys.Up) {
			if d.cursor > 0 {
				d.cursor--
			}
			return d, nil
		}
		if keyMatch(msg, keys.Down) {
			if d.cursor < len(d.tunnels)-1 {
				d.cursor++
			}
			return d, nil
		}
	}

	return d, nil
}

func (d *DashboardScreen) View() string {
	var b strings.Builder

	tableWidth := d.width - 4

	b.WriteString(d.renderStatusCard(tableWidth))
	b.WriteString("\n")
	b.WriteString(d.renderTunnelTable(tableWidth))
	b.WriteString("\n")
	b.WriteString(d.renderStatsCard(tableWidth))

	return b.String()
}

func (d *DashboardScreen) SetSize(width, height int) {
	d.width = width
	d.height = height
}

func (d *DashboardScreen) renderStatusCard(width int) string {
	s := d.status

	statusLine := ""
	switch s.Status {
	case StatusConnected:
		statusLine = fmt.Sprintf("%s  │  Tunnel: %s",
			statusBadge(string(StatusConnected)),
			styles.Value.Render(s.TunnelID))
	case StatusDisconnected:
		statusLine = fmt.Sprintf("%s", statusBadge(string(StatusDisconnected)))
	case StatusConnecting:
		statusLine = fmt.Sprintf("%s", statusBadge(string(StatusConnecting)))
	case StatusReconnecting:
		statusLine = fmt.Sprintf("%s", statusBadge(string(StatusReconnecting)))
	}

	if s.Error != "" {
		statusLine += fmt.Sprintf("\n%s", styles.Error.Render("Error: "+s.Error))
	}

	lines := []string{
		styles.Title.Render("┌─ Status ──────────────────────────┐"),
	}

	if statusLine != "" {
		lines = append(lines, "│ "+statusLine+" │")
	} else {
		padding := max(width-4, 0)
		lines = append(lines, "│ "+strings.Repeat(" ", padding)+" │")
	}

	lines = append(lines, "└──────────────────────────────────┘")

	return strings.Join(lines, "\n")
}

func (d *DashboardScreen) renderTunnelTable(width int) string {
	if len(d.tunnels) == 0 {
		return styles.Box.Render(
			styles.CardTitle.Render("Tunnels") + "\n\n" +
				styles.Muted.Render("No tunnels configured.") + "\n" +
				styles.Info.Render("Go to Tunnels (4) to add one."),
		)
	}

	tableWidth := width - 4
	labelWidth := 20
	addrWidth := 25
	urlWidth := tableWidth - labelWidth - addrWidth - 15 - 10

	headers := []string{"Label", "Local Addr", "Public URL", "Status"}
	widths := []int{labelWidth, addrWidth, urlWidth, 10}

	headerRow := renderTableHeader(headers, widths)
	dividerWidth := max(labelWidth+addrWidth+urlWidth+10-8, 0)
	divider := "│ " + strings.Repeat("─", dividerWidth) + " │"

	var rows []string
	rows = append(rows, styles.CardTitle.Render("Tunnels"))
	rows = append(rows, strings.Repeat("─", max(width-2, 0)))
	rows = append(rows, headerRow)
	rows = append(rows, divider)

	for i, t := range d.tunnels {
		selected := i == d.cursor
		status := tunnelStatusBadge(t.Active)

		var url string
		if t.PublicURL != "" {
			url = styles.Value.Render(t.PublicURL)
		} else {
			url = styles.Muted.Render("—")
		}

		rowStyle := styles.TableRow
		if selected {
			rowStyle = styles.TableSelRow
		}

		cells := []string{
			rowStyle.Render(fmt.Sprintf("%-*s", labelWidth-2, t.Label)),
			rowStyle.Render(fmt.Sprintf("%-*s", addrWidth, t.LocalAddr)),
			rowStyle.Render(fmt.Sprintf("%-*s", urlWidth, url)),
			status,
		}

		rowLine := renderRow(cells, widths)
		rows = append(rows, "│ "+rowLine+" │")
	}

	rows = append(rows, strings.Repeat("─", max(width-2, 0)))
	return strings.Join(rows, "\n")
}

func (d *DashboardScreen) renderStatsCard(width int) string {
	s := d.status

	stats := []struct {
		label string
		value string
	}{
		{"Active Streams", fmt.Sprintf("%d", s.ActiveStreams)},
		{"Sent", formatBytes(s.Sent)},
		{"Received", formatBytes(s.Received)},
		{"Uptime", formatDuration(s.Uptime)},
	}

	var lines []string
	lines = append(lines, styles.CardTitle.Render("Statistics"))
	lines = append(lines, strings.Repeat("─", max(width-2, 0)))

	for _, stat := range stats {
		line := fmt.Sprintf("  %s: %s",
			styles.Label.Render(stat.label),
			styles.Value.Render(stat.value))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d interface{}) string {
	return fmt.Sprintf("%v", d)
}
