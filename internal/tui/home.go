package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/session"
	"github.com/EmoFa/anitui/internal/store"
)

const recentLimit = 30

type homeItem struct {
	media    anilist.Media
	progress store.Progress
}

type homeLoadedMsg struct {
	items []homeItem
	err   error
}

type homeScreen struct {
	ctx     context.Context
	svc     Services
	loading bool
	err     error
	items   []homeItem
	list    list
}

func newHome(ctx context.Context, svc Services) *homeScreen {
	return &homeScreen{ctx: ctx, svc: svc, loading: true}
}

func (h *homeScreen) Init() tea.Cmd    { return h.load() }
func (h *homeScreen) Refresh() tea.Cmd { return h.load() }
func (h *homeScreen) Title() string    { return "Home" }

func (h *homeScreen) load() tea.Cmd {
	ctx, svc := h.ctx, h.svc
	return func() tea.Msg {
		recent, err := svc.RecentShows(ctx, recentLimit)
		if err != nil {
			return homeLoadedMsg{err: err}
		}
		items := make([]homeItem, 0, len(recent))
		for _, p := range recent {
			m, err := svc.Media(ctx, p.MediaID)
			if err != nil {
				m = anilist.Media{ID: p.MediaID, Title: anilist.Title{Romaji: fmt.Sprintf("AniList #%d", p.MediaID)}}
			}
			items = append(items, homeItem{media: m, progress: p})
		}
		return homeLoadedMsg{items: items}
	}
}

var (
	keyResume   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue"))
	keyDetails  = key.NewBinding(key.WithKeys("d", "right", "l"), key.WithHelp("d", "details"))
	keySearch   = key.NewBinding(key.WithKeys("/", "s"), key.WithHelp("/", "search"))
	keySettings = key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings"))
)

func (h *homeScreen) Help() []key.Binding {
	if len(h.items) == 0 {
		return []key.Binding{keySearch, keySettings}
	}
	return []key.Binding{keyResume, keyDetails, keySearch, keySettings}
}

func (h *homeScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case homeLoadedMsg:
		h.loading, h.err = false, msg.err
		if msg.err == nil {
			h.items = msg.items
			h.list.setLen(len(h.items))
		}
	case tea.KeyPressMsg:
		switch {
		case key.Matches(msg, keySearch):
			return h, push(newSearch(h.ctx, h.svc))
		case key.Matches(msg, keySettings):
			return h, push(newSettings(h.svc.Settings()))
		}
		if len(h.items) == 0 {
			return h, nil
		}
		it := h.items[h.list.cursor]
		switch {
		case key.Matches(msg, keyResume):
			if caughtUp(it) {
				return h, toast(fmt.Sprintf("You're caught up on %s.", it.media.DisplayTitle()), false)
			}
			return h, watch(session.Request{Media: it.media, Mode: domain.Mode(it.progress.Mode), Provider: it.progress.Provider})
		case key.Matches(msg, keyDetails):
			return h, push(newDetails(h.ctx, h.svc, it.media, domain.Mode(it.progress.Mode)))
		default:
			h.list.handleKey(msg, 5)
		}
	}
	return h, nil
}

func caughtUp(it homeItem) bool {
	aired := it.media.AiredEpisodes()
	return aired > 0 && nextEpisode(it.progress) > float64(aired)
}

func (h *homeScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString(styleSection.Render("Continue watching") + "\n\n")
	switch {
	case h.loading:
		b.WriteString(styleMuted.Render("Loading…"))
		return b.String()
	case h.err != nil:
		b.WriteString(styleBad.Render("Couldn't load history: " + h.err.Error()))
		return b.String()
	case len(h.items) == 0:
		b.WriteString("Nothing watched yet.\n\n")
		b.WriteString("Press " + styleKey.Render("/") + " to search for an anime.")
		return b.String()
	}

	// Two lines per show plus a gap.
	rows := max((height-3)/3, 1)
	start, end := h.list.window(rows)
	for i := start; i < end; i++ {
		it := h.items[i]
		marker, titleStyle := "  ", styleTitle
		if i == h.list.cursor {
			marker, titleStyle = styleSelected.Render("▌ "), styleSelected
		}
		b.WriteString(marker + titleStyle.Render(truncate(it.media.DisplayTitle(), width-4)) + "\n")
		b.WriteString("  " + styleMuted.Render(truncate(progressLine(it), width-4)) + "\n\n")
	}
	return b.String()
}

func progressLine(it homeItem) string {
	p := it.progress
	var state string
	switch {
	case caughtUp(it):
		state = fmt.Sprintf("Watched episode %s · caught up", episodeLabel(p.Episode))
	case p.Completed:
		state = fmt.Sprintf("Watched episode %s · next up: episode %s", episodeLabel(p.Episode), episodeLabel(nextEpisode(p)))
	default:
		state = fmt.Sprintf("Episode %s · %s / %s", episodeLabel(p.Episode), clock(p.Position), clock(p.Duration))
	}
	line := fmt.Sprintf("%s · %s", state, p.Mode)
	if p.Provider != "" {
		line += " · " + p.Provider
	}
	return line + " · " + ago(p.UpdatedAt)
}
