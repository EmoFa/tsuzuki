package episodes

import (
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

var frieren = anilist.Media{ID: 154587, Episodes: 28, Status: "FINISHED"}

func TestNextEpisode(t *testing.T) {
	if got := NextEpisode(store.Progress{Episode: 12.5, Completed: true}); got != 13 {
		t.Errorf("after a finished 12.5: %v", got)
	}
	if got := NextEpisode(store.Progress{Episode: 4}); got != 4 {
		t.Errorf("after an unfinished 4: %v", got)
	}
}

// The list covers what aired, what a provider has, and anything watched beyond
// both, such as a special.
func TestNumbers(t *testing.T) {
	s := Show{
		Media:    anilist.Media{Episodes: 2, Status: "FINISHED"},
		Provider: map[float64]domain.Episode{3: {Number: 3}},
		Progress: map[float64]store.Progress{2.5: {Episode: 2.5}},
	}
	got := s.Numbers()
	want := []float64{1, 2, 2.5, 3}
	if len(got) != len(want) {
		t.Fatalf("numbers %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("numbers %v, want %v", got, want)
		}
	}
}

func TestRows(t *testing.T) {
	now := time.Now()
	s := Show{
		Media: frieren,
		Progress: map[float64]store.Progress{
			1: {Episode: 1, Completed: true, UpdatedAt: now.Add(-time.Hour)},
			2: {Episode: 2, Position: 5 * time.Minute, Duration: 24 * time.Minute, UpdatedAt: now},
		},
		Provider: map[float64]domain.Episode{3: {Number: 3, Title: "Killing Magic", Recap: true}},
		Kinds:    map[int]skip.EpisodeKind{4: skip.Filler, 5: skip.Mixed},
		Round:    1,
	}
	rows := s.Rows(true)
	if len(rows) != 28 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, tc := range []struct {
		n     int
		state State
		kind  string
	}{
		{1, Done, ""}, {2, Partial, ""}, {3, Unwatched, "recap"}, {4, Unwatched, "filler"}, {5, Unwatched, "mixed"},
	} {
		r := rows[tc.n-1]
		if r.State != tc.state || r.Kind != tc.kind {
			t.Errorf("episode %d: %+v", tc.n, r)
		}
	}
	if rows[1].Position != 5*time.Minute || rows[2].Title != "Killing Magic" {
		t.Errorf("rows %+v %+v", rows[1], rows[2])
	}
	// The unfinished episode is what continue plays.
	if next := s.NextUp(); next != 2 {
		t.Errorf("next up %v", next)
	}
}

// The user's list counts as watched, and continue picks up after it.
func TestTheListCountsAsWatched(t *testing.T) {
	s := Show{Media: frieren, Entry: &store.ListEntry{Status: tracker.Current, Progress: 6}, Round: 1}
	if !s.Watched(6) || s.Watched(7) {
		t.Error("watched doesn't follow the list")
	}
	if next := s.NextUp(); next != 7 {
		t.Errorf("next up %v", next)
	}
	rows := s.Rows(true)
	if rows[5].State != OnList || rows[6].State != Next {
		t.Errorf("rows 6, 7: %+v %+v", rows[5], rows[6])
	}
	// Until progress has loaded, nothing is called next.
	if r := s.Row(7, s.NextUp(), false); r.State != Unwatched {
		t.Errorf("before progress loads: %+v", r)
	}
}

// A rewatch starts again from 1, and what was watched before shows dimmed.
func TestRewatch(t *testing.T) {
	s := Show{Media: frieren, Round: 2, BeforeThrough: 10, WatchedBefore: map[float64]bool{12: true},
		Entry: &store.ListEntry{Status: tracker.Repeating}}
	if !s.Started() {
		t.Error("a rewatch hasn't started")
	}
	if next := s.NextUp(); next != 1 {
		t.Errorf("next up %v", next)
	}
	rows := s.Rows(true)
	if rows[0].State != Next || rows[1].State != Earlier || rows[11].State != Earlier || rows[10].State != Unwatched {
		t.Errorf("rows %+v %+v %+v %+v", rows[0], rows[1], rows[10], rows[11])
	}
}

// A show nobody has started has no next episode to point at.
func TestNothingStarted(t *testing.T) {
	s := Show{Media: frieren, Round: 1}
	if s.Started() {
		t.Error("started")
	}
	if r := s.Row(1, s.NextUp(), true); r.State != Unwatched {
		t.Errorf("row 1 %+v", r)
	}
}

func TestNeedsProviderEpisodes(t *testing.T) {
	if NeedsProviderEpisodes(frieren) {
		t.Error("a finished show with a count")
	}
	if !NeedsProviderEpisodes(anilist.Media{Status: "RELEASING"}) {
		t.Error("an airing show without a count")
	}
}

func TestContinueWatching(t *testing.T) {
	recent := []store.Progress{{MediaID: 1}, {MediaID: 2}, {MediaID: 3}, {MediaID: 4}}
	completed := []store.ListEntry{{MediaID: 2, Status: tracker.Completed}}
	shows, allFinished := ContinueWatching(recent, completed, 2)
	if allFinished || len(shows) != 2 || shows[0].MediaID != 1 || shows[1].MediaID != 3 {
		t.Errorf("shows %+v, allFinished %v", shows, allFinished)
	}
	if shows, allFinished := ContinueWatching(recent[1:2], completed, 30); len(shows) != 0 || !allFinished {
		t.Errorf("only a finished show: %+v, allFinished %v", shows, allFinished)
	}
	if _, allFinished := ContinueWatching(nil, nil, 30); allFinished {
		t.Error("no history counts as all finished")
	}
}

func TestCaughtUp(t *testing.T) {
	airing := anilist.Media{Status: "RELEASING", NextAiringEpisode: &anilist.AiringEpisode{Episode: 6}}
	if !CaughtUp(airing, &store.Progress{Episode: 5, Completed: true}, nil) {
		t.Error("watched every aired episode")
	}
	if CaughtUp(airing, &store.Progress{Episode: 5}, nil) {
		t.Error("episode 5 isn't finished")
	}
	if !CaughtUp(airing, nil, &store.ListEntry{Progress: 5}) {
		t.Error("the list is at the last aired episode")
	}
	if CaughtUp(anilist.Media{Status: "RELEASING"}, &store.Progress{Episode: 9, Completed: true}, nil) {
		t.Error("unknown aired count")
	}
}

// AniList's listings give titles and thumbnails; a provider's title wins.
func TestRowsUseAniListListings(t *testing.T) {
	s := Show{
		Media:    frieren,
		Provider: map[float64]domain.Episode{2: {Number: 2, Title: "From the provider"}},
		Listed: map[float64]anilist.EpisodeInfo{
			1: {Title: "The Journey's End", Thumbnail: "https://img/1.jpg"},
			2: {Title: "From AniList", Thumbnail: "https://img/2.jpg"},
		},
	}
	rows := s.Rows(true)
	if rows[0].Title != "The Journey's End" || rows[0].Thumbnail != "https://img/1.jpg" {
		t.Errorf("episode 1: %+v", rows[0])
	}
	if rows[1].Title != "From the provider" || rows[1].Thumbnail != "https://img/2.jpg" {
		t.Errorf("episode 2: %+v", rows[1])
	}
	if rows[2].Thumbnail != "" || rows[2].Title != "" {
		t.Errorf("episode 3: %+v", rows[2])
	}
}
