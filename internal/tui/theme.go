package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var theme = struct {
	Primary       lipgloss.Color
	Secondary     lipgloss.Color
	Accent        lipgloss.Color
	Success       lipgloss.Color
	Warning       lipgloss.Color
	Error         lipgloss.Color
	Info          lipgloss.Color
	Background    lipgloss.Color
	Surface       lipgloss.Color
	Surface2      lipgloss.Color
	Text          lipgloss.Color
	Muted         lipgloss.Color
	Dimmed        lipgloss.Color
}{
	Primary:    "#00D4AA",
	Secondary:  "#626880",
	Accent:     "#8CAAEE",
	Success:    "#A6D189",
	Warning:    "#E5C890",
	Error:      "#E78284",
	Info:       "#8CAAEE",
	Background: "#11111b",
	Surface:    "#1a1a2e",
	Surface2:   "#232334",
	Text:       "#cdd6f4",
	Muted:      "#6c7086",
	Dimmed:     "#414559",
}

var border = lipgloss.NewStyle().
	Foreground(theme.Secondary).
	BorderStyle(lipgloss.NormalBorder())

var styles = struct {
	Title       lipgloss.Style
	Subtitle    lipgloss.Style
	ActiveItem  lipgloss.Style
	InactiveItem lipgloss.Style
	Header      lipgloss.Style
	Box         lipgloss.Style
	BoxTitle    lipgloss.Style
	Label       lipgloss.Style
	Value       lipgloss.Style
	Error       lipgloss.Style
	Success     lipgloss.Style
	Warning     lipgloss.Style
	Info        lipgloss.Style
	Muted       lipgloss.Style
	Dimmed      lipgloss.Style
	Bold        lipgloss.Style
	Footer       lipgloss.Style
	HelpKey     lipgloss.Style
	HelpDesc    lipgloss.Style
	TableHeader lipgloss.Style
	TableRow   lipgloss.Style
	TableSelRow lipgloss.Style
	StatusBar   lipgloss.Style
	Card       lipgloss.Style
	CardTitle   lipgloss.Style
	Separator   lipgloss.Style
	KeyBinding  lipgloss.Style
}{
	Title: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true).
		Padding(0, 1),
	Subtitle: lipgloss.NewStyle().
		Foreground(theme.Secondary).
		Padding(0, 1),
	ActiveItem: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true).
		Background(theme.Surface2).
		Padding(0, 1),
	InactiveItem: lipgloss.NewStyle().
		Foreground(theme.Text).
		Padding(0, 1),
	Header: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true).
		Background(theme.Surface).
		Padding(0, 1),
	Box: lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.Secondary).
		Background(theme.Surface).
		Padding(1, 1),
	BoxTitle: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true),
	Label: lipgloss.NewStyle().
		Foreground(theme.Info).
		Bold(true),
	Value: lipgloss.NewStyle().
		Foreground(theme.Text),
	Error: lipgloss.NewStyle().
		Foreground(theme.Error).
		Bold(true),
	Success: lipgloss.NewStyle().
		Foreground(theme.Success),
	Warning: lipgloss.NewStyle().
		Foreground(theme.Warning),
	Info: lipgloss.NewStyle().
		Foreground(theme.Info),
	Muted: lipgloss.NewStyle().
		Foreground(theme.Muted),
	Dimmed: lipgloss.NewStyle().
		Foreground(theme.Dimmed),
	Bold: lipgloss.NewStyle().
		Bold(true),
	Footer: lipgloss.NewStyle().
		Foreground(theme.Muted).
		Background(theme.Surface).
		Padding(0, 1),
	HelpKey: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true),
	HelpDesc: lipgloss.NewStyle().
		Foreground(theme.Muted),
	TableHeader: lipgloss.NewStyle().
		Foreground(theme.Info).
		Bold(true).
		Background(theme.Surface2).
		Padding(0, 1),
	TableRow: lipgloss.NewStyle().
		Foreground(theme.Text),
	TableSelRow: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true).
		Background(theme.Surface2),
	StatusBar: lipgloss.NewStyle().
		Background(theme.Surface2).
		Foreground(theme.Text).
		Padding(0, 1),
	Card: lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(theme.Secondary).
		Background(theme.Surface).
		Padding(0, 0),
	CardTitle: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true).
		Padding(0, 1),
	Separator: lipgloss.NewStyle().
		Foreground(theme.Secondary),
	KeyBinding: lipgloss.NewStyle().
		Foreground(theme.Primary).
		Bold(true),
}

func statusBadge(status string) string {
	switch status {
	case "connected":
		return styles.Success.Render("● CONNECTED")
	case "disconnected":
		return styles.Error.Render("○ DISCONNECTED")
	case "reconnecting":
		return styles.Warning.Render("◐ RECONNECTING")
	case "connecting":
		return styles.Info.Render("◔ CONNECTING")
	default:
		return styles.Muted.Render("○ UNKNOWN")
	}
}

func tunnelStatusBadge(active bool) string {
	if active {
		return styles.Success.Render("●")
	}
	return styles.Muted.Render("○")
}

func divider(width int) string {
	return lipgloss.NewStyle().
		Foreground(theme.Secondary).
		Render(strings.Repeat("─", width))
}

func renderBox(title, content string) string {
	return lipgloss.JoinVertical(
		lipgloss.Center,
		styles.BoxTitle.Render(title),
		border.Copy().Width(lipgloss.Width(content)).Render(content),
	)
}

func renderRow(cells []string, widths []int) string {
	var b strings.Builder
	for i, cell := range cells {
		if i > 0 {
			b.WriteString(" │ ")
		}
		b.WriteString(cell)
		if i < len(widths)-1 {
			remaining := widths[i] - lipgloss.Width(cell)
			b.WriteString(strings.Repeat(" ", max(remaining, 0)))
		}
	}
	return b.String()
}

func renderTableRow(cells []string, widths []int, selected bool) string {
	rowStyle := styles.TableRow
	if selected {
		rowStyle = styles.TableSelRow
	}
	return rowStyle.Render(renderRow(cells, widths))
}

func renderTableHeader(cells []string, widths []int) string {
	headerStyle := styles.TableHeader
	return headerStyle.Render(renderRow(cells, widths))
}
