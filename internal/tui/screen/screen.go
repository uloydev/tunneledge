package screen

import "github.com/charmbracelet/bubbletea"

type Screen interface {
	Init() tea.Cmd
	Update(tea.Msg) (Screen, tea.Cmd)
	View() string
	SetSize(width, height int)
}

type ID int

const (
	DashboardID ID = iota
	ConfigID
	AuthID
	TunnelsID
	LogsID
)
