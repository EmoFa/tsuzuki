package mapping

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/store"
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

func TestSeriesNotCrowdedOutByArcFilms(t *testing.T) {
	// Anikoto's real results for JoJo (2012): the AniList synonyms name the two
	// arcs, which Anikoto also lists as films, and an OVA shares the bare title.
	jojo := anilist.Media{
		ID: 14719, IDMal: 14719, Format: "TV", Episodes: 26, Status: "FINISHED", Year: 2012,
		Title:    anilist.Title{English: "JoJo's Bizarre Adventure (TV)", Romaji: "JoJo no Kimyou na Bouken (TV)"},
		Synonyms: []string{"JoJo no Kimyou na Bouken (2012)", "JoJo's Bizarre Adventure: Phantom Blood", "JoJo's Bizarre Adventure: Battle Tendency", "JJBA"},
	}
	shows := []domain.Show{
		{ID: "514", Title: "JoJo's Bizarre Adventure: Phantom Blood", AltTitles: []string{"JoJo no Kimyou na Bouken: Phantom Blood"}, Type: "Movie", Episodes: 1},
		{ID: "6678", Title: "JoJo's Bizarre Adventure Part 6: Stone Ocean Part 2", Type: "ONA", Episodes: 12},
		{ID: "1815", Title: "JoJo's Bizarre Adventure", AltTitles: []string{"JoJo no Kimyou na Bouken"}, Type: "OVA", Episodes: 6},
		{ID: "6742", Title: "JoJo's Bizarre Adventure: Battle Tendency", AltTitles: []string{"JoJo no Kimyou na Bouken: Battle Tendency"}, Type: "Movie", Episodes: 2},
		{ID: "2483", Title: "JoJo's Bizarre Adventure", AltTitles: []string{"JoJo no Kimyou na Bouken: Adventure"}, Type: "OVA", Episodes: 7},
		{ID: "6844", Title: "JoJo's Bizarre Adventure Part 2: Stardust Crusaders", Type: "TV", Episodes: 24},
		{ID: "6845", Title: "JoJo's Bizarre Adventure", AltTitles: []string{"JoJo no Kimyou na Bouken"}, Type: "TV", Episodes: 26},
		{ID: "1485", Title: "JoJo's Bizarre Adventure (Uncensored)", AltTitles: []string{"JoJo no Kimyou na Bouken (Uncensored)"}, Type: "TV", Episodes: 26},
	}
	base := &fakeProvider{name: "anikoto",
		results: map[string][]domain.Show{"JoJo's Bizarre Adventure (TV)": shows, "JoJo no Kimyou na Bouken (TV)": shows},
		ids: map[string][2]int{"514": {0, 666}, "6742": {0, 665}, "1815": {0, 666}, "2483": {0, 665},
			"6678": {0, 51606}, "6844": {0, 20899}, "6845": {0, 14719}, "1485": {0, 14719}},
	}
	show, err := New(newStore()).Resolve(context.Background(), resolvingProvider{base}, jojo, domain.Sub)
	if err != nil || show.ID != "6845" {
		t.Fatalf("show=%+v err=%v (verified %v)", show, err, base.resolved)
	}
	if len(base.resolved) != 1 {
		t.Errorf("verified %v, want the series first", base.resolved)
	}
}

func TestEntriesWithoutIDsDontExhaustLookups(t *testing.T) {
	// Anikoto's Stardust Crusaders: the right entry (mislabelled "Part 2") has no
	// MAL ID; its uncensored copy does, but ranks fourth.
	sc := anilist.Media{
		ID: 20474, IDMal: 20899, Format: "TV", Episodes: 24, Status: "FINISHED", Year: 2014,
		Title:    anilist.Title{English: "JoJo's Bizarre Adventure: Stardust Crusaders", Romaji: "JoJo no Kimyou na Bouken: Stardust Crusaders"},
		Synonyms: []string{"JoJo's Bizarre Adventure Part 3: Stardust Crusaders"},
	}
	shows := []domain.Show{
		{ID: "1640", Title: "JoJo's Bizarre Adventure Part 3: Stardust Crusaders 2nd Season (Uncensored)", Type: "TV", Episodes: 24},
		{ID: "6843", Title: "JoJo's Bizarre Adventure Part 3: Stardust Crusaders 2nd Season", Type: "TV", Episodes: 24},
		{ID: "6844", Title: "JoJo's Bizarre Adventure Part 2: Stardust Crusaders", Type: "TV", Episodes: 24},
		{ID: "1503", Title: "JoJo's Bizarre Adventure Part 2: Stardust Crusaders (Uncensored)", Type: "TV", Episodes: 24},
		{ID: "514", Title: "JoJo's Bizarre Adventure: Phantom Blood", Type: "Movie", Episodes: 1},
		{ID: "6742", Title: "JoJo's Bizarre Adventure: Battle Tendency", Type: "Movie", Episodes: 2},
	}
	base := &fakeProvider{name: "anikoto",
		results: map[string][]domain.Show{"JoJo's Bizarre Adventure: Stardust Crusaders": shows},
		ids:     map[string][2]int{"1640": {0, 26055}, "1503": {0, 20899}, "514": {0, 666}, "6742": {0, 665}},
	}
	show, err := New(newStore()).Resolve(context.Background(), resolvingProvider{base}, sc, domain.Sub)
	if err != nil || show.ID != "1503" {
		t.Fatalf("show=%+v err=%v (verified %v)", show, err, base.resolved)
	}
	for _, id := range base.resolved {
		if id == "514" || id == "6742" {
			t.Errorf("looked up film %s while matching a series (verified %v)", id, base.resolved)
		}
	}
}

func TestFormatMatch(t *testing.T) {
	for _, tc := range []struct {
		format, typ string
		want        idResult
	}{
		{"TV", "TV", matchYes},
		{"TV_SHORT", "TV", matchYes},
		{"ONA", "ONA", matchYes},
		{"MOVIE", "Movie", matchYes},
		{"TV", "ONA", matchUnknown},
		{"OVA", "Special", matchUnknown},
		{"TV", "Movie", matchNo},
		{"MOVIE", "TV", matchNo},
		{"TV", "", matchUnknown},
	} {
		if got := formatMatch(tc.format, tc.typ); got != tc.want {
			t.Errorf("formatMatch(%q, %q) = %v, want %v", tc.format, tc.typ, got, tc.want)
		}
	}
}

func TestConflictingIDsAreRejectedEvenWithExactTitle(t *testing.T) {
	p := &fakeProvider{name: "senshi", results: map[string][]domain.Show{
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
		"JoJo no Kimyou na Bouken (TV)":            "jojo no kimyou na bouken",
		"Hunter x Hunter (2011)":                   "hunter x hunter",
		"Mob Psycho 100 (Uncensored)":              "mob psycho 100 uncensored",
	} {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
