package tui

import (
	"context"
	"slices"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
)

const discoverPerPage = 25

type discoverPageMsg struct {
	gen  int
	page anilist.BrowsePage
	err  error
}

type discoverGenresMsg struct{ genres []string }

type discoverEntriesMsg struct{ entries map[int]string }

// discoverScreen browses AniList lists (this season, trending, ...) with
// genre and format filters, loading more as the cursor reaches the end.
type discoverScreen struct {
	ctx   context.Context
	svc   Services
	lists []anilist.DiscoverList
	tab   int

	gen      int // identifies the current list+filters; stale pages are dropped
	items    []anilist.Media
	nextPage int
	hasNext  bool
	loading  bool
	err      error
	list     list

	genres  []string       // options, loaded once
	genre   string         // "" for any
	format  string         // "" for any
	entries map[int]string // media ID → list status

	filtering bool
	filterRow int // 0 genre, 1 format
	editGenre string
	editFmt   string
}

var discoverFormats = []string{"", "TV", "MOVIE", "ONA", "OVA", "SPECIAL", "TV_SHORT"}

func newDiscover(ctx context.Context, svc Services) *discoverScreen {
	return &discoverScreen{ctx: ctx, svc: svc, lists: anilist.DiscoverLists(time.Now())}
}

func (d *discoverScreen) Title() string { return "Discover" }

func (d *discoverScreen) Init() tea.Cmd {
	ctx, svc := d.ctx, d.svc
	return tea.Batch(d.reload(), func() tea.Msg {
		genres, err := svc.Genres(ctx)
		if err != nil {
			return nil
		}
		return discoverGenresMsg{genres}
	}, d.loadEntries())
}

func (d *discoverScreen) Refresh() tea.Cmd { return d.loadEntries() }

func (d *discoverScreen) loadEntries() tea.Cmd {
	ctx, svc := d.ctx, d.svc
	return func() tea.Msg {
		entries, err := svc.ListEntries(ctx, "")
		if err != nil {
			return nil
		}
		m := map[int]string{}
		for _, e := range entries {
			m[e.MediaID] = e.Status
		}
		return discoverEntriesMsg{m}
	}
}

// reload starts the current list from its first page.
func (d *discoverScreen) reload() tea.Cmd {
	d.gen++
	d.items, d.list, d.nextPage, d.hasNext, d.err = nil, list{}, 1, false, nil
	return d.loadMore()
}

func (d *discoverScreen) loadMore() tea.Cmd {
	if d.loading && d.nextPage > 1 {
		return nil
	}
	d.loading = true
	q := d.lists[d.tab].Query
	q.Page, q.PerPage = d.nextPage, discoverPerPage
	if d.genre != "" {
		q.Genres = []string{d.genre}
	}
	if d.format != "" {
		q.Formats = []string{d.format}
	}
	ctx, svc, gen := d.ctx, d.svc, d.gen
	return func() tea.Msg {
		page, err := svc.Browse(ctx, q)
		return discoverPageMsg{gen, page, err}
	}
}

var (
	keyDiscoverTabs = key.NewBinding(key.WithKeys("tab", "right", "shift+tab", "left"), key.WithHelp("←/→", "lists"))
	keyFilter       = key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter"))
)

func (d *discoverScreen) Help() []key.Binding {
	if d.filtering {
		return []key.Binding{
			key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "genre/format")),
			key.NewBinding(key.WithKeys("left"), key.WithHelp("←/→", "change")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	}
	return []key.Binding{keyOpen, keyDiscoverTabs, keyFilter}
}

// CapturesInput keeps esc and arrows for the filter panel.
func (d *discoverScreen) CapturesInput() bool { return d.filtering }

func (d *discoverScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case discoverPageMsg:
		if msg.gen != d.gen {
			return d, nil
		}
		d.loading, d.err = false, msg.err
		if msg.err == nil {
			d.items = append(d.items, msg.page.Media...)
			d.hasNext = msg.page.HasNext
			d.nextPage++
			d.list.setLen(len(d.items))
		}
	case discoverGenresMsg:
		d.genres = msg.genres
	case discoverEntriesMsg:
		d.entries = msg.entries
	case tea.KeyPressMsg:
		if d.filtering {
			return d, d.updateFilter(msg)
		}
		switch {
		case key.Matches(msg, keyDiscoverTabs):
			step := 1
			if msg.String() == "left" || msg.String() == "shift+tab" {
				step = -1
			}
			d.tab = (d.tab + step + len(d.lists)) % len(d.lists)
			return d, d.reload()
		case key.Matches(msg, keyFilter):
			d.filtering, d.filterRow, d.editGenre, d.editFmt = true, 0, d.genre, d.format
			return d, nil
		case key.Matches(msg, keyOpen):
			if len(d.items) > 0 {
				return d, push(newDetails(d.ctx, d.svc, d.items[d.list.cursor], ""))
			}
			return d, nil
		}
		if d.list.handleKey(msg, 5) && d.list.cursor >= len(d.items)-5 && d.hasNext && !d.loading {
			return d, d.loadMore()
		}
	}
	return d, nil
}

