package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
)

var frieren2 = anilist.Media{
	ID: 182255, Episodes: 10, Status: "FINISHED", Format: "TV", Year: 2026, AverageScore: 88,
	Title:       anilist.Title{English: "Frieren: Beyond Journey’s End Season 2", Romaji: "Sousou no Frieren 2nd Season"},
	Description: "Following the exam,<br><br>the <i>trio</i> heads north &amp; beyond.",
	Genres:      []string{"Adventure", "Fantasy"},
}

type fakeServices struct {
	mu       sync.Mutex
	progress []store.Progress
	watches  []session.Request
	entries  []store.ListEntry
	prefs    map[int]store.ShowPrefs
	browsed  []anilist.BrowseQuery
	scores   map[int]float64
}

func (f *fakeServices) Search(context.Context, string) ([]anilist.Media, error) {
	return []anilist.Media{{ID: 1, Title: anilist.Title{English: "Frieren: Beyond Journey’s End"}, Episodes: 28}, frieren2}, nil
}

// Browse returns 30 numbered shows over two pages.
func (f *fakeServices) Browse(_ context.Context, q anilist.BrowseQuery) (anilist.BrowsePage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.browsed = append(f.browsed, q)
	var page anilist.BrowsePage
	first := (q.Page - 1) * q.PerPage
	for i := first; i < min(first+q.PerPage, 30); i++ {
		page.Media = append(page.Media, anilist.Media{ID: 1000 + i, Title: anilist.Title{English: fmt.Sprintf("Show %d", i)}, Format: "TV"})
	}
	page.HasNext = first+q.PerPage < 30
	return page, nil
}

func (f *fakeServices) Genres(context.Context) ([]string, error) {
	return []string{"Action", "Romance"}, nil
}

func (f *fakeServices) Media(_ context.Context, id int) (anilist.Media, error) {
	if id == frieren2.ID {
		return frieren2, nil
	}
	return anilist.Media{}, errors.New("not found")
}

func (f *fakeServices) RecentShows(context.Context, int) ([]store.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.progress) == 0 {
		return nil, nil
	}
	return f.progress[len(f.progress)-1:], nil
}

func (f *fakeServices) ShowProgress(context.Context, int) ([]store.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]store.Progress(nil), f.progress...), nil
}

func (f *fakeServices) ProviderEpisodes(context.Context, anilist.Media, domain.Mode) ([]domain.Episode, error) {
	return nil, errors.New("unused")
}

// Watch reports a stream starting, then plays until cancelled and records progress.
func (f *fakeServices) Watch(ctx context.Context, req session.Request, onStatus func(session.Status)) error {
	f.mu.Lock()
	f.watches = append(f.watches, req)
	f.mu.Unlock()
	st := session.Status{Media: req.Media, Episode: req.Episode, Provider: "fakeprov", Stream: domain.Stream{Label: "1080p"}}
	st.Kind = session.StatusResolving
	onStatus(st)
	st.Kind = session.StatusPlaying
	onStatus(st)
	st.Kind, st.Position, st.Duration = session.StatusProgress, 90*time.Second, 24*time.Minute
	onStatus(st)
	<-ctx.Done()
	f.mu.Lock()
	f.progress = append(f.progress, store.Progress{MediaID: req.Media.ID, Episode: req.Episode,
		Position: 90 * time.Second, Duration: 24 * time.Minute, Mode: string(req.Mode), UpdatedAt: time.Now()})
	f.mu.Unlock()
	return ctx.Err()
}

func (f *fakeServices) ListEntries(context.Context, string) ([]store.ListEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.entries, nil
}

func (f *fakeServices) ListEntry(_ context.Context, id int) (*store.ListEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.entries {
		if f.entries[i].MediaID == id {
			e := f.entries[i]
			return &e, nil
		}
	}
	return nil, nil
}

func (f *fakeServices) SetListStatus(_ context.Context, id int, status string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, store.ListEntry{MediaID: id, Status: status, UpdatedAt: time.Now()})
	return "list updated", nil
}

func (f *fakeServices) Account() Account { return Account{Backend: "anilist"} }

func (f *fakeServices) EpisodeKinds(context.Context, anilist.Media) (map[int]skip.EpisodeKind, error) {
	return map[int]skip.EpisodeKind{5: skip.Filler}, nil
}
func (f *fakeServices) Login(context.Context) (string, error) {
	return "", errors.New("unused")
}
func (f *fakeServices) Sync(context.Context) (string, error) { return "", nil }

