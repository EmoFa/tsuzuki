package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

var (
	styleBrand    lipgloss.Style
	styleTitle    lipgloss.Style
	styleMuted    lipgloss.Style
	styleSelected lipgloss.Style
	styleGood     lipgloss.Style
	styleWarn     lipgloss.Style
	styleBad      lipgloss.Style
	styleInfo     lipgloss.Style
	styleKey      lipgloss.Style
	styleSection  lipgloss.Style
	styleHelpBox  lipgloss.Style
)

func init() { applyTheme("default") }

// applyTheme sets the styles for ui.theme. "default" uses the terminal's
// 16-colour palette so the UI follows the user's theme, light or dark; "mono"
// uses no colour at all, only bold and faint text.
func applyTheme(name string) {
	var accent, muted, good, warn, bad, info color.Color
	if name != "mono" {
		accent, muted = lipgloss.Color("5"), lipgloss.Color("8")
		good, warn, bad, info = lipgloss.Color("2"), lipgloss.Color("3"), lipgloss.Color("1"), lipgloss.Color("4")
	}
	fg := func(s lipgloss.Style, c color.Color) lipgloss.Style {
		if c == nil {
			return s
		}
		return s.Foreground(c)
	}
	bold := lipgloss.NewStyle().Bold(true)

	styleBrand = fg(bold, accent)
	styleTitle = bold
	styleMuted = fg(lipgloss.NewStyle(), muted)
	styleSelected = fg(bold, accent)
	styleGood = fg(lipgloss.NewStyle(), good)
	styleWarn = fg(lipgloss.NewStyle(), warn)
	styleBad = fg(lipgloss.NewStyle(), bad)
	styleInfo = fg(lipgloss.NewStyle(), info)
	styleKey = bold
	styleSection = fg(bold, accent).MarginTop(1)
	styleHelpBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
	if name == "mono" {
		styleMuted = styleMuted.Faint(true)
		styleBad = styleBad.Bold(true)
	} else {
		styleHelpBox = styleHelpBox.BorderForeground(accent)
	}
}
