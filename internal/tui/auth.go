package tui

import (
	"fmt"
	"strings"

	"tunneledge/internal/tui/screen"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
)

type AuthScreen struct {
	bridge     *AgentBridge
	tokenInput  textinput.Model
	submitted   bool
	status     string
	err        string
	width      int
	height     int
}

func NewAuthScreen(bridge *AgentBridge) *AuthScreen {
	ti := textinput.New()
	ti.Placeholder = "Enter auth token..."
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256
	ti.Focus()

	cfg := bridge.GetConfig()
	ti.SetValue(cfg.Agent.Token)

	return &AuthScreen{
		bridge:     bridge,
		tokenInput: ti,
		status:     "Ready",
	}
}

func (a *AuthScreen) Init() tea.Cmd {
	return nil
}

func (a *AuthScreen) Update(msg tea.Msg) (screen.Screen, tea.Cmd) {
	a.submitted = false
	a.err = ""

	// Check for screen switching keys first - let them pass through to app
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 {
			r := keyMsg.Runes[0]
			if r >= '1' && r <= '5' {
				// Pass screen switching key through to app
				return a, tea.Sequence(func() tea.Msg { return msg })
			}
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "enter" {
			token := a.tokenInput.Value()
			if token == "" {
				a.err = "token cannot be empty"
				return a, nil
			}
			a.bridge.UpdateToken(token)
			a.submitted = true
			a.status = "Token updated"
			return a, nil
		}
		if msg.String() == "esc" {
			a.tokenInput.Blur()
			return a, nil
		}
	}

	var cmd tea.Cmd
	a.tokenInput, cmd = a.tokenInput.Update(msg)
	return a, cmd
}

func (a *AuthScreen) View() string {
	var b strings.Builder

	tableWidth := a.width - 6
	inputWidth := tableWidth - 12

	cfg := a.bridge.GetConfig()
	status := a.bridge.GetStatus()

	b.WriteString(styles.BoxTitle.Render("Authentication"))
	b.WriteString("\n")
	b.WriteString(divider(tableWidth))
	b.WriteString("\n")

	rows := []string{
		fmt.Sprintf("  %s: %s",
			styles.Label.Render("Gateway"),
			styles.Value.Render(cfg.Agent.GatewayAddr)),
		fmt.Sprintf("  %s: %s",
			styles.Label.Render("Status"),
			statusBadge(string(status.Status))),
		"  " + styles.Label.Render("Token:") + strings.Repeat(" ", 5),
	}

	b.WriteString(strings.Join(rows, "\n"))
	b.WriteString("\n")

	a.tokenInput.Width = inputWidth
	tokenLine := "  " + a.tokenInput.View() + strings.Repeat(" ", max(inputWidth-lipgloss.Width(a.tokenInput.View()), 0))
	b.WriteString(tokenLine)

	if a.tokenInput.Value() != "" {
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render("      (masked)"))
	}

	b.WriteString("\n")
	b.WriteString(divider(tableWidth))

	if a.submitted {
		b.WriteString("\n")
		b.WriteString(styles.Box.Render(
			styles.Success.Render("✓ "+a.status),
		))
	}
	if a.err != "" {
		b.WriteString("\n")
		b.WriteString(styles.Box.Render(
			styles.Error.Render("✗ "+a.err),
		))
	} else {
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render("  Enter:apply  Esc:cancel"))
	}

	return b.String()
}

func (a *AuthScreen) SetSize(width, height int) {
	a.width = width
	a.height = height
	a.tokenInput.Width = max(width-42, 20)
}
