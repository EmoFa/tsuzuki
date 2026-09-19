package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/session"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
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

	rounds        int
	beforeThrough int
	watchedBefore map[float64]bool
	marked        []string
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

func (f *fakeServices) RecentShows(_ context.Context, limit int) ([]store.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.progress) == 0 {
		return nil, nil
	}
	// The most recent episode of each show, newest first, like the store.
	seen, out := map[int]bool{}, []store.Progress{}
	for i := len(f.progress) - 1; i >= 0 && len(out) < limit; i-- {
		if p := f.progress[i]; !seen[p.MediaID] {
			seen[p.MediaID] = true
			out = append(out, p)
		}
	}
	return out, nil
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

func (f *fakeServices) ListEntries(_ context.Context, status string) ([]store.ListEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if status == "" {
		return f.entries, nil
	}
	var out []store.ListEntry
	for _, e := range f.entries {
		if e.Status == status {
			out = append(out, e)
		}
	}
	return out, nil
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

func (f *fakeServices) SetWatched(_ context.Context, media anilist.Media, from, to float64, watched bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for ep := from; ep <= to; ep++ {
		if !watched {
			f.progress = slices.DeleteFunc(f.progress, func(p store.Progress) bool { return p.Episode == ep })
			continue
		}
		f.progress = append(f.progress, store.Progress{MediaID: media.ID, Episode: ep, Completed: true, UpdatedAt: time.Now()})
	}
	f.marked = append(f.marked, fmt.Sprintf("%v-%v=%v", from, to, watched))
	// Like the real thing: the list follows the marks as far as an unbroken
	// run from the first episode reaches.
	done := map[int]bool{}
	for _, p := range f.progress {
		if p.MediaID == media.ID && p.Completed {
			done[int(p.Episode)] = true
		}
	}
	var entry *store.ListEntry
	for i := range f.entries {
		if f.entries[i].MediaID == media.ID {
			entry = &f.entries[i]
		}
	}
	round := 1
	if f.rounds > 0 {
		round = f.rounds + 1
	}
	was := tracker.ListThrough(entry, media.Episodes, round)
	want := tracker.WatchedRun(done, was, media.Episodes)
	if !watched {
		want = min(want, int(from)-1)
	}
	if want == was {
		if watched {
			return "Marked as watched. Your list is unchanged.", nil
		}
		return "Unmarked.", nil
	}
	if entry == nil {
		f.entries = append(f.entries, store.ListEntry{MediaID: media.ID, UpdatedAt: time.Now()})
		entry = &f.entries[len(f.entries)-1]
	}
	entry.Status, entry.Progress = tracker.Current, want
	if media.Episodes > 0 && want >= media.Episodes {
		entry.Status = tracker.Completed
	}
	if !watched {
		return fmt.Sprintf("Unmarked. Your list now says %d episodes watched.", want), nil
	}
	return "Marked as watched.", nil
}

func (f *fakeServices) StartRewatch(_ context.Context, id int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rounds++
	f.watchedBefore = map[float64]bool{1: true, 2: true}
	// However far the show had been watched, from history or the list.
	for _, e := range f.entries {
		if e.MediaID == id && e.Status == "COMPLETED" {
			f.beforeThrough = frieren2.Episodes
		}
	}
	f.progress = nil
	return fmt.Sprintf("Rewatch started (round %d).", f.rounds+1), nil
}

func (f *fakeServices) WatchedBefore(_ context.Context, id int) (map[float64]bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.watchedBefore, nil
}

func (f *fakeServices) Round(_ context.Context, id int) (round, watchedBefore int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rounds + 1, f.beforeThrough, nil
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

var frieren1 = anilist.Media{ID: 154587, Episodes: 28, Title: anilist.Title{English: "Frieren: Beyond Journey’s End"}, Format: "TV", Status: "FINISHED"}

func (f *fakeServices) Sequels(_ context.Context, id int) ([]anilist.Media, error) {
	if id == frieren2.ID {
		return []anilist.Media{frieren3}, nil
	}
	return nil, nil
}

func (f *fakeServices) Prequels(_ context.Context, id int) ([]anilist.Media, error) {
	if id == frieren2.ID {
		return []anilist.Media{frieren1}, nil
	}
	return nil, nil
}

func (f *fakeServices) CheckUpdate(context.Context, bool) (Update, error) {
	return Update{Current: "0.1.0", Latest: "0.1.0"}, nil
}

func (f *fakeServices) Settings() Settings {
	return Settings{Config: config.Default(), ConfigPath: "/cfg/config.toml", DataDir: "/data", CacheDir: "/cache"}
}

// ansiRE matches the colour codes lipgloss writes, so assertions can look at
// the text a user sees.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

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
	for _, msg := range runBatch(d.loadPrequels()) {
		d.Update(msg)
	}
	view := d.View(100, 30)
	for _, want := range []string{"filler", "✓", "▶", "next", "c continues with episode 3", "Following the exam,", "trio heads north & beyond.", "Sequel: Frieren: Beyond Journey’s End Season 3 · r to open",
		"Prequel: Frieren: Beyond Journey’s End · p to open"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q:\n%s", want, view)
		}
	}
}

