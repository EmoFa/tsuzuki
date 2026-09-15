package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// settingsScreen shows the effective configuration. Editing happens in the
// config file.
type settingsScreen struct {
	lines []string
	list  list
}

func newSettings(s Settings) *settingsScreen {
	c := s.Config
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	section := func(name string) string { return styleSection.Render(name) }
	row := func(k, v string) string { return fmt.Sprintf("  %-22s %s", k, v) }

	lines := []string{
		section("Files"),
		row("config", s.ConfigPath),
		row("data", s.DataDir),
		row("cache & logs", s.CacheDir),
		section("Playback"),
		row("mode", c.General.Mode),
		row("quality", c.General.Quality),
		row("autoplay next", onOff(c.General.AutoplayNext)),
		row("watched at", fmt.Sprintf("%.0f%%", c.General.WatchedThreshold*100)),
		row("resume rewind", fmt.Sprintf("%ds", c.General.ResumeRewindSeconds)),
		section("Providers"),
		row("order", strings.Join(c.Providers.Order, " → ")),
		section("Player"),
		row("mpv", valueOr(c.Player.MpvPath, "auto-detect")),
		row("extra args", valueOr(strings.Join(c.Player.ExtraArgs, " "), "none")),
		section("Browser"),
		row("path", valueOr(c.Browser.Path, "auto-detect")),
		row("headless first", onOff(c.Browser.Headless)),
		row("auto download", onOff(c.Browser.AutoDownload)),
		section("Tracking"),
		row("backend", c.Tracking.Backend),
		section("Skipping"),
		row("opening / ending", c.Skip.Opening+" / "+c.Skip.Ending),
		row("recap", c.Skip.Recap),
		row("filler episodes", onOff(c.Skip.FillerEpisodes)),
		section("Discord"),
		row("rich presence", onOff(c.Discord.Enabled)),
		styleMuted.Render("Change these with `anitui config edit`, then restart anitui."),
	}
	st := &settingsScreen{lines: strings.Split(strings.Join(lines, "\n"), "\n")}
	st.list.setLen(len(st.lines))
	return st
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (s *settingsScreen) Init() tea.Cmd       { return nil }
func (s *settingsScreen) Title() string       { return "Settings" }
func (s *settingsScreen) Help() []key.Binding { return nil }

func (s *settingsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok {
		// Scroll by moving the offset directly; there is no selection.
		switch k.String() {
		case "down", "j":
			s.list.offset++
		case "up", "k":
			s.list.offset--
		}
		s.list.offset = min(max(s.list.offset, 0), max(len(s.lines)-1, 0))
	}
	return s, nil
}

func (s *settingsScreen) View(width, height int) string {
	start := min(s.list.offset, max(len(s.lines)-height, 0))
	end := min(start+height, len(s.lines))
	out := make([]string, 0, end-start)
	for _, l := range s.lines[start:end] {
		out = append(out, truncate(l, width))
	}
	return strings.Join(out, "\n")
}
