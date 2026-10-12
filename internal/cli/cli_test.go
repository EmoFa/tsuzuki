package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/buildinfo"
	"github.com/EmoFa/tsuzuki/internal/config"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
	"github.com/EmoFa/tsuzuki/internal/update"
)

var frieren = anilist.Media{
	ID: 154587, Episodes: 28, Status: "FINISHED", Format: "TV", Year: 2023,
	Title: anilist.Title{English: "Frieren: Beyond Journey's End", Romaji: "Sousou no Frieren"},
}

// testApp builds an app backed by a temporary database, tracking locally and
// answering AniList from a fake, so commands run without touching anything real.
func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	app := newApp()
	app.Config = config.Default()
	app.Paths = config.Paths{ConfigDir: dir, DataDir: dir, CacheDir: dir}
	app.ConfigPath = filepath.Join(dir, "config.toml")
	app.AniListURL = fakeAniList(t, frieren)
	t.Cleanup(app.Close)
	return app
}

// fakeAniList answers media lookups for the given shows.
func fakeAniList(t *testing.T, shows ...anilist.Media) string {
	t.Helper()
	media := map[int]anilist.Media{}
	for _, m := range shows {
		media[m.ID] = m
	}
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
	return srv.URL
}

func testStore(t *testing.T, app *App) *store.Store {
	t.Helper()
	st, err := app.Store(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func listEntry(t *testing.T, app *App, mediaID int) *store.ListEntry {
	t.Helper()
	e, err := app.ListEntry(t.Context(), mediaID)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEpisodeRangeArg(t *testing.T) {
	for _, tc := range []struct {
		arg        string
		from, to   float64
		wantErr    bool
		errMatches string
	}{
		{arg: "7", from: 7, to: 7},
		{arg: "7-12", from: 7, to: 12},
		{arg: " 7 - 12 ", from: 7, to: 12},
		{arg: "7.5", from: 7.5, to: 7.5},
		{arg: "12-7", wantErr: true, errMatches: "episode range"},
		{arg: "0", wantErr: true, errMatches: "positive"},
		{arg: "one", wantErr: true, errMatches: "positive"},
		{arg: "", wantErr: true},
	} {
		from, to, err := episodeRangeArg(tc.arg)
		switch {
		case tc.wantErr && err == nil:
			t.Errorf("%q: no error", tc.arg)
		case tc.wantErr:
			if tc.errMatches != "" && !strings.Contains(err.Error(), tc.errMatches) {
				t.Errorf("%q: error %q, want it to mention %q", tc.arg, err, tc.errMatches)
			}
		case err != nil:
			t.Errorf("%q: %v", tc.arg, err)
		case from != tc.from || to != tc.to:
			t.Errorf("%q: %v-%v, want %v-%v", tc.arg, from, to, to, tc.to)
		}
	}
}

// The mark command is the whole path: arguments, the show it names, the store
// and the list.
func TestMarkCommand(t *testing.T) {
	app := testApp(t)
	cmd := newMarkCmd(app)
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"154587", "1-3"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if note := out.String(); !strings.Contains(note, "episodes 1–3") {
		t.Errorf("said %q", note)
	}
	if e := listEntry(t, app, frieren.ID); e == nil || e.Progress != 3 {
		t.Fatalf("list = %+v", e)
	}

	out.Reset()
	cmd = newMarkCmd(app)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"154587", "3", "--unwatched"})
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if e := listEntry(t, app, frieren.ID); e == nil || e.Progress != 2 {
		t.Fatalf("after unmarking: %+v", e)
	}

	// A show that isn't on AniList is an error, not a crash.
	cmd = newMarkCmd(app)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"999999", "1"})
	if err := cmd.ExecuteContext(t.Context()); err == nil {
		t.Error("marking an unknown show didn't fail")
	}
}

// releases serves GitHub's latest-release endpoint.
func releases(t *testing.T, tag string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"html_url": "https://github.com/EmoFa/tsuzuki/releases/tag/" + tag,
		})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// version sets the running version for one test.
func version(t *testing.T, v string) {
	t.Helper()
	was := buildinfo.Version
	buildinfo.Version = v
	t.Cleanup(func() { buildinfo.Version = was })
}

