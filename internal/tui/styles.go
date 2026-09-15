package tui

import "charm.land/lipgloss/v2"

// Colours use the terminal's 16-colour palette so the UI follows the user's
// theme, light or dark.
var (
	colorAccent = lipgloss.Color("5")
	colorMuted  = lipgloss.Color("8")
	colorGood   = lipgloss.Color("2")
	colorWarn   = lipgloss.Color("3")
	colorBad    = lipgloss.Color("1")
	colorInfo   = lipgloss.Color("4")
)

var (
	styleBrand    = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleTitle    = lipgloss.NewStyle().Bold(true)
	styleMuted    = lipgloss.NewStyle().Foreground(colorMuted)
	styleSelected = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	styleGood     = lipgloss.NewStyle().Foreground(colorGood)
	styleWarn     = lipgloss.NewStyle().Foreground(colorWarn)
	styleBad      = lipgloss.NewStyle().Foreground(colorBad)
	styleInfo     = lipgloss.NewStyle().Foreground(colorInfo)
	styleKey      = lipgloss.NewStyle().Bold(true)
	styleSection  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent).MarginTop(1)
	styleHelpBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colorAccent).Padding(1, 2)
)
