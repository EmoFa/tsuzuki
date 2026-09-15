package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/player"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/store"
)

var media = anilist.Media{ID: 182255, Title: anilist.Title{English: "Frieren S2"}, Episodes: 3, Status: "FINISHED"}

type fakeProvider struct {
	name       string
	eps        int
	streamsErr error
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Search(context.Context, string, domain.Mode) ([]domain.Show, error) {
	return nil, nil
}
func (f *fakeProvider) Episodes(context.Context, string, domain.Mode) ([]domain.Episode, error) {
	var out []domain.Episode
	for i := 1; i <= f.eps; i++ {
		out = append(out, domain.Episode{ID: f.name + "-ep", Number: float64(i)})
	}
	return out, nil
}
func (f *fakeProvider) Streams(_ context.Context, show string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error) {
	if f.streamsErr != nil {
		return nil, f.streamsErr
	}
	return []domain.Stream{
		{URL: "https://" + f.name + "/720.m3u8", Height: 720, Audio: mode},
		{URL: "https://" + f.name + "/1080.m3u8", Height: 1080, Audio: mode},
	}, nil
}

type fakeResolver struct{}

func (fakeResolver) Resolve(_ context.Context, p provider.Provider, m anilist.Media, _ domain.Mode) (domain.Show, error) {
	return domain.Show{Provider: p.Name(), ID: "show"}, nil
}

// script is what one fake mpv run does: its events and final state.
type script struct {
	events []player.Event
	final  player.State
}

type fakePlayback struct {
	events chan player.Event
	final  player.State
}

func (f *fakePlayback) Events() <-chan player.Event { return f.events }
func (f *fakePlayback) State() player.State         { return f.final }
func (f *fakePlayback) Wait() error                 { return nil }
func (f *fakePlayback) Close() error                { return nil }

type harness struct {
	sess     *Session
	store    *store.Store
	mu       sync.Mutex
	requests []player.Request
	statuses []Status
}

func newHarness(t *testing.T, providers []provider.Provider, scripts ...script) *harness {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	h := &harness{store: st}
	var names []string
	for _, p := range providers {
		names = append(names, p.Name())
	}
	h.sess = New(Deps{
		Settings: Settings{
			ProviderOrder: names, Quality: "best", AutoplayNext: true,
			WatchedThreshold: 0.85, ResumeRewind: 5 * time.Second,
		},
		Providers: provider.NewRegistry(providers...),
		Resolver:  fakeResolver{},
		Progress:  st,
		Play: func(_ context.Context, req player.Request) (Playback, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if len(h.requests) >= len(scripts) {
				t.Fatalf("unexpected extra playback: %+v", req)
			}
			sc := scripts[len(h.requests)]
			h.requests = append(h.requests, req)
			ch := make(chan player.Event, len(sc.events))
			for _, e := range sc.events {
				ch <- e
			}
			close(ch)
			return &fakePlayback{events: ch, final: sc.final}, nil
		},
		Proxy: func() (Proxy, error) { return nil, errors.New("no proxy in tests") },
		OnStatus: func(s Status) {
			h.mu.Lock()
			h.statuses = append(h.statuses, s)
			h.mu.Unlock()
		},
	})
	return h
}

func (h *harness) kinds(kind StatusKind) []Status {
	var out []Status
	for _, s := range h.statuses {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

func played(pos, dur time.Duration, reason string) script {
	return script{
		events: []player.Event{{Kind: player.EventPosition, Position: pos, Duration: dur}, {Kind: player.EventEndFile, Reason: reason}},
		final:  player.State{Position: pos, Duration: dur, EndReason: reason},
	}
}

func TestAutoplayStopsWhenUserQuits(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p},
		played(24*time.Minute, 24*time.Minute, "eof"),
		played(3*time.Minute, 24*time.Minute, "quit"),
	)
	ctx := context.Background()
	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 2 {
		t.Fatalf("played %d episodes, want 2", len(h.requests))
	}
	if h.requests[0].URL != "https://senshi/1080.m3u8" || !strings.Contains(h.requests[1].Title, "Episode 2") {
		t.Errorf("requests = %+v", h.requests)
	}

	ep1, _ := h.store.EpisodeProgress(ctx, media.ID, 1)
	ep2, _ := h.store.EpisodeProgress(ctx, media.ID, 2)
	if ep1 == nil || !ep1.Completed || ep2 == nil || ep2.Completed || ep2.Position != 3*time.Minute {
		t.Fatalf("ep1=%+v ep2=%+v", ep1, ep2)
	}

	// Continue picks up the unfinished episode 2, rewound a little.
	next, err := NextToWatch(ctx, h.store, media.ID)
	if err != nil || next != 2 {
		t.Fatalf("next=%v err=%v", next, err)
	}
}

func TestResumeFromSavedPosition(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p}, played(12*time.Minute, 24*time.Minute, "quit"))
	ctx := context.Background()
	h.store.SaveProgress(ctx, store.Progress{MediaID: media.ID, Episode: 2, Position: 10 * time.Minute, Duration: 24 * time.Minute, Provider: "senshi", Mode: "sub"})

	if err := h.sess.Watch(ctx, Request{Media: media, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if got := h.requests[0]; !strings.Contains(got.Title, "Episode 2") || got.Start != 10*time.Minute-5*time.Second {
		t.Fatalf("request = %+v", got)
	}
}

func TestFallsBackToNextProvider(t *testing.T) {
	broken := &fakeProvider{name: "senshi", eps: 3, streamsErr: errors.New("source 403")}
	working := &fakeProvider{name: "animepahe", eps: 3}
	h := newHarness(t, []provider.Provider{broken, working}, played(time.Minute, 24*time.Minute, "quit"))

	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if h.requests[0].URL != "https://animepahe/1080.m3u8" {
		t.Fatalf("request = %+v", h.requests[0])
	}
	failed := h.kinds(StatusProviderFailed)
	if len(failed) != 1 || failed[0].Provider != "senshi" || !strings.Contains(failed[0].Err.Error(), "403") {
		t.Fatalf("failures = %+v", failed)
	}
}

func TestUnavailableEverywhere(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p})
	err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 4, Mode: domain.Sub})
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestWatchedThresholdAndLastEpisode(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p}, played(24*time.Minute, 24*time.Minute, "eof"))
	ctx := context.Background()
	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 3, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(h.kinds(StatusWatched)) != 1 || len(h.kinds(StatusNoNextEpisode)) != 1 {
		t.Fatalf("statuses = %+v", h.statuses)
	}
	if next, _ := NextToWatch(ctx, h.store, media.ID); next != 4 {
		t.Fatalf("next = %v", next)
	}
}

