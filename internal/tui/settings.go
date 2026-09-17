package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type loginDoneMsg struct {
	user string
	err  error
}

// syncDoneMsg reports a background or requested AniList sync.
type syncDoneMsg struct {
	note   string
	err    error
	silent bool // startup sync: only report failures
}

func syncCmd(ctx context.Context, svc Services, silent bool) tea.Cmd {
	return func() tea.Msg {
		note, err := svc.Sync(ctx)
		return syncDoneMsg{note, err, silent}
	}
}

// settingsScreen shows the effective configuration and the AniList account.
// Editing happens in the config file.
type settingsScreen struct {
	ctx     context.Context
	svc     Services
	lines   []string
	offset  int
	working string // "login" or "sync" while running
}

func newSettings(ctx context.Context, svc Services) *settingsScreen {
	s := &settingsScreen{ctx: ctx, svc: svc}
	s.build()
	return s
}

func (s *settingsScreen) build() {
	st, acct := s.svc.Settings(), s.svc.Account()
	c := st.Config
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	section := func(name string) string { return styleSection.Render(name) }
	row := func(k, v string) string { return fmt.Sprintf("  %-22s %s", k, v) }

	login := "not logged in · press L to log in"
	switch {
	case acct.Expired:
		login = acct.User + " (login expired) · press L to log in again"
	case acct.LoggedIn:
		login = styleGood.Render(acct.User) + " · press S to sync now"
	}
	sync := "your list syncs to AniList when logged in"
	if acct.Backend != "anilist" {
		sync = "off (tracking.backend = \"local\")"
	}

	lines := []string{
		section("AniList"),
		row("account", login),
		row("sync", sync),
		section("Files"),
		row("config", st.ConfigPath),
		row("data", st.DataDir),
		row("cache & logs", st.CacheDir),
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
		section("Skipping"),
		row("opening / ending", c.Skip.Opening+" / "+c.Skip.Ending),
		row("recap", c.Skip.Recap),
		row("filler episodes", onOff(c.Skip.FillerEpisodes)),
		section("Subtitles"),
		row("languages", valueOr(strings.Join(c.Subtitles.Languages, ", "), "any")),
		row("show", onOff(c.Subtitles.Show)),
		section("Discord"),
		row("rich presence", onOff(c.Discord.Enabled)),
		row("cover art", onOff(c.Discord.ShowCover)),
		section("Interface"),
		row("theme", c.UI.Theme),
		"",
		styleMuted.Render("Change these with `tsuzuki config edit`, then restart tsuzuki."),
	}
	s.lines = strings.Split(strings.Join(lines, "\n"), "\n")
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

var (
	keyLogin = key.NewBinding(key.WithKeys("L"), key.WithHelp("L", "log in to AniList"))
	keySync  = key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sync list"))
)

func (s *settingsScreen) Init() tea.Cmd    { return nil }
func (s *settingsScreen) Title() string    { return "Settings" }
func (s *settingsScreen) Refresh() tea.Cmd { s.build(); return nil }

func (s *settingsScreen) Help() []key.Binding {
	if s.svc.Account().LoggedIn {
		return []key.Binding{keySync, keyLogin}
	}
	return []key.Binding{keyLogin}
}

func (s *settingsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case loginDoneMsg:
		s.working = ""
		s.build()
		if msg.err != nil {
			return s, toast("Login failed: "+firstLine(msg.err.Error()), true)
		}
		return s, tea.Batch(toast("Logged in to AniList as "+msg.user+". Syncing your list…", false), syncCmd(s.ctx, s.svc, false))
	case syncDoneMsg:
		s.working = ""
		s.build()
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyLogin) && s.working == "":
			s.working = "login"
			ctx, svc := s.ctx, s.svc
			return s, tea.Batch(toast("Approve tsuzuki in the AniList page that opened in your browser…", false), func() tea.Msg {
				user, err := svc.Login(ctx)
				return loginDoneMsg{user, err}
			})
		case key.Matches(msg, keySync) && s.working == "" && s.svc.Account().LoggedIn:
			s.working = "sync"
			return s, tea.Batch(toast("Syncing with AniList…", false), syncCmd(s.ctx, s.svc, false))
		case msg.String() == "down" || msg.String() == "j":
			s.offset++
		case msg.String() == "up" || msg.String() == "k":
			s.offset--
		}
		s.offset = min(max(s.offset, 0), max(len(s.lines)-1, 0))
	}
	return s, nil
}

func (s *settingsScreen) View(width, height int) string {
	start := min(s.offset, max(len(s.lines)-height, 0))
	end := min(start+height, len(s.lines))
	out := make([]string, 0, end-start)
	for _, l := range s.lines[start:end] {
		out = append(out, truncate(l, width))
	}
	return strings.Join(out, "\n")
}
