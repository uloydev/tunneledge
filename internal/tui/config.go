package tui

import (
	"fmt"
	"strings"

	"tunneledge/internal/tui/screen"
	"tunneledge/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"
)

type configField struct {
	label   string
	key     string
	value   string
	editing bool
	input   textinput.Model
}

type ConfigScreen struct {
	bridge  *AgentBridge
	cfgPath string
	fields  []configField
	cursor  int
	width   int
	height  int
	saved   bool
	err     string
}

func NewConfigScreen(bridge *AgentBridge, cfgPath string) *ConfigScreen {
	c := &ConfigScreen{
		bridge:  bridge,
		cfgPath: cfgPath,
	}
	c.initFields()
	return c
}

func (c *ConfigScreen) initFields() {
	cfg := c.bridge.GetConfig()

	c.fields = []configField{
		c.makeField("Gateway Address", "gateway_addr", cfg.Agent.GatewayAddr),
		c.makeField("Log Level", "log_level", cfg.Log.Level),
		c.makeField("Log Format", "log_format", cfg.Log.Format),
		c.makeField("Metrics Enabled", "metrics_enabled", fmt.Sprintf("%v", cfg.Observability.MetricsEnabled)),
		c.makeField("Metrics Address", "metrics_addr", cfg.Observability.MetricsAddr),
		c.makeField("Reconnect Delay", "reconnect_delay", cfg.Agent.ReconnectDelay.String()),
		c.makeField("Max Reconnect", "max_reconnect", fmt.Sprintf("%d", cfg.Agent.MaxReconnect)),
		c.makeField("Heartbeat Interval", "heartbeat_interval", cfg.Agent.HeartbeatInterval.String()),
		c.makeField("QUIC Timeout", "quic_timeout", cfg.Agent.QUICTimeout.String()),
	}
}

func (c *ConfigScreen) makeField(label, key, value string) configField {
	ti := textinput.New()
	ti.SetValue(value)
	ti.CharLimit = 80
	return configField{
		label: label,
		key:   key,
		value: value,
		input: ti,
	}
}

func (c *ConfigScreen) Init() tea.Cmd {
	return nil
}

func (c *ConfigScreen) Update(msg tea.Msg) (screen.Screen, tea.Cmd) {
	c.saved = false
	c.err = ""

	// Check for screen switching keys first - let them pass through to app
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.Type == tea.KeyRunes && len(keyMsg.Runes) == 1 {
			r := keyMsg.Runes[0]
			if r >= '1' && r <= '5' {
				// Pass screen switching key through to app
				return c, tea.Sequence(func() tea.Msg { return msg })
			}
		}
	}

	if c.cursor < len(c.fields) && c.fields[c.cursor].editing {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				c.fields[c.cursor].value = c.fields[c.cursor].input.Value()
				c.fields[c.cursor].editing = false
				c.fields[c.cursor].input.Blur()
				c.applyField(c.cursor)
				return c, nil
			}
			if msg.String() == "esc" {
				c.fields[c.cursor].editing = false
				c.fields[c.cursor].input.Blur()
				return c, nil
			}
		}

		var cmd tea.Cmd
		c.fields[c.cursor].input, cmd = c.fields[c.cursor].input.Update(msg)
		return c, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keyMatch(msg, keys.Up) {
			if c.cursor > 0 {
				c.cursor--
			}
			return c, nil
		}
		if keyMatch(msg, keys.Down) {
			if c.cursor < len(c.fields)-1 {
				c.cursor++
			}
			return c, nil
		}
		if keyMatch(msg, keys.Enter) {
			c.fields[c.cursor].editing = true
			c.fields[c.cursor].input.SetValue(c.fields[c.cursor].value)
			c.fields[c.cursor].input.Focus()
			return c, nil
		}
		if keyMatch(msg, keys.Save) {
			c.saveConfig()
			return c, nil
		}
	}

	return c, nil
}

func (c *ConfigScreen) View() string {
	var b strings.Builder

	b.WriteString(styles.BoxTitle.Render("Configuration"))
	b.WriteString("\n")
	b.WriteString(divider(c.width-2))
	b.WriteString("\n")

	tableWidth := c.width - 6
	labelWidth := 22
	valueWidth := tableWidth - labelWidth - 10

	for i, f := range c.fields {
		cursor := " "
		if i == c.cursor {
			cursor = "▸"
		}

		var value string
		if f.editing {
			f.input.Width = valueWidth
			value = f.input.View()
		} else {
			value = styles.Value.Render(fmt.Sprintf("%-*s", valueWidth, f.value))
		}

		label := styles.Label.Render(fmt.Sprintf("%-*s", labelWidth, f.label))

		rowStyle := styles.TableRow
		if i == c.cursor && !f.editing {
			rowStyle = styles.TableSelRow
		}

		b.WriteString(fmt.Sprintf("%s│ %s │ %s │\n",
			rowStyle.Render(cursor+" "), label, value))
	}

	b.WriteString(divider(c.width-2))

	if c.saved {
		b.WriteString("\n")
		b.WriteString(styles.Box.Render(
			styles.Success.Render("✓ Configuration saved"),
		))
	}
	if c.err != "" {
		b.WriteString("\n")
		b.WriteString(styles.Box.Render(
			styles.Error.Render("✗ "+c.err),
		))
	} else {
		b.WriteString("\n")
		b.WriteString(styles.Muted.Render("  ↑/↓ navigate  Enter edit  Ctrl+S save  Esc cancel"))
	}

	return b.String()
}

func (c *ConfigScreen) SetSize(width, height int) {
	c.width = width
	c.height = height
}

func (c *ConfigScreen) applyField(idx int) {
	cfg := c.bridge.GetConfig()
	f := c.fields[idx]

	switch f.key {
	case "gateway_addr":
		cfg.Agent.GatewayAddr = f.value
	case "log_level":
		cfg.Log.Level = f.value
	case "log_format":
		cfg.Log.Format = f.value
	case "metrics_enabled":
		cfg.Observability.MetricsEnabled = f.value == "true"
	case "metrics_addr":
		cfg.Observability.MetricsAddr = f.value
	}

	c.bridge.UpdateConfig(cfg)
}

func (c *ConfigScreen) saveConfig() {
	cfg := c.bridge.GetConfig()

	if c.cfgPath == "" {
		c.err = "no config file path specified (use -c flag)"
		return
	}

	if err := config.Save(cfg, c.cfgPath); err != nil {
		c.err = err.Error()
		return
	}

	c.saved = true
}
