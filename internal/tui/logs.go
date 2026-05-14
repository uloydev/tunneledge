package tui

import (
	"fmt"
	"strings"
	"time"

	"tunneledge/internal/tui/screen"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type LogsScreen struct {
	bridge     *AgentBridge
	width      int
	height     int
	tunnels    []TunnelStatus
	cursor     int
	logs       []LogEntry
	filter     string
	filterIdx  int
	filters    []string
	autoscroll bool
	selected   string
}

func NewLogsScreen(bridge *AgentBridge) *LogsScreen {
	return &LogsScreen{
		bridge:     bridge,
		filters:    []string{"all", "debug", "info", "warn", "error"},
		filterIdx:  0,
		filter:     "all",
		autoscroll: true,
	}
}

func (l *LogsScreen) Init() tea.Cmd {
	return nil
}

func (l *LogsScreen) Update(msg tea.Msg) (screen.Screen, tea.Cmd) {
	l.tunnels = l.bridge.GetTunnelStatuses()

	if len(l.tunnels) > 0 && l.selected == "" {
		l.selected = l.tunnels[0].Label
	}

	switch msg := msg.(type) {
	case statusTickMsg:
		l.loadLogs()
		return l, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
			return statusTickMsg{}
		})

	case logEntryMsg:
		l.loadLogs()
		return l, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
			return logEntryMsg{}
		})

	case tea.KeyMsg:
		if keyMatch(msg, keys.Up) {
			if l.cursor > 0 {
				l.cursor--
				l.autoscroll = false
			}
			return l, nil
		}
		if keyMatch(msg, keys.Down) {
			if l.cursor < len(l.logs)-1 {
				l.cursor++
			}
			if l.cursor >= len(l.logs)-1 {
				l.autoscroll = true
			}
			return l, nil
		}
		if keyMatch(msg, keys.PageUp) {
			l.cursor -= l.height
			if l.cursor < 0 {
				l.cursor = 0
			}
			l.autoscroll = false
			return l, nil
		}
		if keyMatch(msg, keys.PageDown) {
			l.cursor += l.height
			if l.cursor >= len(l.logs)-1 {
				l.cursor = max(len(l.logs)-1, 0)
				l.autoscroll = true
			}
			return l, nil
		}
		if keyMatch(msg, keys.Home) {
			l.cursor = 0
			l.autoscroll = false
			return l, nil
		}
		if keyMatch(msg, keys.End) {
			l.cursor = max(len(l.logs)-1, 0)
			l.autoscroll = true
			return l, nil
		}
		if keyMatch(msg, keys.Filter) {
			l.filterIdx = (l.filterIdx + 1) % len(l.filters)
			l.filter = l.filters[l.filterIdx]
			l.loadLogs()
			return l, nil
		}
		if keyMatch(msg, keys.Left) {
			tIdx := l.tunnelIndex(l.selected)
			if tIdx > 0 {
				tIdx--
				l.selected = l.tunnels[tIdx].Label
				l.loadLogs()
			}
			return l, nil
		}
		if keyMatch(msg, keys.Right) {
			tIdx := l.tunnelIndex(l.selected)
			if tIdx < len(l.tunnels)-1 {
				tIdx++
				l.selected = l.tunnels[tIdx].Label
				l.loadLogs()
			}
			return l, nil
		}
	}

	return l, nil
}