func TestQuitPastThresholdCountsAsWatched(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p}, played(22*time.Minute, 24*time.Minute, "quit"))
	ctx := context.Background()
	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	ep, _ := h.store.EpisodeProgress(ctx, media.ID, 1)
	if ep == nil || !ep.Completed || len(h.kinds(StatusWatched)) != 1 {
		t.Fatalf("ep=%+v statuses=%+v", ep, h.statuses)
	}
	if len(h.requests) != 1 {
		t.Fatal("quitting must not autoplay")
	}
}

func TestPlaybackErrorIsReported(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p}, script{
		events: []player.Event{{Kind: player.EventEndFile, Reason: "error"}},
		final:  player.State{EndReason: "error"},
	})
	ctx := context.Background()
	err := h.sess.Watch(ctx, Request{Media: media, Episode: 1, Mode: domain.Sub})
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("err = %v", err)
	}
	if ep, _ := h.store.EpisodeProgress(ctx, media.ID, 1); ep != nil {
		t.Fatalf("failed playback recorded progress: %+v", ep)
	}
}

type fakeProxy struct{}

func (fakeProxy) Stream(s domain.Stream) string            { return "http://proxy/" + s.URL }
func (fakeProxy) URL(u string, _ map[string]string) string { return "http://proxy/" + u }

func TestPlayerRequest(t *testing.T) {
	s := domain.Stream{
		URL: "https://cdn/x.m3u8", Headers: map[string]string{"Origin": "o"}, AudioLang: "en",
		Subtitles: []domain.Subtitle{{URL: "https://cdn/sub.ass"}},
	}
	direct, err := PlayerRequest(s, nil)
	if err != nil || direct.URL != s.URL || direct.Headers["Origin"] != "o" || direct.Subtitles[0] != "https://cdn/sub.ass" {
		t.Fatalf("direct = %+v err=%v", direct, err)
	}

	s.NeedsProxy = true
	if _, err := PlayerRequest(s, nil); err == nil {
		t.Fatal("expected error without proxy")
	}
	proxied, err := PlayerRequest(s, fakeProxy{})
	if err != nil || proxied.URL != "http://proxy/https://cdn/x.m3u8" || proxied.Headers != nil ||
		proxied.Subtitles[0] != "http://proxy/https://cdn/sub.ass" || proxied.AudioLang != "en" {
		t.Fatalf("proxied = %+v err=%v", proxied, err)
	}
}