func TestDetailsOpensPrequelAndSequel(t *testing.T) {
	svc := &fakeServices{}
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	for _, msg := range append(runBatch(d.loadSequels()), runBatch(d.loadPrequels())...) {
		d.Update(msg)
	}
	for _, tc := range []struct{ key, want string }{{"p", frieren1.DisplayTitle()}, {"r", frieren3.DisplayTitle()}} {
		_, cmd := d.Update(press(tc.key))
		if cmd == nil {
			t.Fatalf("%q did nothing", tc.key)
		}
		msg, ok := cmd().(pushMsg)
		if !ok {
			t.Fatalf("%q sent %T, want pushMsg", tc.key, cmd())
		}
		if got := msg.s.Title(); got != tc.want {
			t.Errorf("%q opened %q, want %q", tc.key, got, tc.want)
		}
	}
}

// A narrow window must still show every key of the screen it's on, and when
// even wrapping can't fit them, ? has to stay reachable.
func TestFooterKeysSurviveASmallWindow(t *testing.T) {
	svc := &fakeServices{}
	m := New(context.Background(), svc)
	m.Update(pushMsg{newDetails(context.Background(), svc, frieren2, domain.Sub)})

	m.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	view := plain(m.render())
	for _, want := range []string{"jump to episode", "mark up to here", "list status", "subtitles", "rewatch", "? help"} {
		if !strings.Contains(view, want) {
			t.Errorf("70x20 footer missing %q:\n%s", want, view)
		}
	}
	if h := lipgloss.Height(view); h != 20 {
		t.Errorf("rendered %d lines, want 20", h)
	}

	// Too narrow for everything: the bar leads with the keys that are always
	// there and says the list goes on.
	m.Update(tea.WindowSizeMsg{Width: 34, Height: 8})
	view = plain(m.render())
	if !strings.Contains(view, "? help") || !strings.Contains(view, "…") {
		t.Errorf("34x8 footer dropped the way out:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := lipgloss.Width(line); w > 34 {
			t.Errorf("line %d wide in a 34-wide window: %q", w, line)
		}
	}
}

