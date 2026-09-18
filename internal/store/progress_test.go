package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestProgressUpsertAndCompletedIsSticky(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	base := time.Now()

	save := func(ep float64, pos time.Duration, done bool, at time.Time) {
		t.Helper()
		err := s.SaveProgress(ctx, Progress{MediaID: 1, Episode: ep, Position: pos, Duration: 24 * time.Minute,
			Completed: done, Provider: "senshi", Mode: "sub", UpdatedAt: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	save(1, 23*time.Minute, true, base)
	save(1, 2*time.Minute, false, base.Add(time.Minute)) // rewatching

	p, err := s.EpisodeProgress(ctx, 1, 1)
	if err != nil || p == nil {
		t.Fatalf("p=%v err=%v", p, err)
	}
	if !p.Completed || p.Position != 2*time.Minute || p.Provider != "senshi" {
		t.Fatalf("progress = %+v", p)
	}
	if p, _ := s.EpisodeProgress(ctx, 1, 2); p != nil {
		t.Fatal("unwatched episode should be nil")
	}
}

func TestRecentShowsReturnsLatestEpisodePerShow(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	base := time.Now().Add(-time.Hour)
	for _, p := range []Progress{
		{MediaID: 10, Episode: 1, UpdatedAt: base},
		{MediaID: 10, Episode: 2, UpdatedAt: base.Add(3 * time.Minute)},
		{MediaID: 20, Episode: 5, UpdatedAt: base.Add(2 * time.Minute)},
		{MediaID: 30, Episode: 1, UpdatedAt: base.Add(time.Minute)},
	} {
		p.Provider, p.Mode = "senshi", "sub"
		if err := s.SaveProgress(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := s.RecentShows(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 || recent[0].MediaID != 10 || recent[0].Episode != 2 || recent[1].MediaID != 20 {
		t.Fatalf("recent = %+v", recent)
	}

	eps, err := s.ShowProgress(ctx, 10)
	if err != nil || len(eps) != 2 || eps[0].Episode != 1 {
		t.Fatalf("eps=%+v err=%v", eps, err)
	}
}

func TestMappings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if m, err := s.Mapping(ctx, 1, "senshi"); m != nil || err != nil {
		t.Fatalf("m=%v err=%v", m, err)
	}
	for _, m := range []Mapping{
		{MediaID: 1, Provider: "senshi", ShowID: "a", ShowTitle: "A"},
		{MediaID: 1, Provider: "senshi", ShowID: "b", ShowTitle: "B", Manual: true},
		{MediaID: 1, Provider: "animepahe", ShowID: "c", ShowTitle: "C"},
	} {
		if err := s.SaveMapping(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	m, err := s.Mapping(ctx, 1, "senshi")
	if err != nil || m.ShowID != "b" || !m.Manual {
		t.Fatalf("m=%+v err=%v", m, err)
	}
	if all, _ := s.Mappings(ctx, 1); len(all) != 2 {
		t.Fatalf("mappings = %+v", all)
	}
	if err := s.DeleteMappings(ctx, 1, "senshi"); err != nil {
		t.Fatal(err)
	}
	if all, _ := s.Mappings(ctx, 1); len(all) != 1 || all[0].Provider != "animepahe" {
		t.Fatalf("after delete: %+v", all)
	}
	s.DeleteMappings(ctx, 1, "")
	if all, _ := s.Mappings(ctx, 1); len(all) != 0 {
		t.Fatalf("after delete all: %+v", all)
	}
}

func TestKV(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, _, ok, err := s.GetKV(ctx, "k"); ok || err != nil {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	s.PutKV(ctx, "k", "v1")
	s.PutKV(ctx, "k", "v2")
	v, updated, ok, err := s.GetKV(ctx, "k")
	if err != nil || !ok || v != "v2" || time.Since(updated) > time.Minute {
		t.Fatalf("v=%q updated=%v ok=%v err=%v", v, updated, ok, err)
	}
}

func TestRewatchRounds(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	at := time.Now().Add(-time.Hour).Truncate(time.Millisecond)
	save := func(episode float64, completed bool) {
		t.Helper()
		at = at.Add(time.Minute) // distinct times, so "most recent" is unambiguous
		if err := s.SaveProgress(ctx, Progress{MediaID: 1, Episode: episode, Position: time.Minute,
			Duration: 24 * time.Minute, Completed: completed, Provider: "senshi", Mode: "sub", UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}

	if round, err := s.Round(ctx, 1); round != 1 || err != nil {
		t.Fatalf("first round = %d, %v", round, err)
	}
	save(1, true)
	save(2, true)

	round, err := s.StartRound(ctx, 1, 2)
	if round != 2 || err != nil {
		t.Fatalf("StartRound = %d, %v", round, err)
	}
	// The new round starts empty, and the old episodes are remembered.
	eps, err := s.ShowProgress(ctx, 1)
	if err != nil || len(eps) != 0 {
		t.Fatalf("progress after rewatch = %+v, %v", eps, err)
	}
	if p, _ := s.EpisodeProgress(ctx, 1, 1); p != nil {
		t.Fatalf("episode 1 still has progress: %+v", p)
	}
	before, err := s.WatchedBefore(ctx, 1)
	if err != nil || !before[1] || !before[2] || len(before) != 2 {
		t.Fatalf("watched before = %v, %v", before, err)
	}

	// Watching again records against the new round, leaving round 1 intact.
	save(1, true)
	if eps, _ = s.ShowProgress(ctx, 1); len(eps) != 1 || eps[0].Round != 2 {
		t.Fatalf("round 2 progress = %+v", eps)
	}
	var rows int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM watch_progress WHERE media_id = 1").Scan(&rows); err != nil || rows != 3 {
		t.Fatalf("stored rows = %d, %v (history should be kept)", rows, err)
	}
	// Recent shows don't care which round an episode belongs to.
	if recent, err := s.RecentShows(ctx, 5); err != nil || len(recent) != 1 || recent[0].Episode != 1 {
		t.Fatalf("recent = %+v, %v", recent, err)
	}
}

func TestSetWatched(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	if err := s.SetWatched(ctx, 1, 3, true); err != nil {
		t.Fatal(err)
	}
	p, err := s.EpisodeProgress(ctx, 1, 3)
	if err != nil || p == nil || !p.Completed {
		t.Fatalf("marked = %+v, %v", p, err)
	}

	// Marking an episode watched keeps a position already recorded.
	if err := s.SaveProgress(ctx, Progress{MediaID: 1, Episode: 4, Position: 5 * time.Minute,
		Duration: 24 * time.Minute, Provider: "senshi", Mode: "sub"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetWatched(ctx, 1, 4, true); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.EpisodeProgress(ctx, 1, 4); p == nil || !p.Completed || p.Position != 5*time.Minute {
		t.Fatalf("episode 4 = %+v", p)
	}

	// Unmarking forgets it.
	if err := s.SetWatched(ctx, 1, 4, false); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.EpisodeProgress(ctx, 1, 4); p != nil {
		t.Fatalf("still recorded: %+v", p)
	}

	// Marks belong to the current round only.
	if _, err := s.StartRound(ctx, 1, 0); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.EpisodeProgress(ctx, 1, 3); p != nil {
		t.Fatalf("round 2 inherited the mark: %+v", p)
	}
	if before, _ := s.WatchedBefore(ctx, 1); !before[3] {
		t.Error("round 1's mark should show as watched before")
	}
}

func TestRecentShowsPicksTheFurthestEpisodeOnTies(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	// Episodes marked in one go share a moment; the furthest one should win.
	at := time.Now().Truncate(time.Millisecond)
	for ep := 1.0; ep <= 71; ep++ {
		if err := s.SaveProgress(ctx, Progress{MediaID: 1, Episode: ep, Completed: true,
			Provider: "senshi", Mode: "sub", UpdatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	recent, err := s.RecentShows(ctx, 5)
	if err != nil || len(recent) != 1 || recent[0].Episode != 71 {
		t.Fatalf("recent = %+v, %v (want episode 71, not the first one)", recent, err)
	}
}

func TestStartRoundRemembersHowFarWatched(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	round, before, err := s.RoundInfo(ctx, 1)
	if round != 1 || before != 0 || err != nil {
		t.Fatalf("first watch: round %d, before %d, %v", round, before, err)
	}

	// Rewatching a show finished elsewhere: nothing was played here, but the
	// snapshot remembers how far it had been watched.
	if _, err := s.StartRound(ctx, 1, 28); err != nil {
		t.Fatal(err)
	}
	if round, before, _ = s.RoundInfo(ctx, 1); round != 2 || before != 28 {
		t.Fatalf("round %d, watched before %d", round, before)
	}

	// A further rewatch keeps the furthest point, so nothing stops being dim.
	if _, err := s.StartRound(ctx, 1, 3); err != nil {
		t.Fatal(err)
	}
	if round, before, _ = s.RoundInfo(ctx, 1); round != 3 || before != 28 {
		t.Fatalf("round %d, watched before %d", round, before)
	}
}
