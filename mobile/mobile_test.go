package mobile

import (
	"encoding/json"
	"errors"
	"testing"
)

type fakeHost struct{}

func (fakeHost) OpenURL(string) error { return nil }

func (fakeHost) Play(string, *Playback) (Player, error) { return nil, errors.New("no player in tests") }

func (fakeHost) Sniff(string) (string, error) { return "", errors.New("no browser in tests") }

func (fakeHost) Solve(string) (string, error) { return "", errors.New("no browser in tests") }

// A fresh install starts with an empty list and no login, and its answers are
// JSON the app can read.
func TestNewAppOnAFreshInstall(t *testing.T) {
	a, err := NewApp(t.TempDir(), t.TempDir(), fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()

	acc, err := a.Account()
	if err != nil {
		t.Fatal(err)
	}
	var account struct {
		Backend  string
		LoggedIn bool
	}
	if err := json.Unmarshal([]byte(acc), &account); err != nil || account.LoggedIn || account.Backend == "" {
		t.Errorf("account = %s (%v)", acc, err)
	}

	list, err := a.ListEntries("")
	if err != nil {
		t.Fatal(err)
	}
	if list != "null" && list != "[]" {
		t.Errorf("list = %s", list)
	}
	if _, err := a.SetListStatus(154587, "PLANNING"); err != nil {
		t.Fatal(err)
	}
	entry, err := a.ListEntry(154587)
	if err != nil {
		t.Fatal(err)
	}
	var e struct {
		MediaID int
		Status  string
	}
	if err := json.Unmarshal([]byte(entry), &e); err != nil || e.Status != "PLANNING" {
		t.Errorf("entry = %s (%v)", entry, err)
	}
}

// A show watched partway lists its episodes with the next one marked.
func TestEpisodes(t *testing.T) {
	a, err := NewApp(t.TempDir(), t.TempDir(), fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	media := `{"id":154587,"episodes":4,"status":"FINISHED"}`
	if _, err := a.SetWatched(media, 1, 2, true); err != nil {
		t.Fatal(err)
	}
	got, err := a.Episodes(media, "sub", false)
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Rows []struct {
			Number float64
			State  string
		}
		NextUp  float64
		Round   int
		Loading bool
	}
	if err := json.Unmarshal([]byte(got), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Rows) != 4 || list.NextUp != 3 || list.Round != 1 || list.Loading {
		t.Fatalf("list %s", got)
	}
	want := []string{"watched", "watched", "next", ""}
	for i, r := range list.Rows {
		if r.State != want[i] {
			t.Errorf("episode %v is %q, want %q", r.Number, r.State, want[i])
		}
	}
}

// Settings changed in the app are saved, and a restart reads them back.
func TestSetConfig(t *testing.T) {
	files, cache := t.TempDir(), t.TempDir()
	a, err := NewApp(files, cache, fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.SetConfig(`{"General":{"Mode":"dub"},"Skip":{"Recap":"off"}}`); err != nil {
		t.Fatal(err)
	}
	if err := a.SetConfig(`{"General":{"Mode":"both"}}`); err == nil {
		t.Error("accepted an invalid mode")
	}
	a.Close()

	b, err := NewApp(files, cache, fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	got, err := b.Config()
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		General struct{ Mode, Quality string }
		Skip    struct{ Recap, Opening string }
	}
	if err := json.Unmarshal([]byte(got), &cfg); err != nil {
		t.Fatal(err)
	}
	// Changed values stick; the rest keep their defaults.
	if cfg.General.Mode != "dub" || cfg.Skip.Recap != "off" || cfg.General.Quality != "best" || cfg.Skip.Opening != "auto" {
		t.Errorf("config %s", got)
	}
}

// Diagnose works offline enough to report the app's own setup.
func TestDiagnoseReportsLocalChecks(t *testing.T) {
	a, err := NewApp(t.TempDir(), t.TempDir(), fakeHost{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var got []struct{ Name, Status string }
	if err := json.Unmarshal([]byte(mustString(t)(a.localChecksJSON())), &got); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, c := range got {
		names[c.Name] = c.Status
	}
	if names["database"] != "ok" || names["anilist login"] != "warn" || names["site verifications"] != "ok" {
		t.Errorf("checks %+v", got)
	}
	if err := a.ClearVerifications(); err != nil {
		t.Fatal(err)
	}
}

func mustString(t *testing.T) func(string, error) string {
	return func(s string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
}