// The ? overlay is the full list, so it has to fit the window it opens in.
func TestHelpOverlayFitsTheWindow(t *testing.T) {
	svc := &fakeServices{}
	m := New(context.Background(), svc)
	m.Update(pushMsg{newDetails(context.Background(), svc, frieren2, domain.Sub)})
	m.Update(tea.WindowSizeMsg{Width: 76, Height: 18})
	m.Update(press("?"))

	view := plain(m.render())
	if h := lipgloss.Height(view); h > 18 {
		t.Errorf("overlay rendered %d lines in an 18-line window:\n%s", h, view)
	}
	for _, want := range []string{"play episode", "jump to episode", "mark up to here", "rewatch", "quit tsuzuki", "press any key to close"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q:\n%s", want, view)
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

	// Enter on an unaired sequel explains instead of searching providers.
	started := func() *session.Request {
		_, cmd := m.Update(press("enter"))
		for _, msg := range runBatch(cmd) {
			switch msg := msg.(type) {
			case watchMsg:
				return &msg.req
			case toastMsg:
				toasts = append(toasts, msg.text)
			}
		}
		return nil
	}
	if r := started(); r != nil || !strings.Contains(toasts[len(toasts)-1], "hasn't aired yet") {
		t.Fatalf("unaired sequel: started %+v, toasts %q", r, toasts)
	}

	// Once it has aired, enter starts it.
	p.finish.sequels[0].Status = "RELEASING"
	if r := started(); r == nil || r.Media.ID != frieren3.ID || r.Mode != domain.Sub {
		t.Fatalf("enter didn't start the sequel: %+v", r)
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

func TestDetailsForUpcomingShow(t *testing.T) {
	svc := &fakeServices{}
	upcoming := anilist.Media{ID: 77, Title: anilist.Title{English: "Next Season"}, Status: "NOT_YET_RELEASED", StartDate: anilist.FuzzyDate{Year: 2027, Month: 1}}
	d := newDetails(context.Background(), svc, upcoming, domain.Sub)
	for _, msg := range runBatch(d.Init()) {
		if msg != nil {
			d.Update(msg)
		}
	}
	if d.loadingEpisodes || d.episodesErr != nil {
		t.Fatalf("looked up episodes for an unaired show: loading=%v err=%v", d.loadingEpisodes, d.episodesErr)
	}
	view := d.View(100, 30)
	if !strings.Contains(view, "Hasn't aired yet · Starts January 2027") || strings.Contains(view, "Couldn't list episodes") || strings.Contains(view, "so far") {
		t.Fatalf("view:\n%s", view)
	}
	for _, k := range []tea.KeyPressMsg{press("enter"), {Code: 'c', Text: "c"}, {Code: 'm', Text: "m"}} {
		_, cmd := d.Update(k)
		for _, msg := range runBatch(cmd) {
			if _, ok := msg.(watchMsg); ok {
				t.Fatalf("%v started a watch", k)
			}
		}
		if d.loadingEpisodes {
			t.Fatalf("%v looked up episodes", k)
		}
	}
	if len(svc.watches) != 0 {
		t.Fatal("watched an unaired show")
	}
}

func TestDetailsRewatch(t *testing.T) {
	svc := &fakeServices{}
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	var apply func(tea.Cmd)
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			switch msg.(type) {
			case nil, toastMsg:
			default:
				_, next := d.Update(msg)
				apply(next)
			}
		}
	}
	d.Update(detailsProgressMsg{progress: []store.Progress{
		{Episode: 1, Completed: true}, {Episode: 2, Completed: true},
	}})
	apply(d.loadRound())
	if view := d.View(100, 30); strings.Contains(view, "rewatch") {
		t.Fatalf("first watch shouldn't mention rewatching:\n%s", view)
	}

	// R asks first, and n doesn't start anything.
	_, cmd := d.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	apply(cmd)
	if !d.CapturesInput() || !strings.Contains(d.View(100, 30), "Rewatch from episode 1?") {
		t.Fatalf("no confirmation:\n%s", d.View(100, 30))
	}
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	apply(cmd)
	if svc.rounds != 0 {
		t.Fatal("n started a rewatch")
	}

	// y starts it: progress resets, earlier watches stay marked, header shows the round.
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	apply(cmd)
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	apply(cmd)
	if svc.rounds != 1 {
		t.Fatalf("rounds = %d", svc.rounds)
	}
	if d.round != 2 || !d.watchedBefore[1] {
		t.Fatalf("round = %d, watchedBefore = %v", d.round, d.watchedBefore)
	}
	view := d.View(100, 30)
	if !strings.Contains(view, "rewatch, round 2") {
		t.Errorf("view missing the round:\n%s", view)
	}
	if next := d.nextUp(); next != 1 {
		t.Errorf("continue would play episode %v, want 1", next)
	}
}

func TestDetailsMarkAndJump(t *testing.T) {
	svc := &fakeServices{}
	// A long show, so jumping matters: AniList says 500 episodes.
	long := anilist.Media{ID: 1735, Title: anilist.Title{English: "Naruto: Shippuden"}, Episodes: 500, Status: "FINISHED", Format: "TV"}
	d := newDetails(context.Background(), svc, long, domain.Sub)
	var apply func(tea.Cmd)
	toasts := []string{}
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			switch msg := msg.(type) {
			case nil:
			case toastMsg:
				toasts = append(toasts, msg.text)
			default:
				_, next := d.Update(msg)
				apply(next)
			}
		}
	}
	apply(d.loadProgress())
	if len(d.numbers) != 500 {
		t.Fatalf("episodes listed = %d", len(d.numbers))
	}

	// # jumps to a typed episode.
	_, cmd := d.Update(tea.KeyPressMsg{Code: '#', Text: "#"})
	apply(cmd)
	if !d.jumping || !d.CapturesInput() {
		t.Fatal("# didn't open the jump input")
	}
	d.jumpInput.SetValue("372")
	_, cmd = d.Update(press("enter"))
	apply(cmd)
	if d.jumping || d.numbers[d.list.cursor] != 372 {
		t.Fatalf("cursor on %v after jumping", d.numbers[d.list.cursor])
	}

	// w marks the selected episode watched, then unmarks it.
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	apply(cmd)
	if p, ok := d.progress[372]; !ok || !p.Completed {
		t.Fatalf("episode 372 not marked: %+v", d.progress[372])
	}
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	apply(cmd)
	if _, ok := d.progress[372]; ok {
		t.Fatal("second w didn't unmark")
	}
	if got := strings.Join(svc.marked, " "); got != "372-372=true 372-372=false" {
		t.Errorf("marks = %q", got)
	}
	// Episode 372 on its own says nothing about the 371 before it, so the list
	// stays where it is and only that episode is ticked.
	if e, _ := svc.ListEntry(context.Background(), long.ID); e != nil && e.Progress != 0 {
		t.Errorf("marking one episode moved the list to %d", e.Progress)
	}
	if !slices.ContainsFunc(toasts, func(s string) bool { return strings.Contains(s, "list is unchanged") }) {
		t.Errorf("marking past a gap should say the list didn't move: %q", toasts)
	}

	// A tick that comes from the list unmarks too, rather than being re-marked.
	svc.entries = []store.ListEntry{{MediaID: long.ID, Status: "CURRENT", Progress: 12, UpdatedAt: time.Now()}}
	apply(d.loadEntry())
	d.list.setCursor(11) // episode 12
	if !d.watched(12) {
		t.Fatal("episode 12 should read as watched from the list")
	}
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'w', Text: "w"})
	apply(cmd)
	if d.watched(12) {
		t.Error("w on a list-derived tick didn't unmark it")
	}
	if e, _ := svc.ListEntry(context.Background(), long.ID); e == nil || e.Progress != 11 {
		t.Errorf("unmarking episode 12 left the list at %+v", e)
	}

	// W marks everything up to the cursor.
	d.list.setCursor(4) // episode 5
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'W', Text: "W"})
	apply(cmd)
	if got := svc.marked[len(svc.marked)-1]; got != "1-5=true" {
		t.Fatalf("W marked %q", got)
	}
	for ep := 1.0; ep <= 5; ep++ {
		if p, ok := d.progress[ep]; !ok || !p.Completed {
			t.Fatalf("episode %v not marked by W", ep)
		}
	}
}

