package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
)

type detailsProgressMsg struct {
	progress []store.Progress
	err      error
}

type detailsEntryMsg struct {
	entry *store.ListEntry
}

type detailsKindsMsg struct {
	kinds map[int]skip.EpisodeKind
}

type detailsSequelsMsg struct {
	sequels []anilist.Media
}

type detailsPrefsMsg struct {
	prefs *store.ShowPrefs
}

type detailsPrefsSavedMsg struct {
	note string
	err  error
}

type detailsStatusMsg struct {
	note string
	err  error
}

type detailsEpisodesMsg struct {
	mode     domain.Mode
	episodes []domain.Episode
	err      error
}

type detailsScreen struct {
	ctx   context.Context
	svc   Services
	media anilist.Media
	mode  domain.Mode

	progress    map[float64]store.Progress
	progressSet bool
	// numbers lists the episodes to show; titles/filler come from the
	// provider when AniList doesn't know the episode count.
	numbers         []float64
	provEpisodes    map[float64]domain.Episode
	loadingEpisodes bool
	episodesErr     error
	cursorPlaced    bool
	list            list

	entry         *store.ListEntry
	pickingStatus bool
	kinds         map[int]skip.EpisodeKind

	sequels     []anilist.Media
	prefs       *store.ShowPrefs // nil: the show uses the config
	editingSubs bool
	subsInput   textinput.Model
	subsShow    bool
}

func newDetails(ctx context.Context, svc Services, media anilist.Media, mode domain.Mode) *detailsScreen {
	d := &detailsScreen{ctx: ctx, svc: svc, media: media, mode: modeOrDefault(mode, svc)}
	d.setNumbers()
	return d
}

func (d *detailsScreen) Title() string { return truncate(d.media.DisplayTitle(), 40) }

func (d *detailsScreen) Init() tea.Cmd {
	cmds := []tea.Cmd{d.loadProgress(), d.loadEntry(), d.loadKinds(), d.loadPrefs(), d.loadSequels()}
	if d.media.AiredEpisodes() == 0 {
		cmds = append(cmds, d.loadEpisodes())
	}
	return tea.Batch(cmds...)
}

func (d *detailsScreen) Refresh() tea.Cmd { return tea.Batch(d.loadProgress(), d.loadEntry()) }

func (d *detailsScreen) loadKinds() tea.Cmd {
	ctx, svc, media := d.ctx, d.svc, d.media
	return func() tea.Msg {
		kinds, err := svc.EpisodeKinds(ctx, media)
		if err != nil {
			return nil
		}
		return detailsKindsMsg{kinds}
	}
}

func (d *detailsScreen) loadSequels() tea.Cmd {
	ctx, svc, id := d.ctx, d.svc, d.media.ID
	return func() tea.Msg {
		s, err := svc.Sequels(ctx, id)
		if err != nil {
			return nil
		}
		return detailsSequelsMsg{s}
	}
}

func (d *detailsScreen) loadPrefs() tea.Cmd {
	ctx, svc, id := d.ctx, d.svc, d.media.ID
	return func() tea.Msg {
		p, err := svc.ShowPrefs(ctx, id)
		if err != nil {
			return nil
		}
		return detailsPrefsMsg{p}
	}
}

// subtitlePrefs is what playback will use for this show.
func (d *detailsScreen) subtitlePrefs() (langs []string, show, own bool) {
	c := d.svc.Settings().Config.Subtitles
	langs, show = c.Languages, c.Show
	if d.prefs != nil {
		if d.prefs.SubLanguages != nil {
			langs, own = d.prefs.SubLanguages, true
		}
		if d.prefs.SubShow != nil {
			show, own = *d.prefs.SubShow, true
		}
	}
	return langs, show, own
}

func (d *detailsScreen) startEditingSubs() tea.Cmd {
	langs, show, _ := d.subtitlePrefs()
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "en, es"
	in.CharLimit = 60
	in.SetValue(strings.Join(langs, ", "))
	d.subsInput, d.subsShow, d.editingSubs = in, show, true
	return d.subsInput.Focus()
}