func TestCheckUpdate(t *testing.T) {
	app := testApp(t)
	app.updateURL = releases(t, "v0.9.0")
	client, err := app.HTTP(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	version(t, "0.4.0")
	st, err := app.CheckUpdate(t.Context(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Latest != "0.9.0" || !st.Available || st.Dev {
		t.Fatalf("a release behind: %+v", st)
	}
	if st.Command == "" {
		t.Error("nothing said about how to upgrade")
	}

	// A build from source is neither behind a release nor up to date with one,
	// and never offers to replace itself.
	version(t, "v0.4.0-2-gab8ec24-dirty")
	st, err = app.CheckUpdate(t.Context(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Available || !st.Dev || st.SelfUpgrade {
		t.Fatalf("dev build: %+v", st)
	}

	// The newest release says so.
	version(t, "0.9.0")
	if st, _ = app.CheckUpdate(t.Context(), client, nil); st.Available {
		t.Fatalf("current version reported an update: %+v", st)
	}
}

// The cache keeps tsuzuki from asking GitHub on every run.
func TestCheckUpdateAsksGitHubOnceADay(t *testing.T) {
	app := testApp(t)
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"tag_name":"v0.9.0","html_url":"https://example.invalid"}`))
	}))
	t.Cleanup(srv.Close)
	app.updateURL = srv.URL
	client, _ := app.HTTP(t.Context())

	version(t, "0.4.0")
	for range 3 {
		if _, err := app.CheckUpdate(t.Context(), client, testStore(t, app)); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("asked GitHub %d times", hits)
	}
}

// A binary a package manager owns is upgraded with that package manager.
func TestUpgradeRefusesAPackagedInstall(t *testing.T) {
	for _, tc := range []struct {
		kind    update.InstallKind
		command string
	}{
		{update.KindScoop, "scoop update tsuzuki"},
		{update.KindWinget, "winget upgrade EmoFa.tsuzuki"},
		{update.KindHomebrew, "brew upgrade tsuzuki"},
	} {
		in := update.Install{Kind: tc.kind, Exe: filepath.Join(t.TempDir(), "tsuzuki")}
		u := &update.Upgrader{Current: "0.4.0", Install: in}
		err := u.Check()
		if err == nil || !strings.Contains(err.Error(), tc.command) {
			t.Errorf("%v: err = %v, want it to name %q", tc.kind, err, tc.command)
		}
	}
}

// Upgrading refuses before it downloads anything when it isn't this command's
// job: the reasons are in internal/update, and this is the wiring.
func TestUpgradeRefusesABuildFromSource(t *testing.T) {
	app := testApp(t)
	version(t, "v0.4.0-2-gab8ec24-dirty")
	var out strings.Builder
	err := runUpgrade(t.Context(), app, &out, false)
	if err == nil || !strings.Contains(err.Error(), "build from source") {
		t.Fatalf("err = %v", err)
	}
	if out.String() != "" {
		t.Errorf("said %q before refusing", out.String())
	}
}

// A rewatch starts a new round: the list says rewatching from nothing, while
// what was watched before is remembered so those episodes still show.
func TestRewatchCommand(t *testing.T) {
	app := testApp(t)
	ctx := t.Context()
	if _, err := app.SetWatched(ctx, frieren, 1, 6, true); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	cmd := newRewatchCmd(app)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"154587"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	if note := out.String(); !strings.Contains(note, "round 2") {
		t.Errorf("said %q", note)
	}

	round, before, err := testStore(t, app).RoundInfo(ctx, frieren.ID)
	if err != nil {
		t.Fatal(err)
	}
	if round != 2 || before != 6 {
		t.Fatalf("round %d, watched before %d", round, before)
	}
	if e := listEntry(t, app, frieren.ID); e == nil || e.Status != tracker.Repeating || e.Progress != 0 {
		t.Fatalf("list = %+v", e)
	}
	// The new round starts empty, and the old one stays in history.
	if progress, _ := testStore(t, app).ShowProgress(ctx, frieren.ID); len(progress) != 0 {
		t.Errorf("the new round already has %d episodes", len(progress))
	}
	if earlier, _ := testStore(t, app).WatchedBefore(ctx, frieren.ID); len(earlier) != 6 {
		t.Errorf("earlier round has %d episodes, want 6", len(earlier))
	}
}

func TestRateCommand(t *testing.T) {
	app := testApp(t)
	ctx := t.Context()
	if _, err := app.SetWatched(ctx, frieren, 1, 1, true); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	cmd := newRateCmd(app)
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"154587", "9"})
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	if e := listEntry(t, app, frieren.ID); e == nil || e.Score != 9 {
		t.Fatalf("list = %+v", e)
	}

	// Out of range is refused before anything is written.
	cmd = newRateCmd(app)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"154587", "11"})
	if err := cmd.ExecuteContext(ctx); err == nil {
		t.Error("11 out of 10 was accepted")
	}
	if e := listEntry(t, app, frieren.ID); e == nil || e.Score != 9 {
		t.Fatalf("score changed to %v", e.Score)
	}
}

// What the list and history commands print is how most of a user's progress is
// read back outside the interface.
func TestListAndHistoryCommands(t *testing.T) {
	app := testApp(t)
	ctx := t.Context()

	var out strings.Builder
	cmd := newListCmd(app)
	cmd.SetOut(&out)
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "empty") {
		t.Errorf("an empty list said %q", out.String())
	}

	if _, err := app.SetWatched(ctx, frieren, 1, 4, true); err != nil {
		t.Fatal(err)
	}
	if err := testStore(t, app).SaveProgress(ctx, store.Progress{
		MediaID: frieren.ID, Episode: 5, Position: 90 * time.Second, Duration: 24 * time.Minute,
		Provider: "anikoto", Mode: "sub",
	}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		cmd     *cobra.Command
		args    []string
		wants   []string
		unwants []string
	}{
		{name: "list", cmd: newListCmd(app), wants: []string{"Frieren", "watching", "4/28"}},
		{name: "list watching", cmd: newListCmd(app), args: []string{"watching"}, wants: []string{"Frieren"}},
		{name: "list completed", cmd: newListCmd(app), args: []string{"completed"}, wants: []string{"empty"}, unwants: []string{"Frieren"}},
		{name: "history", cmd: newHistoryCmd(app), wants: []string{"Frieren", "5 (01:30 / 24:00)", "just now"}},
	} {
		out.Reset()
		tc.cmd.SetOut(&out)
		tc.cmd.SetArgs(tc.args)
		if err := tc.cmd.ExecuteContext(ctx); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, want := range tc.wants {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: missing %q in:\n%s", tc.name, want, out.String())
			}
		}
		for _, unwant := range tc.unwants {
			if strings.Contains(out.String(), unwant) {
				t.Errorf("%s: shouldn't mention %q in:\n%s", tc.name, unwant, out.String())
			}
		}
	}

	// An unknown status is a clear error rather than an empty list.
	cmd = newListCmd(app)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"nonsense"})
	if err := cmd.ExecuteContext(ctx); err == nil || !strings.Contains(err.Error(), "unknown status") {
		t.Errorf("err = %v", err)
	}
}