func TestDetailsMarksFromAniListProgress(t *testing.T) {
	// Completed on the list, never played here: every episode reads as watched,
	// without inventing watch history.
	svc := &fakeServices{entries: []store.ListEntry{{MediaID: frieren2.ID, Status: "COMPLETED", Progress: 0, UpdatedAt: time.Now()}}}
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	var apply func(tea.Cmd)
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			switch msg.(type) {
			case nil, toastMsg:
			default:
				_, next := d.Update(msg)
				apply(next)
			}
		}
	}
	apply(d.Init())

	if len(d.progress) != 0 {
		t.Fatalf("watch history was invented: %+v", d.progress)
	}
	if through := d.listWatchedThrough(); through != float64(frieren2.Episodes) {
		t.Fatalf("completed show reads as %v episodes watched", through)
	}
	if got := strings.Count(d.View(100, 60), "✓"); got < frieren2.Episodes {
		t.Errorf("view shows %d ticks, want %d", got, frieren2.Episodes)
	}

	// Part way through: the first 12 read as watched and continue follows.
	svc.entries = []store.ListEntry{{MediaID: frieren2.ID, Status: "CURRENT", Progress: 12, UpdatedAt: time.Now()}}
	apply(d.loadEntry())
	if through := d.listWatchedThrough(); through != 12 {
		t.Fatalf("watched through %v, want 12", through)
	}
	if next := d.nextUp(); next != 13 {
		t.Errorf("continue would play %v, want 13", next)
	}

	// Planning with no progress marks nothing.
	svc.entries = []store.ListEntry{{MediaID: frieren2.ID, Status: "PLANNING", UpdatedAt: time.Now()}}
	apply(d.loadEntry())
	if through := d.listWatchedThrough(); through != 0 {
		t.Errorf("planning marked %v episodes", through)
	}
}

