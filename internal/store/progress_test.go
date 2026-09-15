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
