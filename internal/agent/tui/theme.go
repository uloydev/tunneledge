package tui

import "github.com/charmbracelet/lipgloss"

var (
	colorPrimary   = lipgloss.Color("#00D4AA")
	colorSuccess   = lipgloss.Color("#A6D189")
	colorWarning   = lipgloss.Color("#E5C890")
	colorError     = lipgloss.Color("#E78284")
	colorInfo      = lipgloss.Color("#8CAAEE")
	colorMuted     = lipgloss.Color("#6c7086")
	colorText      = lipgloss.Color("#cdd6f4")
	colorSurface   = lipgloss.Color("#1a1a2e")
	colorSurface2  = lipgloss.Color("#232334")
	colorCyan      = lipgloss.Color("#89DCEB")
	colorBorder    = lipgloss.Color("#626880")
	colorSparkline = lipgloss.Color("#00D4AA")
)

// Header / status bar.
var (
	styleHeader = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorText).
			Padding(0, 1).
			Width(0) // set dynamically in View

	styleBrand = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleEndpoint = lipgloss.NewStyle().
			Foreground(colorInfo)

	styleRxTx = lipgloss.NewStyle().
			Foreground(colorMuted)
)

// Connection status badges.
var (
	styleBadgeConnected = lipgloss.NewStyle().
				Foreground(colorSuccess).
				Bold(true)

	styleBadgeConnecting = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	styleBadgeReconnecting = lipgloss.NewStyle().
				Foreground(colorWarning).
				Bold(true)

	styleBadgeDisconnected = lipgloss.NewStyle().
				Foreground(colorError).
				Bold(true)
)

// Body panes.
var (
	stylePane = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorBorder)

	stylePaneTitle = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1)
)

// Log level styles.
var (
	styleErr         = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	styleWarn        = lipgloss.NewStyle().Foreground(colorWarning)
	styleInfo        = lipgloss.NewStyle().Foreground(colorInfo)
	styleMuted       = lipgloss.NewStyle().Foreground(colorMuted)
	styleValue       = lipgloss.NewStyle().Foreground(colorText)
	styleStreamOpen  = lipgloss.NewStyle().Foreground(colorCyan)
	styleStreamClose = lipgloss.NewStyle().Foreground(colorCyan)
)

// Footer.
var (
	styleFooter = lipgloss.NewStyle().
			Background(colorSurface).
			Foreground(colorMuted).
			Padding(0, 1)

	styleSparklineBar   = lipgloss.NewStyle().Foreground(colorSparkline)
	styleSparklineLabel = lipgloss.NewStyle().Foreground(colorMuted)
)

// statusBadge returns a coloured, labelled badge for a connection status string.
func statusBadge(status string) string {
	switch status {
	case ConnectionConnected:
		return styleBadgeConnected.Render("◉ Connected")
	case ConnectionConnecting:
		return styleBadgeConnecting.Render("◌ Connecting…")
	case ConnectionReconnecting:
		return styleBadgeReconnecting.Render("⟳ Reconnecting…")
	default:
		return styleBadgeDisconnected.Render("○ Disconnected")
	}
}