// marksIn returns the mark shown for each episode: "done" (green), "dim"
// (watched before this rewatch), "part", "next" or "none".
func marksIn(d *detailsScreen, episodes int) []string {
	out := make([]string, 0, episodes)
	next := d.nextUp()
	for i := range episodes {
		row := d.episodeRow(i, next, 100)
		switch {
		case strings.Contains(row, styleGood.Render("✓")):
			out = append(out, "done")
		case strings.Contains(row, styleMuted.Render("✓")):
			out = append(out, "dim")
		case strings.Contains(row, styleWarn.Render("◐")):
			out = append(out, "part")
		case strings.Contains(row, styleInfo.Render("▶")):
			out = append(out, "next")
		default:
			out = append(out, "none")
		}
	}
	return out
}

func TestRewatchDimsTheEarlierWatch(t *testing.T) {
	// Completed on AniList, never played here, then rewatched: the first watch
	// should show faintly so the rewatch's own progress stands out.
	svc := &fakeServices{entries: []store.ListEntry{{MediaID: frieren2.ID, Status: "COMPLETED", UpdatedAt: time.Now()}}}
	d := newDetails(context.Background(), svc, frieren2, domain.Sub)
	var apply func(tea.Cmd)
	apply = func(cmd tea.Cmd) {
		for _, msg := range runBatch(cmd) {
			switch msg.(type) {
			case nil, toastMsg:
			default:
				_, next := d.Update(msg)
				apply(next)
			}
		}
	}
	apply(d.Init())
	for i, m := range marksIn(d, 3) {
		if m != "done" {
			t.Fatalf("before the rewatch, episode %d is %q, want done", i+1, m)
		}
	}

	// Start the rewatch, and report it as rewatching on the list.
	_, cmd := d.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	apply(cmd)
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	apply(cmd)
	svc.entries = []store.ListEntry{{MediaID: frieren2.ID, Status: "REPEATING", Progress: 0, UpdatedAt: time.Now()}}
	apply(d.loadEntry())

	marks := marksIn(d, frieren2.Episodes)
	if marks[0] != "next" {
		t.Errorf("episode 1 is %q, want next", marks[0])
	}
	for i, m := range marks[1:] {
		if m != "dim" {
			t.Fatalf("episode %d is %q, want dim (watched before the rewatch)", i+2, m)
		}
	}

	// Watching episode 1 in this round marks it normally, the rest stay dim.
	svc.progress = []store.Progress{{MediaID: frieren2.ID, Episode: 1, Completed: true, UpdatedAt: time.Now()}}
	apply(d.loadProgress())
	marks = marksIn(d, 3)
	if marks[0] != "done" || marks[1] != "next" || marks[2] != "dim" {
		t.Fatalf("during the rewatch: %v, want [done next dim]", marks[:3])
	}
}

func TestHomeHidesFinishedShows(t *testing.T) {
	svc := &fakeServices{}
	svc.progress = []store.Progress{
		{MediaID: frieren2.ID, Episode: 10, Completed: true, UpdatedAt: time.Now()},
		{MediaID: 555, Episode: 3, Completed: true, UpdatedAt: time.Now().Add(-time.Hour)},
	}
	svc.entries = []store.ListEntry{{MediaID: frieren2.ID, Status: "COMPLETED", Progress: 10, UpdatedAt: time.Now()}}

	h := newHome(context.Background(), svc)
	for _, msg := range runBatch(h.load()) {
		h.Update(msg)
	}
	if len(h.items) != 1 || h.items[0].media.ID != 555 {
		t.Fatalf("continue watching = %+v, want only the unfinished show", h.items)
	}

	// With everything finished, the tab says so instead of "nothing watched yet".
	svc.entries = []store.ListEntry{
		{MediaID: frieren2.ID, Status: "COMPLETED", UpdatedAt: time.Now()},
		{MediaID: 555, Status: "COMPLETED", UpdatedAt: time.Now()},
	}
	for _, msg := range runBatch(h.load()) {
		h.Update(msg)
	}
	if view := h.View(100, 20); !strings.Contains(view, "everything you've watched is finished") {
		t.Fatalf("empty view:\n%s", view)
	}

	// Rewatching it brings it back.
	svc.entries = []store.ListEntry{
		{MediaID: frieren2.ID, Status: "REPEATING", UpdatedAt: time.Now()},
		{MediaID: 555, Status: "CURRENT", UpdatedAt: time.Now()},
	}
	for _, msg := range runBatch(h.load()) {
		h.Update(msg)
	}
	if len(h.items) != 2 {
		t.Fatalf("after starting a rewatch = %+v, want both", h.items)
	}
}
