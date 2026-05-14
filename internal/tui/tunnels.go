package tui

import (
	"fmt"
	"strings"

	"tunneledge/internal/tui/screen"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
)

type tunnelsMode int

const (
	tunnelsList tunnelsMode = iota
	tunnelsAddLabel
	tunnelsAddAddr
	tunnelsConfirmDelete
)

type TunnelsScreen struct {
	bridge    *AgentBridge
	mode      tunnelsMode
	cursor    int
	tunnels   []TunnelStatus
	width     int
	height    int
	err       string

	labelInput textinput.Model
	addrInput  textinput.Model
	deleteIdx  int
}

func NewTunnelsScreen(bridge *AgentBridge) *TunnelsScreen {
	li := textinput.New()
	li.Placeholder = "e.g. web"
	li.CharLimit = 64
	li.Focus()

	ai := textinput.New()
	ai.Placeholder = "e.g. localhost:3000"
	ai.CharLimit = 128

	return &TunnelsScreen{
		bridge:     bridge,
		mode:       tunnelsList,
		labelInput: li,
		addrInput:  ai,
	}
}

func (t *TunnelsScreen) Init() tea.Cmd {
	return nil
}

func (t *TunnelsScreen) Update(msg tea.Msg) (screen.Screen, tea.Cmd) {
	t.err = ""
	t.tunnels = t.bridge.GetTunnelStatuses()

	switch t.mode {
	case tunnelsAddLabel:
		return t.updateAddLabel(msg)
	case tunnelsAddAddr:
		return t.updateAddAddr(msg)
	case tunnelsConfirmDelete:
		return t.updateConfirmDelete(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keyMatch(msg, keys.Up) {
			if t.cursor > 0 {
				t.cursor--
			}
			return t, nil
		}
		if keyMatch(msg, keys.Down) {
			if t.cursor < len(t.tunnels)-1 {
				t.cursor++
			}
			return t, nil
		}
		if keyMatch(msg, keys.Add) {
			t.mode = tunnelsAddLabel
			t.labelInput.SetValue("")
			t.labelInput.Focus()
			t.addrInput.SetValue("")
			return t, nil
		}
		if keyMatch(msg, keys.Delete) && len(t.tunnels) > 0 {
			t.mode = tunnelsConfirmDelete
			t.deleteIdx = t.cursor
			return t, nil
		}
	}

	return t, nil
}

func (t *TunnelsScreen) updateAddLabel(msg tea.Msg) (screen.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			t.mode = tunnelsList
			t.labelInput.Blur()
			return t, nil
		}
		if msg.String() == "enter" {
			if t.labelInput.Value() == "" {
				t.err = "label cannot be empty"
				return t, nil
			}
			t.mode = tunnelsAddAddr
			t.labelInput.Blur()
			t.addrInput.Focus()
			return t, nil
		}
	}

	var cmd tea.Cmd
	t.labelInput, cmd = t.labelInput.Update(msg)
	return t, cmd
}

func (t *TunnelsScreen) updateAddAddr(msg tea.Msg) (screen.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "esc" {
			t.mode = tunnelsList
			t.addrInput.Blur()
			return t, nil
		}
		if msg.String() == "enter" {
			if t.addrInput.Value() == "" {
				t.err = "address cannot be empty"
				return t, nil
			}
			if err := t.bridge.AddTunnel(t.labelInput.Value(), t.addrInput.Value()); err != nil {
				t.err = err.Error()
				return t, nil
			}
			t.mode = tunnelsList
			t.addrInput.Blur()
			t.tunnels = t.bridge.GetTunnelStatuses()
			t.cursor = len(t.tunnels) - 1
			return t, nil
		}
	}

	var cmd tea.Cmd
	t.addrInput, cmd = t.addrInput.Update(msg)
	return t, cmd
}

func (t *TunnelsScreen) updateConfirmDelete(msg tea.Msg) (screen.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "y" || msg.String() == "Y" {
			if t.deleteIdx < len(t.tunnels) {
				label := t.tunnels[t.deleteIdx].Label
				if err := t.bridge.RemoveTunnel(label); err != nil {
					t.err = err.Error()
				} else {
					t.tunnels = t.bridge.GetTunnelStatuses()
					if t.cursor >= len(t.tunnels) && t.cursor > 0 {
						t.cursor--
					}
				}
			}
			t.mode = tunnelsList
			return t, nil
		}
		t.mode = tunnelsList
		return t, nil
	}

	return t, nil
}