func (f *fakeServices) ShowPrefs(_ context.Context, id int) (*store.ShowPrefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.prefs[id]; ok {
		return &p, nil
	}
	return nil, nil
}

func (f *fakeServices) SaveShowPrefs(_ context.Context, p store.ShowPrefs) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.prefs == nil {
		f.prefs = map[int]store.ShowPrefs{}
	}
	f.prefs[p.MediaID] = p
	return nil
}

func (f *fakeServices) DeleteShowPrefs(_ context.Context, id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.prefs, id)
	return nil
}

func (f *fakeServices) SetScore(_ context.Context, id int, score float64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scores == nil {
		f.scores = map[int]float64{}
	}
	f.scores[id] = score
	return fmt.Sprintf("Rated %g/10.", score), nil
}

var frieren3 = anilist.Media{ID: 999001, Title: anilist.Title{English: "Frieren: Beyond Journey’s End Season 3"}, Format: "TV", Status: "NOT_YET_RELEASED"}

func (f *fakeServices) Sequels(_ context.Context, id int) ([]anilist.Media, error) {
	if id == frieren2.ID {
		return []anilist.Media{frieren3}, nil
	}
	return nil, nil
}

func (f *fakeServices) CheckUpdate(context.Context, bool) (Update, error) {
	return Update{Current: "0.1.0", Latest: "0.1.0"}, nil
}

func (f *fakeServices) Settings() Settings {
	return Settings{Config: config.Default(), ConfigPath: "/cfg/config.toml", DataDir: "/data", CacheDir: "/cache"}
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	r := []rune(s)[0]
	return tea.KeyPressMsg{Code: r, Text: s}
}

func waitFor(t *testing.T, tm *teatest.TestModel, want string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool { return bytes.Contains(b, []byte(want)) },
		teatest.WithDuration(5*time.Second), teatest.WithCheckInterval(20*time.Millisecond))
}

// TestSearchDetailsPlayStop drives the real program loop through a full watch.
func TestSearchDetailsPlayStop(t *testing.T) {
	svc := &fakeServices{}
	m := New(context.Background(), svc)
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 30))
	m.send = tm.Send

	waitFor(t, tm, "Nothing watched yet")
	tm.Send(press("/"))
	tm.Type("frieren")
	waitFor(t, tm, "Season 2")

	tm.Send(press("down"))
	tm.Send(press("down"))
	tm.Send(press("enter"))
	waitFor(t, tm, "Episodes (sub)")

	tm.Send(press("enter"))
	waitFor(t, tm, "Playing from fakeprov")

	tm.Send(press("x"))
	waitFor(t, tm, "c continues with episode 1")

	tm.Send(press("ctrl+c"))
	final := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*Model)
	if _, ok := final.top().(*detailsScreen); !ok {
		t.Errorf("top screen = %T, want details", final.top())
	}
	if len(svc.watches) != 1 || svc.watches[0].Episode != 1 || svc.watches[0].Media.ID != frieren2.ID {
		t.Errorf("watches = %+v", svc.watches)
	}
}

func TestWatchLifecycle(t *testing.T) {
	svc := &fakeServices{}
	m := New(context.Background(), svc)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(watchMsg{session.Request{Media: frieren2, Episode: 3}})
	if _, ok := m.top().(*playingScreen); !ok || !m.watching || m.gen != 1 {
		t.Fatalf("after start: top=%T watching=%v gen=%d", m.top(), m.watching, m.gen)
	}
	firstCancel := m.cancel

	// A stale status from an older watch is ignored.
	m.Update(statusMsg{gen: 0, st: session.Status{Kind: session.StatusPlaying, Provider: "old"}})
	if m.top().(*playingScreen).provider != "" {
		t.Fatal("stale status applied")
	}
	m.Update(statusMsg{gen: 1, st: session.Status{Kind: session.StatusPlaying, Episode: 3, Provider: "anikoto"}})
	if m.top().(*playingScreen).provider != "anikoto" {
		t.Fatal("current status not applied")
	}

	// Switching episodes queues the new request until the old watch is done.
	m.Update(watchMsg{session.Request{Media: frieren2, Episode: 4}})
	if m.pending == nil || m.gen != 1 {
		t.Fatalf("pending=%v gen=%d", m.pending, m.gen)
	}
	if firstCancel == nil {
		t.Fatal("no cancel func")
	}
	m.Update(watchDoneMsg{gen: 1, err: context.Canceled})
	if m.gen != 2 || m.pending != nil || !m.watching {
		t.Fatalf("after switch: gen=%d pending=%v watching=%v", m.gen, m.pending, m.watching)
	}
	if p, ok := m.top().(*playingScreen); !ok || p.req.Episode != 4 || len(m.stack) != 2 {
		t.Fatalf("stack = %d, top %T", len(m.stack), m.top())
	}

	// The watch ending with an error returns to the previous screen with a toast.
	_, cmd := m.Update(watchDoneMsg{gen: 2, err: errors.New("episode 4 not available\nmore detail")})
	for _, msg := range runBatch(cmd) {
		m.Update(msg)
	}
	if _, ok := m.top().(*homeScreen); !ok || m.watching {
		t.Fatalf("after done: top=%T watching=%v", m.top(), m.watching)
	}
	if m.toast != "episode 4 not available" || !m.toastErr {
		t.Fatalf("toast = %q err=%v", m.toast, m.toastErr)
	}
}