// saveSubs stores the edited settings for this show, or with reset, removes
// them so the show follows the config again.
func (d *detailsScreen) saveSubs(reset bool) tea.Cmd {
	ctx, svc, id := d.ctx, d.svc, d.media.ID
	if reset {
		return func() tea.Msg {
			return detailsPrefsSavedMsg{"Subtitles: using your defaults", svc.DeleteShowPrefs(ctx, id)}
		}
	}
	langs := []string{}
	for _, l := range strings.Split(d.subsInput.Value(), ",") {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" {
			continue
		}
		if !config.IsLanguageCode(l) {
			return toast(fmt.Sprintf("%q isn't a language code like en or es", l), true)
		}
		langs = append(langs, l)
	}
	show := d.subsShow
	p := store.ShowPrefs{MediaID: id, SubLanguages: langs, SubShow: &show}
	return func() tea.Msg {
		return detailsPrefsSavedMsg{"Subtitles saved for this show", svc.SaveShowPrefs(ctx, p)}
	}
}

func (d *detailsScreen) loadEntry() tea.Cmd {
	ctx, svc, id := d.ctx, d.svc, d.media.ID
	return func() tea.Msg {
		e, err := svc.ListEntry(ctx, id)
		if err != nil {
			return detailsEntryMsg{}
		}
		return detailsEntryMsg{e}
	}
}

func (d *detailsScreen) loadProgress() tea.Cmd {
	ctx, svc, id := d.ctx, d.svc, d.media.ID
	return func() tea.Msg {
		p, err := svc.ShowProgress(ctx, id)
		return detailsProgressMsg{p, err}
	}
}

func (d *detailsScreen) loadEpisodes() tea.Cmd {
	d.loadingEpisodes = true
	ctx, svc, media, mode := d.ctx, d.svc, d.media, d.mode
	return func() tea.Msg {
		eps, err := svc.ProviderEpisodes(ctx, media, mode)
		return detailsEpisodesMsg{mode, eps, err}
	}
}

var (
	keyPlay     = key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "play episode"))
	keyContinue = key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "continue"))
	keyMode     = key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "sub/dub"))
	keyStatus   = key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "list status"))
	keySubs     = key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "subtitles"))
	keySequel   = key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "sequel"))
)