func (l *LogsScreen) View() string {
	var b strings.Builder

	tableWidth := l.width - 4

	b.WriteString(styles.BoxTitle.Render("Logs"))
	b.WriteString("\n")
	b.WriteString(divider(tableWidth))
	b.WriteString("\n")

	tunnelSelector := l.renderTunnelSelector()
	filterBadge := l.renderFilterBadge()

	b.WriteString(tunnelSelector)
	b.WriteString(strings.Repeat(" ", max(tableWidth-lipgloss.Width(tunnelSelector)-lipgloss.Width(filterBadge), 0)))
	b.WriteString(filterBadge)
	b.WriteString("\n")
	b.WriteString(divider(tableWidth))
	b.WriteString("\n")

	maxLines := l.height - 8
	if maxLines < 5 {
		maxLines = 5
	}

	if len(l.logs) == 0 {
		b.WriteString(styles.Muted.Render("  No logs yet."))
		return b.String()
	}

	timestampWidth := 10
	levelWidth := 8
	msgWidth := tableWidth - timestampWidth - levelWidth - 8

	headers := []string{"Time", "Level", "Message"}
	widths := []int{timestampWidth, levelWidth, msgWidth}

	headerRow := renderTableHeader(headers, widths)
	b.WriteString(headerRow)
	b.WriteString("\n")
	b.WriteString("│ " + strings.Repeat("─", timestampWidth+levelWidth+msgWidth-8) + " │")
	b.WriteString("\n")

	start := 0
	end := len(l.logs)

	if !l.autoscroll {
		half := maxLines / 2
		start = l.cursor - half
		if start < 0 {
			start = 0
		}
		end = start + maxLines
		if end > len(l.logs) {
			end = len(l.logs)
		}
	} else {
		l.cursor = max(len(l.logs)-1, 0)
		start = len(l.logs) - maxLines
		if start < 0 {
			start = 0
		}
		end = len(l.logs)
	}

	for i := start; i < end; i++ {
		entry := l.logs[i]

		var levelStyle = styles.TableRow
		switch strings.ToLower(entry.Level) {
		case "debug":
			levelStyle = styles.Muted
		case "info":
			levelStyle = styles.TableRow
		case "warn":
			levelStyle = styles.Warning
		case "error":
			levelStyle = styles.Error
		}

		timestamp := styles.Muted.Render(fmt.Sprintf("%-*s", timestampWidth, entry.Time.Format("15:04:05")))
		level := levelStyle.Render(fmt.Sprintf("%-*s", levelWidth, strings.ToUpper(entry.Level)))

		// Word-wrap the message across multiple rows so it never overflows.
		lines := wrapText(entry.Message, msgWidth)
		for j, line := range lines {
			msg := levelStyle.Render(fmt.Sprintf("%-*s", msgWidth, line))
			if j == 0 {
				rowLine := renderRow([]string{timestamp, level, msg}, widths)
				b.WriteString("│ " + rowLine + " │\n")
			} else {
				// Continuation: blank timestamp+level, indented message.
				blank := styles.Muted.Render(strings.Repeat(" ", timestampWidth))
				blankLevel := styles.Muted.Render(strings.Repeat(" ", levelWidth))
				rowLine := renderRow([]string{blank, blankLevel, msg}, widths)
				b.WriteString("│ " + rowLine + " │\n")
			}
		}
	}

	b.WriteString(divider(tableWidth))

	if !l.autoscroll {
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render(fmt.Sprintf("  [%d/%d]  ↑/↓:scroll  PgUp/PgDn:page  G/Home:top  G/End:bottom",
			l.cursor+1, len(l.logs))))
	}

	return b.String()
}

func (l *LogsScreen) SetSize(width, height int) {
	l.width = width
	l.height = height
}

func (l *LogsScreen) renderTunnelSelector() string {
	if len(l.tunnels) == 0 {
		return styles.Muted.Render("[no tunnels]")
	}

	var parts []string
	for _, t := range l.tunnels {
		if t.Label == l.selected {
			parts = append(parts, styles.ActiveItem.Render("["+t.Label+"]"))
		} else {
			parts = append(parts, styles.Muted.Render("["+t.Label+"]"))
		}
	}
	return styles.HelpKey.Render("←/→:") + " " + strings.Join(parts, " ")
}

func (l *LogsScreen) renderFilterBadge() string {
	var style = styles.Muted
	switch l.filter {
	case "all":
		style = styles.TableRow
	case "debug":
		style = styles.Muted
	case "info":
		style = styles.Info
	case "warn":
		style = styles.Warning
	case "error":
		style = styles.Error
	}
	return style.Render(fmt.Sprintf("[Filter: %s]", strings.ToUpper(l.filter)))
}

func (l *LogsScreen) loadLogs() {
	ring := l.bridge.LogWriter().Ring(l.selected)
	if ring == nil {
		l.logs = nil
		return
	}
	l.logs = ring.Filter(l.selected, l.filter)
	if l.autoscroll && len(l.logs) > 0 {
		l.cursor = len(l.logs) - 1
	}
}

func (l *LogsScreen) tunnelIndex(label string) int {
	for i, t := range l.tunnels {
		if t.Label == label {
			return i
		}
	}
	return 0
}

// wrapText splits s into lines of at most maxLen runes, breaking at word
// boundaries where possible. It never returns an empty slice.
func wrapText(s string, maxLen int) []string {
	if maxLen <= 0 {
		return []string{s}
	}
	// Fast path: fits on one line.
	if len([]rune(s)) <= maxLen {
		return []string{s}
	}

	var lines []string
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}

	current := ""
	for _, word := range words {
		wordRunes := []rune(word)
		// If a single word is longer than maxLen, hard-break it.
		if len(wordRunes) > maxLen {
			if current != "" {
				lines = append(lines, current)
				current = ""
			}
			for len(wordRunes) > 0 {
				cut := maxLen
				if cut > len(wordRunes) {
					cut = len(wordRunes)
				}
				lines = append(lines, string(wordRunes[:cut]))
				wordRunes = wordRunes[cut:]
			}
			continue
		}
		// Would adding this word overflow the current line?
		sep := ""
		if current != "" {
			sep = " "
		}
		if len([]rune(current))+len(sep)+len(wordRunes) > maxLen {
			lines = append(lines, current)
			current = word
		} else {
			current = current + sep + word
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