// runBatch executes a command (expanding batches) and returns its messages.
// Only use with commands that return immediately.
func runBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, runBatch(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestQuitKeysRespectTextInput(t *testing.T) {
	m := New(context.Background(), &fakeServices{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(pushMsg{newSearch(context.Background(), m.svc)})
	s := m.top().(*searchScreen)
	s.input.Focus()

	_, cmd := m.Update(press("q"))
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("typing q in the search box quit tsuzuki")
		}
	}
	if s.input.Value() != "q" {
		t.Fatalf("input = %q", s.input.Value())
	}
}

func TestDetailsMarkers(t *testing.T) {
	svc := &fakeServices{}
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	now := time.Now()
	d.Update(detailsProgressMsg{progress: []store.Progress{
		{Episode: 1, Completed: true, UpdatedAt: now.Add(-2 * time.Hour)},
		{Episode: 2, Completed: true, UpdatedAt: now.Add(-time.Hour)},
	}})
	if next := d.nextUp(); next != 3 {
		t.Fatalf("nextUp = %v", next)
	}
	if d.numbers[d.list.cursor] != 3 {
		t.Fatalf("cursor on episode %v, want 3", d.numbers[d.list.cursor])
	}
	for _, msg := range runBatch(d.loadKinds()) {
		d.Update(msg)
	}
	for _, msg := range runBatch(d.loadSequels()) {
		d.Update(msg)
	}
	view := d.View(100, 30)
	for _, want := range []string{"filler", "✓", "▶", "next", "c continues with episode 3", "Following the exam,", "trio heads north & beyond.", "Sequel: Frieren: Beyond Journey’s End Season 3 · r to open"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestListWindowKeepsCursorVisible(t *testing.T) {
	var l list
	l.setLen(100)
	l.setCursor(50)
	if start, end := l.window(10); start > 50 || end <= 50 || end-start != 10 {
		t.Fatalf("window = %d..%d", start, end)
	}
	l.setCursor(99)
	if _, end := l.window(10); end != 100 {
		t.Fatalf("end = %d", end)
	}
	l.setCursor(0)
	if start, _ := l.window(10); start != 0 {
		t.Fatalf("start = %d", start)
	}
	l.setLen(3)
	if start, end := l.window(10); start != 0 || end != 3 {
		t.Fatalf("short list window = %d..%d", start, end)
	}
}

func TestTextHelpers(t *testing.T) {
	if got := plainDescription("A<br><br><br>B <b>bold</b> &amp; <i>it</i>"); got != "A\n\nB bold & it" {
		t.Errorf("plainDescription = %q", got)
	}
	if got := truncate("Frieren: Beyond Journey’s End", 10); got != "Frieren: …" {
		t.Errorf("truncate = %q", got)
	}
	if got := clampLines("a\nb\nc", 2); got != "a\nb …" {
		t.Errorf("clampLines = %q", got)
	}
	if got := nextEpisode(store.Progress{Episode: 12.5, Completed: true}); got != 13 {
		t.Errorf("nextEpisode = %v", got)
	}
}

func TestTryAnotherProviderSkipsCurrent(t *testing.T) {
	m := New(context.Background(), &fakeServices{})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.Update(watchMsg{session.Request{Media: frieren2, Episode: 3, Mode: domain.Sub, SkipProviders: []string{"senshi"}}})
	m.Update(statusMsg{gen: 1, st: session.Status{Kind: session.StatusPlaying, Episode: 3, Provider: "anikoto"}})

	_, cmd := m.Update(press("f"))
	msgs := runBatch(cmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %+v", msgs)
	}
	req := msgs[0].(watchMsg).req
	if req.Episode != 3 || req.Provider != "" || strings.Join(req.SkipProviders, ",") != "senshi,anikoto" {
		t.Fatalf("request = %+v", req)
	}
	// Feeding it back queues the switch behind the running watch.
	m.Update(msgs[0])
	if m.pending == nil || m.pending.SkipProviders[1] != "anikoto" {
		t.Fatalf("pending = %+v", m.pending)
	}

	// Moving to another episode forgets the skipped providers.
	_, cmd = m.Update(press("n"))
	if next := runBatch(cmd)[0].(watchMsg).req; next.Episode != 4 || next.SkipProviders != nil {
		t.Fatalf("next = %+v", next)
	}
}

func TestHomeListTabsAndDetailsStatusPicker(t *testing.T) {
	svc := &fakeServices{entries: []store.ListEntry{{MediaID: frieren2.ID, Status: "CURRENT", Progress: 4, UpdatedAt: time.Now()}}}
	m := New(context.Background(), svc)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	home := m.top().(*homeScreen)

	// Switch to the Watching tab and load it.
	_, cmd := m.Update(press("right"))
	for _, msg := range runBatch(cmd) {
		m.Update(msg)
	}
	if home.tab != 1 || len(home.items) != 1 {
		t.Fatalf("tab=%d items=%d", home.tab, len(home.items))
	}
	view := home.View(100, 20)
	if !strings.Contains(view, "Watching · 4/10 episodes") {
		t.Fatalf("home view:\n%s", view)
	}

	// Details shows the entry; the picker sets a new status.
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	for _, msg := range runBatch(d.loadEntry()) {
		d.Update(msg)
	}
	if !strings.Contains(d.View(100, 30), "On your list: Watching") {
		t.Fatalf("details view:\n%s", d.View(100, 30))
	}
	d.Update(press("l"))
	if !d.CapturesInput() || !strings.Contains(d.View(100, 30), "Set list status") {
		t.Fatal("status picker not shown")
	}
	_, cmd = d.Update(press("2"))
	msgs := runBatch(cmd)
	if len(msgs) != 1 || d.CapturesInput() {
		t.Fatalf("msgs=%+v picking=%v", msgs, d.pickingStatus)
	}
	if svc.entries[len(svc.entries)-1].Status != "PLANNING" {
		t.Fatalf("entries = %+v", svc.entries)
	}
}

func TestDetailsSubtitleEditor(t *testing.T) {
	svc := &fakeServices{}
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	var apply func(tea.Cmd)
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			if msg != nil {
				_, next := d.Update(msg)
				apply(next)
			}
		}
	}
	if view := d.View(100, 30); !strings.Contains(view, "Subtitles: en · s to change") {
		t.Fatalf("default line missing:\n%s", view)
	}

	_, cmd := d.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	apply(cmd)
	if !d.editingSubs || !d.CapturesInput() {
		t.Fatal("s didn't open the editor")
	}
	d.subsInput.SetValue("ES, en")
	d.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	_, cmd = d.Update(press("enter"))
	apply(cmd)

	p := svc.prefs[frieren2.ID]
	if strings.Join(p.SubLanguages, ",") != "es,en" || p.SubShow == nil || *p.SubShow {
		t.Fatalf("saved %+v", p)
	}
	if view := d.View(100, 30); !strings.Contains(view, "Subtitles: es, en · hidden (this show)") {
		t.Fatalf("override line missing:\n%s", view)
	}

	_, cmd = d.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	apply(cmd)
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	apply(cmd)
	if _, ok := svc.prefs[frieren2.ID]; ok {
		t.Fatal("ctrl+r didn't reset to defaults")
	}

	// Invalid codes aren't saved.
	_, cmd = d.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	apply(cmd)
	d.subsInput.SetValue("english")
	_, cmd = d.Update(press("enter"))
	apply(cmd)
	if _, ok := svc.prefs[frieren2.ID]; ok {
		t.Fatal("saved an invalid language")
	}
}

func TestDiscoverPagingFiltersAndTags(t *testing.T) {
	svc := &fakeServices{entries: []store.ListEntry{{MediaID: 1001, Status: "PLANNING"}}}
	d := newDiscover(context.Background(), svc)
	var apply func(tea.Cmd)
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			if msg != nil {
				_, next := d.Update(msg)
				apply(next)
			}
		}
	}
	apply(d.Init())
	if len(d.items) != 25 || !d.hasNext {
		t.Fatalf("first page: %d items, hasNext %v", len(d.items), d.hasNext)
	}
	view := d.View(100, 40)
	for _, want := range []string{"This season", "Trending", "Show 0", "Show 1", "Planning", "Genre: any · Format: any"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}

	// Moving near the end loads the next page.
	for range 21 {
		_, cmd := d.Update(press("down"))
		apply(cmd)
	}
	if len(d.items) != 30 || d.hasNext {
		t.Fatalf("after scrolling: %d items, hasNext %v", len(d.items), d.hasNext)
	}

	// Filter: genre Action, format MOVIE, then apply reloads from page 1.
	_, cmd := d.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	apply(cmd)
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	d.Update(press("down"))
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	_, cmd = d.Update(press("enter"))
	apply(cmd)
	last := svc.browsed[len(svc.browsed)-1]
	if last.Page != 1 || !slices.Equal(last.Genres, []string{"Action"}) || !slices.Equal(last.Formats, []string{"MOVIE"}) {
		t.Fatalf("filtered query = %+v", last)
	}

	// Switching lists keeps the filters.
	_, cmd = d.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	apply(cmd)
	last = svc.browsed[len(svc.browsed)-1]
	if last.Sort != "TRENDING_DESC" || !slices.Equal(last.Genres, []string{"Action"}) {
		t.Fatalf("trending query = %+v", last)
	}
}