func (d *discoverScreen) updateFilter(msg tea.KeyPressMsg) tea.Cmd {
	cycle := func(options []string, cur string, step int) string {
		i := slices.Index(options, cur)
		return options[(i+step+len(options))%len(options)]
	}
	switch msg.String() {
	case "esc":
		d.filtering = false
	case "enter":
		d.filtering = false
		if d.editGenre != d.genre || d.editFmt != d.format {
			d.genre, d.format = d.editGenre, d.editFmt
			return d.reload()
		}
	case "up", "down", "k", "j":
		d.filterRow = 1 - d.filterRow
	case "left", "right", "h", "l":
		step := 1
		if s := msg.String(); s == "left" || s == "h" {
			step = -1
		}
		if d.filterRow == 0 {
			d.editGenre = cycle(append([]string{""}, d.genres...), d.editGenre, step)
		} else {
			d.editFmt = cycle(discoverFormats, d.editFmt, step)
		}
	}
	return nil
}

func (d *discoverScreen) View(width, height int) string {
	var b strings.Builder
	var tabs []string
	for i, l := range d.lists {
		if i == d.tab {
			tabs = append(tabs, styleSelected.Render(l.Title))
		} else {
			tabs = append(tabs, styleMuted.Render(l.Title))
		}
	}
	b.WriteString("\n" + truncate(strings.Join(tabs, styleMuted.Render("  ·  ")), width) + "\n")
	b.WriteString(d.filterLine(width) + "\n\n")

	switch {
	case d.err != nil && len(d.items) == 0:
		b.WriteString(styleBad.Render("Couldn't load: " + firstLine(d.err.Error())))
		return b.String()
	case d.loading && len(d.items) == 0:
		b.WriteString(styleMuted.Render("Loading from AniList…"))
		return b.String()
	case len(d.items) == 0:
		b.WriteString(styleMuted.Render("Nothing matches these filters."))
		return b.String()
	}

	rows := max((height-5)/2, 1)
	start, end := d.list.window(rows)
	for i := start; i < end; i++ {
		m := d.items[i]
		marker, titleStyle := "  ", styleTitle
		if i == d.list.cursor {
			marker, titleStyle = styleSelected.Render("▌ "), styleSelected
		}
		b.WriteString(marker + titleStyle.Render(truncate(m.DisplayTitle(), width-4)) + "\n")
		meta := mediaMeta(m)
		tag := ""
		if status, ok := d.entries[m.ID]; ok {
			tag = "  " + styleGood.Render("· "+statusTitle(status))
		}
		b.WriteString("  " + styleMuted.Render(truncate(meta, width-4-len(tag))) + tag + "\n")
	}
	if end == len(d.items) && d.loading {
		b.WriteString(styleMuted.Render("  Loading more…"))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (d *discoverScreen) filterLine(width int) string {
	genre, format := d.genre, d.format
	if d.filtering {
		genre, format = d.editGenre, d.editFmt
	}
	show := func(v, any string) string {
		if v == "" {
			return any
		}
		return strings.ReplaceAll(v, "_", " ")
	}
	g := "Genre: " + show(genre, "any")
	f := "Format: " + show(format, "any")
	if !d.filtering {
		return styleMuted.Render(truncate(g+" · "+f+" · f to filter", width))
	}
	if d.filterRow == 0 {
		g = styleSelected.Render("‹ " + g + " ›")
	} else {
		f = styleSelected.Render("‹ " + f + " ›")
	}
	return styleWarn.Render("Filter  ") + g + styleMuted.Render("  ·  ") + f
}

// statusTitle names a list status the way the home tabs do.
func statusTitle(status string) string {
	for _, t := range homeTabs {
		if t.status == status && status != "" {
			return t.title
		}
	}
	return status
}

var _ refresher = (*discoverScreen)(nil)
