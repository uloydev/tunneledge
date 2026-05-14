package tui

import tea "github.com/charmbracelet/bubbletea"

type keyBindings struct {
	Up         []string
	Down       []string
	Left       []string
	Right      []string
	Enter       []string
	Escape     []string
	Quit       []string
	Help       []string
	Save       []string
	Add        []string
	Edit       []string
	Delete      []string
	Filter      []string
	Home       []string
	End        []string
	PageUp      []string
	PageDown   []string
	Backspace   []string
	Space       []string
	Tab         []string
	Screen1     []string
	Screen2     []string
	Screen3     []string
	Screen4     []string
	Screen5     []string
}

var keys = keyBindings{
	Up:         []string{"up", "w", "k"},
	Down:       []string{"down", "s", "j"},
	Left:       []string{"left", "a", "h"},
	Right:      []string{"right", "d", "l"},
	Enter:       []string{"enter"},
	Escape:     []string{"esc"},
	Quit:       []string{"q", "ctrl+c"},
	Help:       []string{"?", "f1"},
	Save:       []string{"ctrl+s"},
	Add:        []string{"a", "ctrl+n"},
	Edit:       []string{"e", "ctrl+e"},
	Delete:      []string{"d", "ctrl+d", "delete"},
	Filter:      []string{"f", "/"},
	Home:       []string{"g", "home"},
	End:        []string{"G", "end"},
	PageUp:      []string{"pgup", "ctrl+b"},
	PageDown:   []string{"pgdown", "ctrl+f"},
	Backspace:   []string{"backspace"},
	Space:       []string{" "},
	Tab:         []string{"tab"},
	Screen1:     []string{"1", "alt+1"},
	Screen2:     []string{"2", "alt+2"},
	Screen3:     []string{"3", "alt+3"},
	Screen4:     []string{"4", "alt+4"},
	Screen5:     []string{"5", "alt+5"},
}

func keyMatch(msg tea.KeyMsg, bindings []string) bool {
	if msg.Type != tea.KeyRunes {
		s := msg.String()
		for _, b := range bindings {
			if s == b {
				return true
			}
		}
		return false
	}

	if len(msg.Runes) == 0 {
		return false
	}
	r := string(msg.Runes[0])
	for _, b := range bindings {
		if r == b {
			return true
		}
	}
	return false
}

func keyMatchString(msg tea.KeyMsg, s string) bool {
	if msg.Type != tea.KeyRunes {
		return msg.String() == s
	}
	if len(msg.Runes) == 0 {
		return false
	}
	return string(msg.Runes[0]) == s
}

type screenSwitchMsg struct {
	index int
}

type helpToggleMsg struct{}

type statusTickMsg struct{}

type logEntryMsg struct{}