func TestFinishingPanel(t *testing.T) {
	svc := &fakeServices{}
	ctx := context.Background()
	m := New(ctx, svc)
	m.width, m.height = 100, 30
	req := session.Request{Media: frieren2, Episode: 10, Mode: domain.Sub}
	m.showPlaying(req)
	m.watching, m.gen = true, 1
	m.Update(statusMsg{gen: 1, st: session.Status{Kind: session.StatusFinishedShow, Media: frieren2, Episode: 10}})

	var toasts []string
	var apply func(tea.Cmd)
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			switch msg := msg.(type) {
			case nil:
			case toastMsg: // don't run the expiry timer
				toasts = append(toasts, msg.text)
			default:
				_, next := m.Update(msg)
				apply(next)
			}
		}
	}
	// Playback ends: the screen stays for the finishing panel instead of closing.
	apply(m.watchDone(watchDoneMsg{gen: 1}))
	p, ok := m.top().(*playingScreen)
	if !ok || p.finish == nil || !p.finish.rating {
		t.Fatalf("top = %T, finish = %+v", m.top(), p)
	}
	if view := m.render(); !strings.Contains(view, "You finished Frieren") || !strings.Contains(view, "Rate it") {
		t.Fatalf("rating view:\n%s", view)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: '9', Text: "9"})
	apply(cmd)
	if svc.scores[frieren2.ID] != 9 {
		t.Fatalf("scores = %v", svc.scores)
	}
	view := m.render()
	for _, want := range []string{"Up next", "Season 3"} {
		if !strings.Contains(view, want) {
			t.Errorf("sequel view missing %q:\n%s", want, view)
		}
	}
	if !slices.Contains(toasts, "Rated 9/10.") {
		t.Errorf("toasts = %q", toasts)
	}

	// p adds the sequel to Planning rather than jumping to a previous episode.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	apply(cmd)
	if n := len(svc.entries); n == 0 || svc.entries[n-1].MediaID != frieren3.ID || svc.entries[n-1].Status != "PLANNING" {
		t.Fatalf("entries = %+v", svc.entries)
	}

	// Enter starts the sequel.
	_, cmd = m.Update(press("enter"))
	var started *session.Request
	for _, msg := range runBatch(cmd) {
		if w, ok := msg.(watchMsg); ok {
			started = &w.req
		}
	}
	if started == nil || started.Media.ID != frieren3.ID || started.Mode != domain.Sub {
		t.Fatalf("enter didn't start the sequel: %+v", started)
	}

	// esc closes the panel.
	_, cmd = m.Update(press("esc"))
	apply(cmd)
	if _, ok := m.top().(*playingScreen); ok {
		t.Fatal("esc didn't close the finishing panel")
	}
}

func TestFinishingSkipsRatingAndNoSequel(t *testing.T) {
	svc := &fakeServices{}
	other := anilist.Media{ID: 5, Title: anilist.Title{English: "Standalone"}, Episodes: 1, Status: "FINISHED"}
	p := newPlaying(context.Background(), svc, session.Request{Media: other, Episode: 1})
	for _, msg := range runBatch(p.startFinishing()) {
		p.Update(msg)
	}
	p.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if len(svc.scores) != 0 || p.finish.rating {
		t.Fatal("s should skip rating without scoring")
	}
	if view := p.View(80, 20); !strings.Contains(view, "No sequel") {
		t.Fatalf("view:\n%s", view)
	}
}
