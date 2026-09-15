package tracker

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/store"
)

var frieren2 = anilist.Media{ID: 182255, Episodes: 10}

type fakeRemote struct {
	mu    sync.Mutex
	saved []string
	fail  error
	list  []anilist.ListItem
}

func (f *fakeRemote) SaveListEntry(_ context.Context, mediaID int, status string, progress int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.saved = append(f.saved, strings.Join([]string{itoa(mediaID), status, itoa(progress)}, ":"))
	return nil
}

func (f *fakeRemote) UserList(context.Context) ([]anilist.ListItem, error) { return f.list, nil }

func itoa(n int) string { return strconv.Itoa(n) }

func newStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestEpisodeWatchedProgressAndStatus(t *testing.T) {
	st := newStore(t)
	remote := &fakeRemote{}
	tr := &Tracker{Store: st, Remote: remote}
	ctx := context.Background()

	r, err := tr.EpisodeWatched(ctx, frieren2, 3)
	if err != nil || !r.Changed || !r.Synced || r.Entry.Status != Current || r.Entry.Progress != 3 {
		t.Fatalf("ep3: %+v err=%v", r, err)
	}
	// Rewatching an earlier episode changes nothing and sends nothing.
	if r, _ := tr.EpisodeWatched(ctx, frieren2, 2); r.Changed {
		t.Fatalf("rewatch changed the list: %+v", r)
	}
	// A .5 recap doesn't advance past the whole episode.
	if r, _ := tr.EpisodeWatched(ctx, frieren2, 3.5); r.Changed {
		t.Fatalf("3.5 changed the list: %+v", r)
	}
	r, _ = tr.EpisodeWatched(ctx, frieren2, 10)
	if r.Entry.Status != Completed || r.Entry.Progress != 10 {
		t.Fatalf("final episode: %+v", r)
	}
	if strings.Join(remote.saved, ",") != "182255:CURRENT:3,182255:COMPLETED:10" {
		t.Fatalf("remote saves = %v", remote.saved)
	}
	if pending, _ := st.PendingSyncs(ctx); len(pending) != 0 {
		t.Fatalf("queue not drained: %+v", pending)
	}
}

func TestOfflineChangesQueueAndFlushLater(t *testing.T) {
	st := newStore(t)
	remote := &fakeRemote{fail: errors.New("dial tcp: no route to host")}
	tr := &Tracker{Store: st, Remote: remote}
	ctx := context.Background()

	r, err := tr.EpisodeWatched(ctx, frieren2, 4)
	if err != nil || r.Synced || r.SyncErr == nil || !strings.Contains(r.Note(), "sync pending") {
		t.Fatalf("offline: %+v err=%v", r, err)
	}
	tr.EpisodeWatched(ctx, frieren2, 5)
	pending, _ := st.PendingSyncs(ctx)
	if len(pending) != 1 || pending[0].Progress != 5 || pending[0].Attempts != 1 {
		t.Fatalf("pending = %+v", pending)
	}
	if e, _ := st.ListEntry(ctx, frieren2.ID); e == nil || e.Progress != 5 {
		t.Fatalf("local entry = %+v", e)
	}

	remote.fail = nil
	sent, err := tr.Flush(ctx)
	if err != nil || sent != 1 || strings.Join(remote.saved, ",") != "182255:CURRENT:5" {
		t.Fatalf("flush sent=%d err=%v saved=%v", sent, err, remote.saved)
	}
}

func TestUnauthorizedStopsWithoutBurningAttempts(t *testing.T) {
	st := newStore(t)
	remote := &fakeRemote{fail: anilist.ErrUnauthorized}
	tr := &Tracker{Store: st, Remote: remote}
	ctx := context.Background()
	r, _ := tr.EpisodeWatched(ctx, frieren2, 1)
	if !errors.Is(r.SyncErr, anilist.ErrUnauthorized) {
		t.Fatalf("sync err = %v", r.SyncErr)
	}
	pending, _ := st.PendingSyncs(ctx)
	if len(pending) != 1 || pending[0].Attempts != 0 {
		t.Fatalf("pending = %+v", pending)
	}
}

func TestLocalOnly(t *testing.T) {
	st := newStore(t)
	tr := &Tracker{Store: st}
	ctx := context.Background()
	r, err := tr.EpisodeWatched(ctx, frieren2, 2)
	if err != nil || !r.Changed || r.Synced || r.SyncErr != nil || r.Note() != "list updated: episode 2 (CURRENT)" {
		t.Fatalf("%+v err=%v note=%q", r, err, r.Note())
	}
	if pending, _ := st.PendingSyncs(ctx); len(pending) != 0 {
		t.Fatal("local-only tracker queued a sync")
	}
}

func TestPullKeepsPendingLocalChanges(t *testing.T) {
	st := newStore(t)
	remote := &fakeRemote{fail: errors.New("offline")}
	tr := &Tracker{Store: st, Remote: remote}
	ctx := context.Background()
	tr.EpisodeWatched(ctx, frieren2, 6) // queued, not synced

	remote.fail = nil
	remote.list = []anilist.ListItem{
		{MediaID: frieren2.ID, Status: Current, Progress: 4},
		{MediaID: 154587, Status: Completed, Progress: 28, Score: 9},
	}
	n, err := tr.Pull(ctx)
	if err != nil || n != 1 {
		t.Fatalf("pulled %d err=%v", n, err)
	}
	if e, _ := st.ListEntry(ctx, frieren2.ID); e.Progress != 6 {
		t.Fatalf("pending local change overwritten: %+v", e)
	}
	if e, _ := st.ListEntry(ctx, 154587); e == nil || e.Status != Completed || e.Score != 9 {
		t.Fatalf("remote entry not imported: %+v", e)
	}
}

func TestSetStatus(t *testing.T) {
	st := newStore(t)
	remote := &fakeRemote{}
	tr := &Tracker{Store: st, Remote: remote}
	ctx := context.Background()
	tr.EpisodeWatched(ctx, frieren2, 3)
	r, err := tr.SetStatus(ctx, frieren2.ID, Paused)
	if err != nil || r.Entry.Progress != 3 || r.Entry.Status != Paused || !r.Synced {
		t.Fatalf("%+v err=%v", r, err)
	}
	if r, _ := tr.SetStatus(ctx, frieren2.ID, Paused); r.Changed {
		t.Fatal("same status reported a change")
	}
	if r, _ := tr.SetStatus(ctx, 1, Planning); r.Entry.Status != Planning || r.Entry.Progress != 0 {
		t.Fatalf("new planning entry = %+v", r.Entry)
	}
}
