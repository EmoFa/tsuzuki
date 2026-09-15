package mapping

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/store"
)

var frierenS2 = anilist.Media{
	ID: 182255, IDMal: 59978, Episodes: 10, Status: "FINISHED", Year: 2026,
	Title: anilist.Title{English: "Frieren: Beyond Journey’s End Season 2", Romaji: "Sousou no Frieren 2nd Season"},
}

type fakeProvider struct {
	name     string
	results  map[string][]domain.Show // by query
	ids      map[string][2]int        // show ID -> anilist, mal
	searches []string
	resolved []string
	err      error
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Search(_ context.Context, q string, _ domain.Mode) ([]domain.Show, error) {
	f.searches = append(f.searches, q)
	if f.err != nil {
		return nil, f.err
	}
	return f.results[q], nil
}

func (f *fakeProvider) Episodes(context.Context, string, domain.Mode) ([]domain.Episode, error) {
	return nil, nil
}

func (f *fakeProvider) Streams(context.Context, string, domain.Episode, domain.Mode) ([]domain.Stream, error) {
	return nil, nil
}

// resolvingProvider adds ExternalIDs.
type resolvingProvider struct{ *fakeProvider }

func (r resolvingProvider) ExternalIDs(_ context.Context, id string) (int, int, error) {
	r.resolved = append(r.resolved, id)
	ids := r.ids[id]
	return ids[0], ids[1], nil
}

type memStore struct {
	mu sync.Mutex
	m  map[string]store.Mapping
}

func (s *memStore) Mapping(_ context.Context, mediaID int, provider string) (*store.Mapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.m[provider]; ok && m.MediaID == mediaID {
		return &m, nil
	}
	return nil, nil
}

func (s *memStore) SaveMapping(_ context.Context, m store.Mapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[m.Provider] = m
	return nil
}

func newStore() *memStore { return &memStore{m: map[string]store.Mapping{}} }

func TestMatchByMalIDInSearchResults(t *testing.T) {
	// Senshi-like: MAL IDs in results; the first result is the wrong season.
	p := &fakeProvider{name: "senshi", results: map[string][]domain.Show{
		"Frieren: Beyond Journey's End Season 2": {
			{ID: "s1", Title: "Sousou no Frieren", MalID: 52991},
			{ID: "s2", Title: "Sousou no Frieren 2nd Season", MalID: 59978},
		},
	}}
	st := newStore()
	show, err := New(st).Resolve(context.Background(), p, frierenS2, domain.Sub)
	if err != nil || show.ID != "s2" {
		t.Fatalf("show=%+v err=%v", show, err)
	}
	if len(p.searches) != 1 || p.searches[0] != "Frieren: Beyond Journey's End Season 2" {
		t.Errorf("searches = %q (curly apostrophe should be straightened)", p.searches)
	}
	if st.m["senshi"].ShowID != "s2" {
		t.Error("mapping not saved")
	}

	// Second resolve uses the saved mapping without searching.
	p.searches = nil
	if show, err := New(st).Resolve(context.Background(), p, frierenS2, domain.Sub); err != nil || show.ID != "s2" || len(p.searches) != 0 {
		t.Fatalf("show=%+v err=%v searches=%v", show, err, p.searches)
	}
}

func TestMatchByResolvingIDs(t *testing.T) {
	// Animepahe-like: no IDs in search; the best title match must be verified.
	base := &fakeProvider{name: "animepahe",
		results: map[string][]domain.Show{
			"Frieren: Beyond Journey's End Season 2": {
				{ID: "mini", Title: "Frieren: Beyond Journey's End Mini Anime", Episodes: 24},
				{ID: "s1", Title: "Frieren: Beyond Journey's End", Episodes: 28},
				{ID: "s2", Title: "Frieren: Beyond Journey's End Season 2", Episodes: 10},
			},
		},
		ids: map[string][2]int{"s1": {154587, 52991}, "s2": {182255, 59978}, "mini": {170068, 56885}},
	}
	show, err := New(newStore()).Resolve(context.Background(), resolvingProvider{base}, frierenS2, domain.Sub)
	if err != nil || show.ID != "s2" {
		t.Fatalf("show=%+v err=%v", show, err)
	}
	if len(base.resolved) != 1 || base.resolved[0] != "s2" {
		t.Errorf("resolved = %v, want only the best title match", base.resolved)
	}
}

func TestConflictingIDsAreRejectedEvenWithExactTitle(t *testing.T) {
	p := &fakeProvider{name: "allanime", results: map[string][]domain.Show{
		"Frieren: Beyond Journey's End Season 2": {
			{ID: "x", Title: "Frieren: Beyond Journey’s End Season 2", AniListID: 1},
		},
	}}
	_, err := New(newStore()).Resolve(context.Background(), p, frierenS2, domain.Sub)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestTitleOnlyFallback(t *testing.T) {
	results := map[string][]domain.Show{
		"Sousou no Frieren 2nd Season": {{ID: "t", Title: "Sousou no Frieren Season 2", Episodes: 10}},
	}
	show, err := New(newStore()).Resolve(context.Background(), &fakeProvider{name: "p", results: results}, frierenS2, domain.Sub)
	if err != nil || show.ID != "t" {
		t.Fatalf("show=%+v err=%v", show, err)
	}

	// Same title but a very different episode count isn't accepted.
	results["Sousou no Frieren 2nd Season"][0].Episodes = 64
	_, err = New(newStore()).Resolve(context.Background(), &fakeProvider{name: "p", results: results}, frierenS2, domain.Sub)
	if !errors.Is(err, ErrNoMatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestMissesAreRememberedAndSearchErrorsSurface(t *testing.T) {
	p := &fakeProvider{name: "p", results: map[string][]domain.Show{}}
	m := New(newStore())
	for range 2 {
		if _, err := m.Resolve(context.Background(), p, frierenS2, domain.Sub); !errors.Is(err, ErrNoMatch) {
			t.Fatalf("err = %v", err)
		}
	}
	if len(p.searches) != 2 { // English + romaji once, not again
		t.Fatalf("searches = %v", p.searches)
	}

	boom := errors.New("site down")
	_, err := New(newStore()).Resolve(context.Background(), &fakeProvider{name: "p", err: boom}, frierenS2, domain.Sub)
	if !errors.Is(err, boom) || errors.Is(err, ErrNoMatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Frieren: Beyond Journey’s End Season 2":   "frieren beyond journeys end season 2",
		"Sousou no Frieren 2nd Season":             "sousou no frieren season 2",
		"Attack on Titan: Final Season":            "attack on titan final season",
		"Kaguya-sama: Love Is War – Second Season": "kaguya sama love is war season 2",
		"Fate/stay night & UBW":                    "fate stay night and ubw",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
