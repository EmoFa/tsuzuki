package tui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/EmoFa/anitui/internal/session"
)

const playingLogLines = 6

type playingScreen struct {
	req     session.Request
	spinner spinner.Model

	episode  float64 // known once the session reports it
	provider string
	label    string
	started  bool
	watched  bool
	position time.Duration
	duration time.Duration
	stopping bool
	log      []string
}

func newPlaying(req session.Request) *playingScreen {
	return &playingScreen{
		req:     req,
		episode: req.Episode,
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
}

func (p *playingScreen) Init() tea.Cmd { return p.spinner.Tick }
func (p *playingScreen) Title() string { return "Now playing" }

var (
	keyNext  = key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next episode"))
	keyPrev  = key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous episode"))
	keyStop  = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop"))
	keyOther = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "try another provider"))
)

func (p *playingScreen) Help() []key.Binding {
	return []key.Binding{keyNext, keyPrev, keyOther, keyStop}
}

// Back stops playback; the screen closes once progress has been saved.
func (p *playingScreen) Back() tea.Cmd { return p.stop() }

func (p *playingScreen) stop() tea.Cmd {
	p.stopping = true
	return func() tea.Msg { return stopWatchMsg{} }
}

func (p *playingScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		p.spinner, cmd = p.spinner.Update(msg)
		return p, cmd

	case statusMsg:
		p.apply(msg.st)

	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keyStop):
			return p, p.stop()
		case key.Matches(msg, keyNext) && p.episode > 0:
			return p, p.jump(p.episode + 1)
		case key.Matches(msg, keyPrev) && p.episode > 1:
			return p, p.jump(p.episode - 1)
		case key.Matches(msg, keyOther) && p.provider != "" && p.episode > 0:
			return p, p.switchProvider()
		}
	}
	return p, nil
}

func (p *playingScreen) jump(episode float64) tea.Cmd {
	req := p.req
	// A different episode starts afresh: providers skipped for this one may work.
	req.Episode, req.Provider, req.SkipProviders = episode, p.provider, nil
	p.logf("Switching to episode %s…", episodeLabel(episode))
	return watch(req)
}

// switchProvider replays the current episode without the current provider (or
// any already abandoned). Stopping saves the position, so it resumes there.
func (p *playingScreen) switchProvider() tea.Cmd {
	req := p.req
	req.Episode, req.Provider = p.episode, ""
	req.SkipProviders = append(slices.Clone(req.SkipProviders), p.provider)
	p.logf("Switching away from %s…", p.provider)
	return watch(req)
}

func (p *playingScreen) apply(st session.Status) {
	if st.Episode > 0 && st.Episode != p.episode {
		// Autoplay moved on.
		p.episode, p.started, p.watched = st.Episode, false, false
		p.position, p.duration = 0, 0
	}
	switch st.Kind {
	case session.StatusResolving:
		p.logf("Looking for episode %s on %s…", episodeLabel(st.Episode), st.Provider)
	case session.StatusProviderFailed:
		p.logf("✗ %s", firstLine(st.Err.Error()))
	case session.StatusPlaying:
		p.started, p.provider, p.label = true, st.Provider, st.Stream.Label
		msg := fmt.Sprintf("▶ Playing from %s (%s)", st.Provider, st.Stream.Label)
		if st.Start > 0 {
			msg += " · resuming at " + clock(st.Start)
		}
		p.logf("%s", msg)
	case session.StatusProgress:
		p.position, p.duration = st.Position, st.Duration
	case session.StatusWatched:
		p.watched = true
		p.logf("✓ Episode %s marked as watched", episodeLabel(st.Episode))
	case session.StatusTracked:
		if st.Err != nil {
			p.logf("✗ Updating your list: %s", firstLine(st.Err.Error()))
		} else {
			p.logf("  %s", st.Reason)
		}
	case session.StatusStopped:
		if st.Reason == "eof" {
			p.logf("Episode %s finished", episodeLabel(st.Episode))
		}
	case session.StatusNoNextEpisode:
		p.logf("No episode after %s is available yet", episodeLabel(st.Episode))
	}
}

func (p *playingScreen) logf(format string, args ...any) {
	p.log = append(p.log, fmt.Sprintf(format, args...))
	if len(p.log) > 50 {
		p.log = p.log[len(p.log)-50:]
	}
}

func (p *playingScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString("\n" + styleTitle.Render(truncate(p.req.Media.DisplayTitle(), width)) + "\n")

	ep := "Next episode"
	if p.episode > 0 {
		ep = "Episode " + episodeLabel(p.episode)
	}
	sub := ep + " · " + string(p.req.Mode)
	if p.provider != "" {
		sub += " · " + p.provider + " " + p.label
	}
	b.WriteString(styleInfo.Render(truncate(sub, width)) + "\n\n")

	switch {
	case p.stopping:
		b.WriteString(p.spinner.View() + " Stopping and saving progress…\n")
	case !p.started:
		b.WriteString(p.spinner.View() + " Finding a stream…\n")
	case p.duration == 0:
		b.WriteString(p.spinner.View() + " Starting mpv…\n")
	default:
		b.WriteString(progressBar(p.position, p.duration, max(width-2, 10)) + "\n")
		status := fmt.Sprintf("%s / %s", clock(p.position), clock(p.duration))
		if p.watched {
			status += "  " + styleGood.Render("✓ watched")
		}
		b.WriteString(status + "\n")
		b.WriteString(styleMuted.Render("Playing in mpv's window. Use mpv's own keys to seek and pause.") + "\n")
	}

	if len(p.log) > 0 {
		b.WriteString("\n")
		for _, line := range p.log[max(len(p.log)-playingLogLines, 0):] {
			b.WriteString(styleMuted.Render(truncate(line, width)) + "\n")
		}
	}
	return b.String()
}

func progressBar(pos, dur time.Duration, width int) string {
	frac := 0.0
	if dur > 0 {
		frac = min(max(pos.Seconds()/dur.Seconds(), 0), 1)
	}
	filled := int(frac * float64(width))
	return styleSelected.Render(strings.Repeat("━", filled)) + styleMuted.Render(strings.Repeat("─", width-filled))
}
