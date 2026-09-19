package tracker

import (
	"context"
	"strings"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/store"
)

func TestListThrough(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry *store.ListEntry
		round int
		want  int
	}{
		{"not on the list", nil, 1, 0},
		{"part way", &store.ListEntry{Status: Current, Progress: 4}, 1, 4},
		{"completed counts the whole show", &store.ListEntry{Status: Completed}, 1, 10},
		{"planning counts nothing", &store.ListEntry{Status: Planning}, 1, 0},
		{"a rewatch's own entry counts", &store.ListEntry{Status: Repeating, Progress: 2}, 2, 2},
		{"an older watch doesn't", &store.ListEntry{Status: Completed, Progress: 10}, 2, 0},
	} {
		if got := ListThrough(tc.entry, frieren2.Episodes, tc.round); got != tc.want {
			t.Errorf("%s: ListThrough = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestWatchedRun(t *testing.T) {
	watched := map[int]bool{5: true, 6: true, 9: true}
	// A hole at 7 stops the run, whatever is watched beyond it.
	if got := WatchedRun(watched, 4, 10); got != 6 {
		t.Errorf("run from 4 = %d, want 6", got)
	}
	// Nothing to add on.
	if got := WatchedRun(watched, 7, 10); got != 7 {
		t.Errorf("run from 7 = %d, want 7", got)
	}
	// An unknown episode count doesn't stop it.
	if got := WatchedRun(map[int]bool{1: true, 2: true}, 0, 0); got != 2 {
		t.Errorf("run with no episode count = %d, want 2", got)
	}
}

func TestSetProgressMovesTheListBothWays(t *testing.T) {
	st := newStore(t)
	remote := &fakeRemote{}
	tr := &Tracker{Store: st, Remote: remote}
	ctx := context.Background()

	if _, err := tr.EpisodeWatched(ctx, frieren2, 10); err != nil {
		t.Fatal(err)
	}
	// Unmarking an episode of a finished show pulls it back to being watched.
	r, err := tr.SetProgress(ctx, frieren2, 4)
	if err != nil || !r.Changed || r.Entry.Status != Current || r.Entry.Progress != 4 {
		t.Fatalf("lowered to %+v err=%v", r.Entry, err)
	}
	// Marking the rest again completes it.
	if r, _ := tr.SetProgress(ctx, frieren2, 10); r.Entry.Status != Completed {
		t.Fatalf("raised to %+v", r.Entry)
	}
	// Nothing to do.
	if r, _ := tr.SetProgress(ctx, frieren2, 10); r.Changed {
		t.Fatalf("unchanged progress still saved: %+v", r)
	}
	if got := strings.Join(remote.saved, ","); got != "182255:COMPLETED:10,182255:CURRENT:4,182255:COMPLETED:10" {
		t.Fatalf("remote saves = %v", remote.saved)
	}

	// A rewatch keeps its status and its score.
	if _, err := tr.StartRewatch(ctx, frieren2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.SetScore(ctx, frieren2.ID, 8); err != nil {
		t.Fatal(err)
	}
	r, _ = tr.SetProgress(ctx, frieren2, 3)
	if r.Entry.Status != Repeating || r.Entry.Progress != 3 || r.Entry.Score != 8 {
		t.Fatalf("during a rewatch: %+v", r.Entry)
	}
}