func (t *TunnelsScreen) View() string {
	var b strings.Builder

	tableWidth := t.width - 4

	b.WriteString(styles.BoxTitle.Render("Tunnel Management"))
	b.WriteString("\n")
	b.WriteString(divider(tableWidth))
	b.WriteString("\n")

	helpLine := styles.Muted.Render("  a:add  d:delete")
	b.WriteString(helpLine)
	b.WriteString("\n")
	b.WriteString(divider(tableWidth))
	b.WriteString("\n")

	switch t.mode {
	case tunnelsAddLabel:
		b.WriteString(t.renderAddLabelForm(tableWidth))
	case tunnelsAddAddr:
		b.WriteString(t.renderAddAddrForm(tableWidth))
	case tunnelsConfirmDelete:
		b.WriteString(t.renderConfirmDelete(tableWidth))
	default:
		b.WriteString(t.renderTunnelTable(tableWidth))
	}

	if t.err != "" {
		b.WriteString("\n")
		b.WriteString(styles.Box.Render(
			styles.Error.Render("✗ "+t.err),
		))
	}

	return b.String()
}

func (t *TunnelsScreen) SetSize(width, height int) {
	t.width = width
	t.height = height
}

func (t *TunnelsScreen) renderAddLabelForm(width int) string {
	inputWidth := width - 14

	t.labelInput.Width = inputWidth

	return fmt.Sprintf("  %s\n  %s\n\n%s",
		styles.Title.Render("Add Tunnel — Step 1/2: Label"),
		styles.Label.Render("Label:"),
		t.labelInput.View(),
	)
}

func (t *TunnelsScreen) renderAddAddrForm(width int) string {
	inputWidth := width - 14

	t.addrInput.Width = inputWidth

	title := styles.Title.Render("Add Tunnel — Step 2/2: Address")
	labelText := styles.Label.Render("Label:")
	labelValue := styles.Value.Render(t.labelInput.Value())
	addrText := styles.Label.Render("Address:")
	input := t.addrInput.View()

	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(title)
	b.WriteString("\n\n  ")
	b.WriteString(labelText)
	b.WriteString(" ")
	b.WriteString(labelValue)
	b.WriteString("\n  ")
	b.WriteString(addrText)
	b.WriteString("\n  ")
	b.WriteString(input)

	return b.String()
}

func (t *TunnelsScreen) renderConfirmDelete(width int) string {
	if t.deleteIdx < len(t.tunnels) {
		label := t.tunnels[t.deleteIdx].Label
		return styles.Box.Render(
			styles.Error.Render(fmt.Sprintf("Delete tunnel %q? (y/N)", label)) + "\n\n" +
				styles.Muted.Render("  y:confirm  any key:cancel"),
		)
	}
	return ""
}

func (t *TunnelsScreen) renderTunnelTable(width int) string {
	if len(t.tunnels) == 0 {
		return styles.Box.Render(
			styles.Info.Render("No tunnels configured.") + "\n\n" +
				styles.Success.Render("Press 'a' to add one."),
		)
	}

	labelWidth := 18
	addrWidth := 28
	statusWidth := 10

	headers := []string{"Label", "Local Address", "Status"}
	widths := []int{labelWidth, addrWidth, statusWidth}

	headerRow := renderTableHeader(headers, widths)
	divider := "│ " + strings.Repeat("─", labelWidth+addrWidth+statusWidth-8) + " │"

	var rows []string
	rows = append(rows, headerRow)
	rows = append(rows, divider)

	for i, tunnel := range t.tunnels {
		selected := i == t.cursor
		status := tunnelStatusBadge(tunnel.Active)

		rowStyle := styles.TableRow
		if selected {
			rowStyle = styles.TableSelRow
		}

		cells := []string{
			rowStyle.Render(fmt.Sprintf("%-*s", labelWidth-2, tunnel.Label)),
			rowStyle.Render(fmt.Sprintf("%-*s", addrWidth, tunnel.LocalAddr)),
			status,
		}

		rowLine := renderRow(cells, widths)
		rows = append(rows, "│ "+rowLine+" │")
	}

	rows = append(rows, strings.Repeat("─", width-2))
	return strings.Join(rows, "\n")
}