func (d *detailsScreen) Help() []key.Binding {
	if d.editingSubs {
		return []key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
			key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "show/hide")),
			key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "use defaults")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
		}
	}
	if d.pickingStatus {
		return []key.Binding{key.NewBinding(key.WithKeys("1"), key.WithHelp("1-6", "choose status")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel"))}
	}
	keys := []key.Binding{keyPlay, keyContinue, keyMode, keyStatus, keySubs}
	if len(d.sequels) > 0 {
		keys = append(keys, keySequel)
	}
	return keys
}

// CapturesInput keeps esc for cancelling the status picker and typed text for
// the subtitle editor.
func (d *detailsScreen) CapturesInput() bool { return d.pickingStatus || d.editingSubs }

func (d *detailsScreen) Update(msg tea.Msg) (screen, tea.Cmd) {
	switch msg := msg.(type) {
	case detailsProgressMsg:
		if msg.err != nil {
			return d, toast("Couldn't load progress: "+msg.err.Error(), true)
		}
		d.progress = map[float64]store.Progress{}
		for _, p := range msg.progress {
			d.progress[p.Episode] = p
		}
		d.progressSet = true
		d.setNumbers()
		if !d.cursorPlaced {
			d.cursorPlaced = true
			if i := slices.Index(d.numbers, d.nextUp()); i >= 0 {
				d.list.setCursor(i)
			}
		}

	case detailsEntryMsg:
		d.entry = msg.entry

	case detailsKindsMsg:
		d.kinds = msg.kinds

	case detailsPrefsMsg:
		d.prefs = msg.prefs

	case detailsSequelsMsg:
		d.sequels = msg.sequels

	case detailsPrefsSavedMsg:
		if msg.err != nil {
			return d, toast("Couldn't save subtitles: "+firstLine(msg.err.Error()), true)
		}
		return d, tea.Batch(toast(msg.note, false), d.loadPrefs())

	case detailsStatusMsg:
		if msg.err != nil {
			return d, toast("Couldn't change list status: "+firstLine(msg.err.Error()), true)
		}
		return d, tea.Batch(toast(msg.note, false), d.loadEntry())

	case detailsEpisodesMsg:
		if msg.mode != d.mode {
			return d, nil
		}
		d.loadingEpisodes, d.episodesErr = false, msg.err
		d.provEpisodes = map[float64]domain.Episode{}
		for _, e := range msg.episodes {
			d.provEpisodes[e.Number] = e
		}
		d.setNumbers()

	case tea.KeyPressMsg:
		if d.editingSubs {
			switch msg.String() {
			case "esc":
				d.editingSubs = false
				return d, nil
			case "enter":
				d.editingSubs = false
				return d, d.saveSubs(false)
			case "ctrl+r":
				d.editingSubs = false
				return d, d.saveSubs(true)
			case "tab":
				d.subsShow = !d.subsShow
				return d, nil
			}
			var cmd tea.Cmd
			d.subsInput, cmd = d.subsInput.Update(msg)
			return d, cmd
		}
		if d.pickingStatus {
			d.pickingStatus = false
			if i := strings.IndexAny("123456", msg.String()); len(msg.String()) == 1 && i >= 0 {
				return d, d.setStatus(homeTabs[i+1].status)
			}
			return d, nil
		}
		switch {
		case key.Matches(msg, keyStatus):
			d.pickingStatus = true
			return d, nil
		case key.Matches(msg, keySubs):
			return d, d.startEditingSubs()
		case key.Matches(msg, keySequel) && len(d.sequels) > 0:
			return d, push(newDetails(d.ctx, d.svc, d.sequels[0], d.mode))
		case key.Matches(msg, keyPlay):
			if len(d.numbers) == 0 {
				return d, nil
			}
			return d, watch(session.Request{Media: d.media, Episode: d.numbers[d.list.cursor], Mode: d.mode})
		case key.Matches(msg, keyContinue):
			return d, watch(session.Request{Media: d.media, Episode: d.nextUp(), Mode: d.mode})
		case key.Matches(msg, keyMode):
			if d.mode == domain.Sub {
				d.mode = domain.Dub
			} else {
				d.mode = domain.Sub
			}
			if d.media.AiredEpisodes() == 0 {
				return d, d.loadEpisodes()
			}
			return d, toast("Mode: "+string(d.mode), false)
		default:
			d.list.handleKey(msg, 10)
		}
	}
	return d, nil
}

func (d *detailsScreen) setStatus(status string) tea.Cmd {
	ctx, svc, id := d.ctx, d.svc, d.media.ID
	return func() tea.Msg {
		note, err := svc.SetListStatus(ctx, id, status)
		return detailsStatusMsg{note, err}
	}
}

// setNumbers rebuilds the episode list from AniList's aired count, the
// provider's list, and any watched episodes beyond both.
func (d *detailsScreen) setNumbers() {
	seen := map[float64]bool{}
	var nums []float64
	add := func(n float64) {
		if !seen[n] {
			seen[n] = true
			nums = append(nums, n)
		}
	}
	for i := 1; i <= d.media.AiredEpisodes(); i++ {
		add(float64(i))
	}
	for n := range d.provEpisodes {
		add(n)
	}
	for n := range d.progress {
		add(n)
	}
	slices.Sort(nums)
	d.numbers = nums
	d.list.setLen(len(nums))
}

// nextUp is the episode "continue" plays: after the most recently watched one.
func (d *detailsScreen) nextUp() float64 {
	var latest *store.Progress
	for _, p := range d.progress {
		if latest == nil || p.UpdatedAt.After(latest.UpdatedAt) {
			latest = &p
		}
	}
	if latest == nil {
		if len(d.numbers) > 0 {
			return d.numbers[0]
		}
		return 1
	}
	return nextEpisode(*latest)
}

func (d *detailsScreen) View(width, height int) string {
	var b strings.Builder
	m := d.media
	b.WriteString("\n" + styleTitle.Render(truncate(m.DisplayTitle(), width)) + "\n")
	if m.Title.Romaji != "" && m.Title.Romaji != m.DisplayTitle() {
		b.WriteString(styleMuted.Render(truncate(m.Title.Romaji, width)) + "\n")
	}
	b.WriteString(styleInfo.Render(truncate(mediaMeta(m), width)) + "\n")
	if len(m.Genres) > 0 {
		b.WriteString(styleMuted.Render(truncate(strings.Join(m.Genres, ", "), width)) + "\n")
	}
	switch {
	case d.pickingStatus:
		var opts []string
		for i, t := range homeTabs[1:] {
			opts = append(opts, styleKey.Render(fmt.Sprint(i+1))+" "+t.title)
		}
		b.WriteString(styleWarn.Render("Set list status: ") + truncate(strings.Join(opts, "  "), width-17) + "\n")
	case d.entry != nil:
		b.WriteString(styleGood.Render(truncate("On your list: "+entryLine(m, *d.entry), width)) + "\n")
	default:
		b.WriteString(styleMuted.Render("Not on your list · l to add") + "\n")
	}
	b.WriteString(d.subtitlesLine(width) + "\n")
	if len(d.sequels) > 0 {
		var names []string
		for _, s := range d.sequels {
			names = append(names, s.DisplayTitle())
		}
		b.WriteString(styleInfo.Render(truncate("Sequel: "+strings.Join(names, ", ")+" · r to open", width)) + "\n")
	}
	if desc := plainDescription(m.Description); desc != "" {
		// Leave room for clampLines' ellipsis so it never wraps.
		wrapped := lipgloss.NewStyle().Width(min(width, 100) - 2).Render(desc)
		b.WriteString("\n" + clampLines(wrapped, 4) + "\n")
	}

	next := d.nextUp()
	header := fmt.Sprintf("Episodes (%s)", d.mode)
	if d.progressSet && len(d.progress) > 0 {
		header += styleMuted.Render(fmt.Sprintf("  ·  c continues with episode %s", episodeLabel(next)))
	}
	b.WriteString(styleSection.Render(header) + "\n")

	switch {
	case d.loadingEpisodes && len(d.numbers) == 0:
		b.WriteString(styleMuted.Render("Looking up episodes…"))
		return b.String()
	case d.episodesErr != nil && len(d.numbers) == 0:
		b.WriteString(styleBad.Render("Couldn't list episodes: " + firstLine(d.episodesErr.Error())))
		return b.String()
	case len(d.numbers) == 0:
		b.WriteString(styleMuted.Render("No episodes have aired yet."))
		return b.String()
	}

	used := lipgloss.Height(b.String())
	start, end := d.list.window(max(height-used, 1))
	for i := start; i < end; i++ {
		b.WriteString(d.episodeRow(i, next, width) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (d *detailsScreen) episodeRow(i int, next float64, width int) string {
	n := d.numbers[i]
	name := "Episode " + episodeLabel(n)
	if e, ok := d.provEpisodes[n]; ok && e.Title != "" {
		name += " · " + e.Title
	}

	var mark, detail string
	markStyle := styleMuted
	if p, ok := d.progress[n]; ok && p.Completed {
		mark, markStyle = "✓", styleGood
	} else if ok && p.Duration > 0 {
		mark, markStyle = "◐", styleWarn
		detail = fmt.Sprintf("%s / %s", clock(p.Position), clock(p.Duration))
	} else if n == next && d.progressSet && len(d.progress) > 0 {
		mark, markStyle = "▶", styleInfo
		detail = "next"
	} else {
		mark = " "
	}
	e, fromProvider := d.provEpisodes[n]
	switch kind := d.kinds[int(n)]; {
	case kind == skip.Filler || (fromProvider && e.Filler):
		detail = strings.TrimSpace(detail + " " + styleWarn.Render("filler"))
	case kind == skip.Mixed:
		detail = strings.TrimSpace(detail + " mixed canon/filler")
	case fromProvider && e.Recap:
		detail = strings.TrimSpace(detail + " " + styleWarn.Render("recap"))
	}

	cursor, nameStyle := "  ", lipgloss.NewStyle()
	if i == d.list.cursor {
		cursor, nameStyle = styleSelected.Render("▌ "), styleSelected
	}
	row := cursor + markStyle.Render(mark) + " " + nameStyle.Render(truncate(name, width-20))
	if detail != "" {
		row += "  " + styleMuted.Render(detail)
	}
	return row
}

func (d *detailsScreen) subtitlesLine(width int) string {
	if d.editingSubs {
		state := "shown"
		if !d.subsShow {
			state = "hidden"
		}
		d.subsInput.SetWidth(max(min(width-40, 30), 8))
		return styleWarn.Render("Subtitles for this show: ") + d.subsInput.View() + styleMuted.Render("  · "+state)
	}
	langs, show, own := d.subtitlePrefs()
	text := "Subtitles: " + valueOr(strings.Join(langs, ", "), "any language")
	if !show {
		text += " · hidden"
	}
	if own {
		text += " (this show)"
	}
	return styleMuted.Render(truncate(text+" · s to change", width))
}
