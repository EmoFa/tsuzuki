package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

var frieren = anilist.Media{
	ID: 154587, Episodes: 28, Status: "FINISHED", Format: "TV", Year: 2023,
	Title: anilist.Title{English: "Frieren: Beyond Journey's End", Romaji: "Sousou no Frieren"},
}

// testApp builds an app backed by a temporary database, tracking locally and
// answering AniList from a fake.
func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	media := map[int]anilist.Media{frieren.ID: frieren}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables struct {
				ID int `json:"id"`
			} `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		m, ok := media[req.Variables.ID]
		if !ok {
			w.Write([]byte(`{"data":{"Media":null}}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"Media": m}})
	}))
	t.Cleanup(srv.Close)

	app := &App{
		Config:     config.Default(),
		Paths:      config.Paths{ConfigDir: dir, DataDir: dir, CacheDir: dir},
		ConfigPath: filepath.Join(dir, "config.toml"),
		AniListURL: srv.URL,
	}
	t.Cleanup(app.Close)
	return app
}

func testStore(t *testing.T, app *App) *store.Store {
	t.Helper()
	st, err := app.Store(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func testTracker(t *testing.T, app *App) *tracker.Tracker {
	t.Helper()
	tr, err := app.Tracker(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func listEntry(t *testing.T, app *App, mediaID int) *store.ListEntry {
	t.Helper()
	e, err := app.ListEntry(t.Context(), mediaID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// A list holds one progress number, so it can only follow marks as far as an
// unbroken run from the first episode reaches — in both directions.
func TestSetWatchedKeepsTheListInStep(t *testing.T) {
	app := testApp(t)
	ctx := t.Context()
	short := frieren
	short.Episodes = 12

	// An episode on its own says nothing about the ones before it.
	note, err := app.SetWatched(ctx, short, 5, 5, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "episodes 1–4 aren't watched") {
		t.Errorf("note = %q", note)
	}
	if e := listEntry(t, app, short.ID); e != nil {
		t.Errorf("marking one episode put %+v on the list", e)
	}

	// Filling the gap moves the list to the end of the run.
	if _, err := app.SetWatched(ctx, short, 1, 4, true); err != nil {
		t.Fatal(err)
	}
	if e := listEntry(t, app, short.ID); e == nil || e.Progress != 5 || e.Status != tracker.Current {
		t.Fatalf("after filling the gap: %+v", e)
	}

	// Finishing it completes the show.
	if _, err := app.SetWatched(ctx, short, 6, 12, true); err != nil {
		t.Fatal(err)
	}
	if e := listEntry(t, app, short.ID); e == nil || e.Progress != 12 || e.Status != tracker.Completed {
		t.Fatalf("after the last episode: %+v", e)
	}

	// Unmarking pulls the list back to just before that episode, and a
	// finished show becomes one being watched again.
	note, err = app.SetWatched(ctx, short, 12, 12, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "11 episodes watched") {
		t.Errorf("note = %q", note)
	}
	if e := listEntry(t, app, short.ID); e == nil || e.Progress != 11 || e.Status != tracker.Current {
		t.Fatalf("after unmarking the last episode: %+v", e)
	}

	// A hole in the middle stops the run there, however much is watched above.
	if _, err := app.SetWatched(ctx, short, 3, 3, false); err != nil {
		t.Fatal(err)
	}
	if e := listEntry(t, app, short.ID); e == nil || e.Progress != 2 {
		t.Fatalf("after unmarking episode 3: %+v", e)
	}
	// The episodes above the hole are still watched here.
	progress, err := testStore(t, app).ShowProgress(ctx, short.ID)
	if err != nil {
		t.Fatal(err)
	}
	watched := map[float64]bool{}
	for _, p := range progress {
		watched[p.Episode] = p.Completed
	}
	if !watched[11] || watched[3] || watched[12] {
		t.Errorf("history after unmarking 3 and 12: %v", watched)
	}
}

// WatchedThrough is the snapshot a rewatch keeps, so earlier episodes still
// show as watched.
func TestWatchedThroughTakesTheFurtherOfTheTwo(t *testing.T) {
	app := testApp(t)
	ctx := t.Context()

	if got, err := app.WatchedThrough(ctx, frieren); err != nil || got != 0 {
		t.Fatalf("nothing watched: %d, %v", got, err)
	}
	if _, err := app.SetWatched(ctx, frieren, 1, 3, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.WatchedThrough(ctx, frieren); got != 3 {
		t.Errorf("after watching three episodes: %d", got)
	}

	// A list further along than this machine's history wins.
	if _, err := testTracker(t, app).SetProgress(ctx, frieren, 20); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.WatchedThrough(ctx, frieren); got != 20 {
		t.Errorf("with the list at 20: %d", got)
	}

	// A completed show counts as watched through the last episode.
	if _, err := testTracker(t, app).SetStatus(ctx, frieren.ID, tracker.Completed); err != nil {
		t.Fatal(err)
	}
	if got, _ := app.WatchedThrough(ctx, frieren); got != frieren.Episodes {
		t.Errorf("completed: %d, want %d", got, frieren.Episodes)
	}
}

func TestStatusLabelAndScoreNote(t *testing.T) {
	if got := StatusLabel(tracker.Repeating); got != "rewatching" {
		t.Errorf("Repeating reads as %q", got)
	}
	// A status the UI doesn't name still reads as something.
	if got := StatusLabel("SOMETHING_ELSE"); got != "something_else" {
		t.Errorf("an unknown status reads as %q", got)
	}
	r := tracker.Result{Entry: store.ListEntry{Score: 8.5}, Changed: true}
	if got := ScoreNote(r); !strings.Contains(got, "8.5") {
		t.Errorf("score note = %q", got)
	}
}

func TestFirstLineOf(t *testing.T) {
	if got := FirstLine("one\ntwo\nthree"); got != "one" {
		t.Errorf("got %q", got)
	}
	if got := FirstLine("only"); got != "only" {
		t.Errorf("got %q", got)
	}
}
