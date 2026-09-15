package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

const recentLimit = 30

// homeTab is "continue watching" (status "") or one list status.
type homeTab struct {
	title  string
	status string
}

var homeTabs = []homeTab{
	{"Continue watching", ""},
	{"Watching", tracker.Current},
	{"Planning", tracker.Planning},
	{"Completed", tracker.Completed},
	{"Paused", tracker.Paused},
	{"Dropped", tracker.Dropped},
	{"Rewatching", tracker.Repeating},
}

type homeItem struct {
	media    anilist.Media
	progress *store.Progress  // continue-watching tab
	entry    *store.ListEntry // list tabs
}

type homeLoadedMsg struct {
	tab   int
	items []homeItem
	err   error
}

type homeScreen struct {
	ctx     context.Context
	svc     Services
	tab     int
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
	h.loading = true
	ctx, svc, tab := h.ctx, h.svc, h.tab
	return func() tea.Msg {
		var items []homeItem
		media := func(id int) anilist.Media {
			m, err := svc.Media(ctx, id)
			if err != nil {
				m = anilist.Media{ID: id, Title: anilist.Title{Romaji: fmt.Sprintf("AniList #%d", id)}}
			}
			return m
		}
		if status := homeTabs[tab].status; status == "" {
			recent, err := svc.RecentShows(ctx, recentLimit)
			if err != nil {
				return homeLoadedMsg{tab: tab, err: err}
			}
			for i := range recent {
				items = append(items, homeItem{media: media(recent[i].MediaID), progress: &recent[i]})
			}
		} else {
			entries, err := svc.ListEntries(ctx, status)
			if err != nil {
				return homeLoadedMsg{tab: tab, err: err}
			}
			for i := range entries {
				items = append(items, homeItem{media: media(entries[i].MediaID), entry: &entries[i]})
			}
		}
		return homeLoadedMsg{tab: tab, items: items}
	}
}

var (
	keyResume   = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "continue"))
	keyDetails  = key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "details"))
	keySearch   = key.NewBinding(key.WithKeys("/", "s"), key.WithHelp("/", "search"))
	keySettings = key.NewBinding(key.WithKeys(","), key.WithHelp(",", "settings"))
	keyTabs     = key.NewBinding(key.WithKeys("tab", "right", "shift+tab", "left"), key.WithHelp("←/→", "list tabs"))
)

func (h *homeScreen) Help() []key.Binding {
	if len(h.items) == 0 {
		return []key.Binding{keySearch, keyTabs, keySettings}
	}
	return []key.Binding{keyResume, keyDetails, keySearch, keyTabs, keySettings}
}

func (h *homeScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case homeLoadedMsg:
		if msg.tab != h.tab {
			return h, nil
		}
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
			return h, push(newSettings(h.ctx, h.svc))
		case key.Matches(msg, keyTabs):
			step := 1
			if msg.String() == "left" || msg.String() == "shift+tab" {
				step = -1
			}
			h.tab = (h.tab + step + len(homeTabs)) % len(homeTabs)
			h.items = nil
			h.list = list{}
			return h, h.load()
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
			req := session.Request{Media: it.media, Mode: modeOrDefault(it.mode(), h.svc)}
			if it.progress != nil {
				req.Provider = it.progress.Provider
			}
			return h, watch(req)
		case key.Matches(msg, keyDetails):
			return h, push(newDetails(h.ctx, h.svc, it.media, it.mode()))
		default:
			h.list.handleKey(msg, 5)
		}
	}
	return h, nil
}

func (it homeItem) mode() domain.Mode {
	if it.progress != nil {
		return domain.Mode(it.progress.Mode)
	}
	return ""
}

// caughtUp reports that nothing after the watched episodes has aired.
func caughtUp(it homeItem) bool {
	aired := float64(it.media.AiredEpisodes())
	switch {
	case aired == 0:
		return false
	case it.progress != nil:
		return nextEpisode(*it.progress) > aired
	case it.entry != nil:
		return float64(it.entry.Progress) >= aired
	}
	return false
}

func (h *homeScreen) View(width, height int) string {
	var b strings.Builder
	b.WriteString("\n" + h.tabBar(width) + "\n\n")
	switch {
	case h.loading && len(h.items) == 0:
		b.WriteString(styleMuted.Render("Loading…"))
		return b.String()
	case h.err != nil:
		b.WriteString(styleBad.Render("Couldn't load: " + h.err.Error()))
		return b.String()
	case len(h.items) == 0 && h.tab == 0:
		b.WriteString("Nothing watched yet.\n\n")
		b.WriteString("Press " + styleKey.Render("/") + " to search for an anime.")
		return b.String()
	case len(h.items) == 0:
		b.WriteString(styleMuted.Render("Nothing here yet."))
		if !h.svc.Account().LoggedIn {
			b.WriteString("\n\n" + styleMuted.Render("Log in to AniList from settings (,) to bring in your list."))
		}
		return b.String()
	}

	rows := max((height-4)/3, 1)
	start, end := h.list.window(rows)
	for i := start; i < end; i++ {
		it := h.items[i]
		marker, titleStyle := "  ", styleTitle
		if i == h.list.cursor {
			marker, titleStyle = styleSelected.Render("▌ "), styleSelected
		}
		b.WriteString(marker + titleStyle.Render(truncate(it.media.DisplayTitle(), width-4)) + "\n")
		b.WriteString("  " + styleMuted.Render(truncate(itemLine(it), width-4)) + "\n\n")
	}
	return b.String()
}

func (h *homeScreen) tabBar(width int) string {
	var parts []string
	for i, t := range homeTabs {
		if i == h.tab {
			parts = append(parts, styleSelected.Render(t.title))
		} else {
			parts = append(parts, styleMuted.Render(t.title))
		}
	}
	return truncate(strings.Join(parts, styleMuted.Render("  ·  ")), width)
}

func itemLine(it homeItem) string {
	if it.entry != nil {
		return entryLine(it.media, *it.entry)
	}
	p := *it.progress
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

func entryLine(m anilist.Media, e store.ListEntry) string {
	total := "?"
	if m.Episodes > 0 {
		total = fmt.Sprint(m.Episodes)
	}
	line := fmt.Sprintf("%s · %d/%s episodes", statusName(e.Status), e.Progress, total)
	if e.Score > 0 {
		line += fmt.Sprintf(" · score %.1f", e.Score)
	}
	return line + " · " + ago(e.UpdatedAt)
}

func statusName(s string) string {
	for _, t := range homeTabs {
		if t.status == s && s != "" {
			return t.title
		}
	}
	return strings.ToLower(s)
}
