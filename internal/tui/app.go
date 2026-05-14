package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"tunneledge/internal/tui/screen"
	"tunneledge/pkg/config"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

type App struct {
	bridge   *AgentBridge
	cfg      *config.Config
	cfgPath  string
	screens  map[screen.ID]screen.Screen
	active   screen.ID
	previous screen.ID
	width    int
	height   int
	help     bool
	quitting bool
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewApp(cfg *config.Config, cfgPath string) *App {
	ctx, cancel := context.WithCancel(context.Background())

	bridge := NewAgentBridge(cfg)

	return &App{
		bridge:  bridge,
		cfg:     cfg,
		cfgPath: cfgPath,
		screens: make(map[screen.ID]screen.Screen),
		active:  screen.DashboardID,
		ctx:     ctx,
		cancel:  cancel,
	}
}

func (a *App) Bridge() *AgentBridge {
	return a.bridge
}

func (a *App) Init() tea.Cmd {
	a.screens[screen.DashboardID] = NewDashboardScreen(a.bridge)
	a.screens[screen.ConfigID] = NewConfigScreen(a.bridge, a.cfgPath)
	a.screens[screen.AuthID] = NewAuthScreen(a.bridge)
	a.screens[screen.TunnelsID] = NewTunnelsScreen(a.bridge)
	a.screens[screen.LogsID] = NewLogsScreen(a.bridge)

	for _, s := range a.screens {
		s.SetSize(a.width, a.height-4)
	}

	a.bridge.Start(a.ctx)

	return tea.Batch(
		a.screens[a.active].Init(),
		a.tickStatus(),
		a.tickLogs(),
	)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle screen switching globally - takes priority over local input
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if a.handleGlobalKeys(keyMsg) {
			return a, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		for _, s := range a.screens {
			s.SetSize(a.width, a.height-4)
		}
		return a, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			a.quitting = true
			a.bridge.Stop()
			a.cancel()
			return a, tea.Quit
		}

		if keyMatch(msg, keys.Help) {
			a.help = !a.help
			return a, nil
		}

		if keyMatch(msg, keys.Quit) && a.active == screen.DashboardID && !a.isInputFocused() {
			a.quitting = true
			a.bridge.Stop()
			a.cancel()
			return a, tea.Quit
		}
	}

	var cmd tea.Cmd
	s, ok := a.screens[a.active]
	if !ok {
		return a, nil
	}
	updated, cmd := s.Update(msg)
	a.screens[a.active] = updated

	return a, cmd
}

func (a *App) handleGlobalKeys(msg tea.KeyMsg) bool {
	// If not a rune key (digit 1-5), skip screen switching
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return false
	}
	r := msg.Runes[0]
	// Check if it's a digit 1-5
	if r < '1' || r > '5' {
		return false
	}
	// It's a digit 1-5, handle screen switching
	return a.trySwitchScreen(msg)
}

func (a *App) trySwitchScreen(msg tea.KeyMsg) bool {
	var targetScreen screen.ID
	switch {
	case keyMatch(msg, keys.Screen1):
		targetScreen = screen.DashboardID
	case keyMatch(msg, keys.Screen2):
		targetScreen = screen.ConfigID
	case keyMatch(msg, keys.Screen3):
		targetScreen = screen.AuthID
	case keyMatch(msg, keys.Screen4):
		targetScreen = screen.TunnelsID
	case keyMatch(msg, keys.Screen5):
		targetScreen = screen.LogsID
	default:
		return false
	}

	if a.active == targetScreen {
		return true
	}

	_ = a.switchScreen(targetScreen)
	return true
}

func (a *App) View() string {
	if a.quitting {
		return "\n  TunnelEdge agent stopped.\n\n"
	}

	var b strings.Builder

	b.WriteString(a.renderHeader())
	b.WriteString("\n")

	s, ok := a.screens[a.active]
	if ok {
		if a.help {
			b.WriteString(a.renderHelpOverlay(s.View()))
		} else {
			b.WriteString(s.View())
		}
	}

	b.WriteString("\n")
	b.WriteString(a.renderFooter())

	return b.String()
}

func (a *App) renderHeader() string {
	status := a.bridge.GetStatus()

	leftPart := styles.Title.Render("TunnelEdge") + styles.Subtitle.Render(" Agent")
	badge := statusBadge(string(status.Status))

	rightPart := a.renderTabs()

	leftWidth := lipgloss.Width(leftPart)
	rightWidth := lipgloss.Width(rightPart)
	middleWidth := a.width - leftWidth - rightWidth - 4
	middle := strings.Repeat(" ", max(middleWidth, 0))

	return styles.Header.Render(fmt.Sprintf("%s%s%s%s", leftPart, middle, badge, rightPart))
}

func (a *App) renderTabs() string {
	tabs := []struct {
		id    screen.ID
		label string
	}{
		{screen.DashboardID, "1"},
		{screen.ConfigID, "2"},
		{screen.AuthID, "3"},
		{screen.TunnelsID, "4"},
		{screen.LogsID, "5"},
	}

	var parts []string
	for _, t := range tabs {
		if t.id == a.active {
			parts = append(parts, styles.ActiveItem.Render("["+t.label+"]"))
		} else {
			parts = append(parts, styles.Muted.Render("["+t.label+"]"))
		}
	}
	return strings.Join(parts, " ")
}

func (a *App) renderFooter() string {
	helpHint := styles.HelpKey.Render("?") + styles.HelpDesc.Render("help")
	quitHint := styles.HelpKey.Render("q") + styles.HelpDesc.Render("quit")
	return styles.Footer.Render(fmt.Sprintf("%s  │  %s", helpHint, quitHint))
}

func (a *App) renderHelpOverlay(content string) string {
	helpText := strings.Join([]string{
		styles.Title.Render("Key Bindings"),
		"",
		" " + styles.HelpKey.Render("↑/w/k ↓/s/j") + "  Navigate",
		" " + styles.HelpKey.Render("←/a/h →/d/l") + "  Navigate left/right",
		" " + styles.HelpKey.Render("Enter") + "       Select / Confirm",
		" " + styles.HelpKey.Render("Esc") + "         Back / Cancel",
		" " + styles.HelpKey.Render("1-5") + "       Switch screens",
		" " + styles.HelpKey.Render("?") + "         Toggle help",
		" " + styles.HelpKey.Render("Ctrl+C") + "     Force quit",
		"",
		"Screen-specific keys shown on each screen.",
	}, "\n")

	return styles.Box.Render(helpText) + "\n" + content
}

func (a *App) switchScreen(id screen.ID) tea.Cmd {
	a.previous = a.active
	a.active = id
	if s, ok := a.screens[id]; ok {
		return s.Init()
	}
	return nil
}

func (a *App) isInputFocused() bool {
	if a.active == screen.ConfigID || a.active == screen.AuthID {
		return true
	}
	return false
}

func (a *App) tickStatus() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return statusTickMsg{}
	})
}

func (a *App) tickLogs() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return logEntryMsg{}
	})
}
