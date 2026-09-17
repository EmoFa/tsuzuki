package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/provider"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
)

var media = anilist.Media{ID: 182255, Title: anilist.Title{English: "Frieren S2"}, Episodes: 3, Status: "FINISHED"}

type fakeProvider struct {
	name       string
	eps        int
	streamsErr error
	skips      []domain.SkipRange
	recaps     map[int]bool
	subs       []domain.Subtitle
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Search(context.Context, string, domain.Mode) ([]domain.Show, error) {
	return nil, nil
}
func (f *fakeProvider) Episodes(context.Context, string, domain.Mode) ([]domain.Episode, error) {
	var out []domain.Episode
	for i := 1; i <= f.eps; i++ {
		out = append(out, domain.Episode{ID: f.name + "-ep", Number: float64(i), Recap: f.recaps[i]})
	}
	return out, nil
}
func (f *fakeProvider) Streams(_ context.Context, show string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error) {
	if f.streamsErr != nil {
		return nil, f.streamsErr
	}
	return []domain.Stream{
		{URL: "https://" + f.name + "/720.m3u8", Height: 720, Audio: mode, Skips: f.skips, Subtitles: f.subs},
		{URL: "https://" + f.name + "/1080.m3u8", Height: 1080, Audio: mode, Skips: f.skips, Subtitles: f.subs},
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
	h      *harness
}

func (f *fakePlayback) Events() <-chan player.Event { return f.events }
func (f *fakePlayback) State() player.State         { return f.final }
func (f *fakePlayback) Wait() error                 { return nil }
func (f *fakePlayback) Close() error                { return nil }

func (f *fakePlayback) Seek(_ context.Context, pos time.Duration) error {
	f.h.mu.Lock()
	defer f.h.mu.Unlock()
	f.h.seeks = append(f.h.seeks, pos)
	return nil
}

func (f *fakePlayback) ShowText(_ context.Context, text string, _ time.Duration) error {
	f.h.mu.Lock()
	defer f.h.mu.Unlock()
	f.h.osd = append(f.h.osd, text)
	return nil
}

func (f *fakePlayback) BindKey(context.Context, string, string) error { return nil }

type harness struct {
	sess     *Session
	store    *store.Store
	mu       sync.Mutex
	requests []player.Request
	statuses []Status
	seeks    []time.Duration
	osd      []string
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
			return &fakePlayback{events: ch, final: sc.final, h: h}, nil
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
	subs := domain.SubtitlePrefs{Show: true}
	direct, err := PlayerRequest(s, nil, subs)
	if err != nil || direct.URL != s.URL || direct.Headers["Origin"] != "o" || direct.Subtitles[0] != "https://cdn/sub.ass" || direct.HideSubs {
		t.Fatalf("direct = %+v err=%v", direct, err)
	}

	s.NeedsProxy = true
	if _, err := PlayerRequest(s, nil, subs); err == nil {
		t.Fatal("expected error without proxy")
	}
	proxied, err := PlayerRequest(s, fakeProxy{}, subs)
	if err != nil || proxied.URL != "http://proxy/https://cdn/x.m3u8" || proxied.Headers != nil ||
		proxied.Subtitles[0] != "http://proxy/https://cdn/sub.ass" || proxied.AudioLang != "en" {
		t.Fatalf("proxied = %+v err=%v", proxied, err)
	}
}

func TestHealthCheckTriesOtherQualityThenProvider(t *testing.T) {
	a := &fakeProvider{name: "anikoto", eps: 3}
	b := &fakeProvider{name: "senshi", eps: 3}

	// 1080p on anikoto is dead but 720p works: stay on anikoto.
	h := newHarness(t, []provider.Provider{a, b}, played(time.Minute, 24*time.Minute, "quit"))
	h.sess.Check = func(_ context.Context, s domain.Stream) error {
		if s.URL == "https://anikoto/1080.m3u8" {
			return errors.New("HTTP 404")
		}
		return nil
	}
	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if h.requests[0].URL != "https://anikoto/720.m3u8" {
		t.Fatalf("played %s, want anikoto 720p", h.requests[0].URL)
	}

	// Every anikoto stream is dead: move on to senshi.
	h = newHarness(t, []provider.Provider{a, b}, played(time.Minute, 24*time.Minute, "quit"))
	h.sess.Check = func(_ context.Context, s domain.Stream) error {
		if strings.HasPrefix(s.URL, "https://anikoto/") {
			return errors.New("dead")
		}
		return nil
	}
	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if h.requests[0].URL != "https://senshi/1080.m3u8" {
		t.Fatalf("played %s, want senshi", h.requests[0].URL)
	}
	failed := h.kinds(StatusProviderFailed)
	if len(failed) != 1 || failed[0].Provider != "anikoto" || !strings.Contains(failed[0].Err.Error(), "health check") {
		t.Fatalf("failures = %+v", failed)
	}
}

func TestPlaybackErrorRetriesNextProviderAndResumes(t *testing.T) {
	a := &fakeProvider{name: "anikoto", eps: 3}
	b := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{a, b},
		played(8*time.Minute, 24*time.Minute, "error"),
		played(10*time.Minute, 24*time.Minute, "quit"),
	)
	ctx := context.Background()
	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 2, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 2 {
		t.Fatalf("played %d times, want 2", len(h.requests))
	}
	second := h.requests[1]
	if second.URL != "https://senshi/1080.m3u8" || !strings.Contains(second.Title, "Episode 2") || second.Start != 8*time.Minute-5*time.Second {
		t.Fatalf("retry = %+v", second)
	}
	if p, _ := h.store.EpisodeProgress(ctx, media.ID, 2); p == nil || p.Provider != "senshi" || p.Completed {
		t.Fatalf("progress = %+v", p)
	}
}

func TestPrematureEndIsNotWatchedAndRetries(t *testing.T) {
	a := &fakeProvider{name: "anikoto", eps: 3}
	b := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{a, b},
		played(10*time.Minute, 24*time.Minute, "eof"), // stream cut off
		played(24*time.Minute, 24*time.Minute, "eof"), // played to the end
		played(time.Minute, 24*time.Minute, "quit"),   // autoplayed next, then quit
	)
	ctx := context.Background()
	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 3 || h.requests[1].Start != 10*time.Minute-5*time.Second || !strings.Contains(h.requests[2].Title, "Episode 2") {
		t.Fatalf("requests = %+v", h.requests)
	}
	// Autoplay kept using the provider that worked.
	if h.requests[2].URL != "https://senshi/1080.m3u8" {
		t.Fatalf("episode 2 from %s", h.requests[2].URL)
	}
	if watched := h.kinds(StatusWatched); len(watched) != 1 {
		t.Fatalf("watched statuses = %d, want 1 (the cut-off play must not count)", len(watched))
	}
}

func TestPlaybackFailsEverywhere(t *testing.T) {
	a := &fakeProvider{name: "anikoto", eps: 3}
	b := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{a, b},
		played(time.Minute, 24*time.Minute, "error"),
		played(time.Minute, 24*time.Minute, "error"),
	)
	err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "from anikoto") || !strings.Contains(err.Error(), "from senshi") {
		t.Fatalf("err = %v", err)
	}
}

func TestPrefersLastProviderAndHonoursSkip(t *testing.T) {
	a := &fakeProvider{name: "anikoto", eps: 3}
	b := &fakeProvider{name: "senshi", eps: 3}
	c := &fakeProvider{name: "animepahe", eps: 3}
	h := newHarness(t, []provider.Provider{a, b, c},
		played(time.Minute, 24*time.Minute, "quit"),
		played(time.Minute, 24*time.Minute, "quit"),
	)
	ctx := context.Background()
	h.store.SaveProgress(ctx, store.Progress{MediaID: media.ID, Episode: 1, Completed: true, Duration: 24 * time.Minute, Provider: "animepahe", Mode: "sub"})

	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 2, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if h.requests[0].URL != "https://animepahe/1080.m3u8" {
		t.Fatalf("played %s, want the last provider (animepahe)", h.requests[0].URL)
	}

	if err := h.sess.Watch(ctx, Request{Media: media, Episode: 2, Mode: domain.Sub, SkipProviders: []string{"animepahe", "anikoto"}}); err != nil {
		t.Fatal(err)
	}
	if h.requests[1].URL != "https://senshi/1080.m3u8" {
		t.Fatalf("played %s, want senshi (others skipped)", h.requests[1].URL)
	}
}

func TestOnWatchedTracksOncePerEpisode(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p},
		script{
			events: []player.Event{
				{Kind: player.EventPosition, Position: 21 * time.Minute, Duration: 24 * time.Minute},
				{Kind: player.EventPause},
				{Kind: player.EventSeek},
				{Kind: player.EventEndFile, Reason: "quit"},
			},
			final: player.State{Position: 23 * time.Minute, Duration: 24 * time.Minute, EndReason: "quit"},
		},
	)
	var tracked []float64
	h.sess.OnWatched = func(_ context.Context, m anilist.Media, ep float64) (string, error) {
		tracked = append(tracked, ep)
		return "AniList updated: episode 2", nil
	}
	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 2, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(tracked) != 1 || tracked[0] != 2 {
		t.Fatalf("tracked = %v", tracked)
	}
	st := h.kinds(StatusTracked)
	if len(st) != 1 || st[0].Reason != "AniList updated: episode 2" {
		t.Fatalf("tracked statuses = %+v", st)
	}
}

func TestNextToWatchUsesListProgress(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	if next, _ := NextToWatch(ctx, h.store, media.ID); next != 1 {
		t.Fatalf("new show: next = %v", next)
	}
	// Watched 5 episodes elsewhere (synced from AniList), none locally.
	h.store.SaveListEntry(ctx, store.ListEntry{MediaID: media.ID, Status: "CURRENT", Progress: 5})
	if next, _ := NextToWatch(ctx, h.store, media.ID); next != 6 {
		t.Fatalf("list progress 5: next = %v", next)
	}
	// Local history further along wins.
	h.store.SaveProgress(ctx, store.Progress{MediaID: media.ID, Episode: 8, Position: time.Minute, Duration: 24 * time.Minute, Provider: "x", Mode: "sub"})
	if next, _ := NextToWatch(ctx, h.store, media.ID); next != 8 {
		t.Fatalf("unfinished local episode 8: next = %v", next)
	}
}

func TestSkipsFromStreamAndAniSkip(t *testing.T) {
	// The fake provider's streams carry no skip ranges, so they come from the
	// SkipRanges lookup (AniSkip), which needs the duration first.
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p}, script{
		events: []player.Event{
			{Kind: player.EventDuration, Duration: 24 * time.Minute},
			{Kind: player.EventPosition, Position: 10 * time.Second, Duration: 24 * time.Minute},
			{Kind: player.EventPosition, Position: 118 * time.Second, Duration: 24 * time.Minute},
			{Kind: player.EventPosition, Position: 1345 * time.Second, Duration: 24 * time.Minute},
			{Kind: player.EventMessage, Args: []string{"tsuzuki-skip"}},
			{Kind: player.EventEndFile, Reason: "quit"},
		},
		final: player.State{Position: 1350 * time.Second, Duration: 24 * time.Minute, EndReason: "quit"},
	})
	h.sess.Settings.SkipActions = map[domain.SkipKind]string{domain.SkipOpening: "auto", domain.SkipEnding: "prompt"}
	lookedUp := make(chan struct{})
	h.sess.SkipRanges = func(_ context.Context, _ anilist.Media, ep float64, length time.Duration) ([]domain.SkipRange, error) {
		defer close(lookedUp)
		if ep != 1 || length != 24*time.Minute {
			t.Errorf("lookup ep=%v length=%v", ep, length)
		}
		return []domain.SkipRange{
			{Kind: domain.SkipOpening, Start: 117 * time.Second, End: 207 * time.Second},
			{Kind: domain.SkipEnding, Start: 1340 * time.Second, End: 1430 * time.Second},
		}, nil
	}
	// Hold events until the lookup has delivered, as a real player would keep
	// sending positions while it runs.
	orig := h.sess.Play
	h.sess.Play = func(ctx context.Context, req player.Request) (Playback, error) {
		pb, err := orig(ctx, req)
		if err != nil {
			return nil, err
		}
		fp := pb.(*fakePlayback)
		src := fp.events
		gated := make(chan player.Event)
		go func() {
			defer close(gated)
			first := true
			for e := range src {
				gated <- e
				if first {
					first = false
					<-lookedUp
					time.Sleep(20 * time.Millisecond) // let the ranges reach the loop
				}
			}
		}()
		fp.events = gated
		return fp, nil
	}

	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	// Opening auto-skipped at 1:58; ending prompted at 22:25, then skipped on request.
	if len(h.seeks) != 2 || h.seeks[0] != 207*time.Second || h.seeks[1] != 1430*time.Second {
		t.Fatalf("seeks = %v", h.seeks)
	}
	if strings.Join(h.osd, "|") != "Skipped opening|Ending · press TAB to skip|Skipped ending" {
		t.Fatalf("osd = %q", h.osd)
	}
	if sk := h.kinds(StatusSkipped); len(sk) != 2 || sk[0].Reason != "opening" {
		t.Fatalf("skipped statuses = %+v", sk)
	}
}

func TestStreamSkipsAvoidLookup(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3}
	h := newHarness(t, []provider.Provider{p}, played(time.Minute, 24*time.Minute, "quit"))
	h.sess.Settings.SkipActions = map[domain.SkipKind]string{domain.SkipOpening: "auto"}
	h.sess.SkipRanges = func(context.Context, anilist.Media, float64, time.Duration) ([]domain.SkipRange, error) {
		t.Error("looked up skips although the stream had them")
		return nil, nil
	}
	p.skips = []domain.SkipRange{{Kind: domain.SkipOpening, Start: 30 * time.Second, End: 90 * time.Second}}
	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(h.seeks) != 1 || h.seeks[0] != 90*time.Second {
		t.Fatalf("seeks = %v (position 1:00 is inside the provider's opening)", h.seeks)
	}
}

func TestAutoplaySkipsFillerAndRecapEpisodes(t *testing.T) {
	// Episodes 2-3 are filler (animefillerlist), 4 is a recap (provider flag), 5 filler.
	p := &fakeProvider{name: "senshi", eps: 7, recaps: map[int]bool{4: true}}
	h := newHarness(t, []provider.Provider{p},
		played(24*time.Minute, 24*time.Minute, "eof"), // episode 1
		played(time.Minute, 24*time.Minute, "quit"),   // episode 6
	)
	h.sess.Settings.SkipFillerEpisodes, h.sess.Settings.SkipRecapEpisodes = true, true
	h.sess.EpisodeKinds = func(context.Context, anilist.Media) (map[int]skip.EpisodeKind, error) {
		return map[int]skip.EpisodeKind{1: skip.Canon, 2: skip.Filler, 3: skip.Filler, 5: skip.Filler, 6: skip.Mixed}, nil
	}
	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 1, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if len(h.requests) != 2 || !strings.Contains(h.requests[1].Title, "Episode 6") {
		t.Fatalf("requests = %+v", h.requests)
	}
	var skipped []string
	for _, st := range h.kinds(StatusEpisodeSkipped) {
		skipped = append(skipped, fmt.Sprintf("%v-%v:%s", st.Episode, st.Through, st.Reason))
	}
	if strings.Join(skipped, ",") != "2-3:filler,4-4:recap,5-5:filler" {
		t.Fatalf("skipped = %v", skipped)
	}
}

func TestExplicitFillerEpisodePlaysButContinueSkipsIt(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 4}
	kinds := func(context.Context, anilist.Media) (map[int]skip.EpisodeKind, error) {
		return map[int]skip.EpisodeKind{2: skip.Filler}, nil
	}

	h := newHarness(t, []provider.Provider{p}, played(time.Minute, 24*time.Minute, "quit"))
	h.sess.Settings.SkipFillerEpisodes, h.sess.EpisodeKinds = true, kinds
	if err := h.sess.Watch(context.Background(), Request{Media: media, Episode: 2, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.requests[0].Title, "Episode 2") {
		t.Fatalf("explicit filler episode not played: %+v", h.requests)
	}

	h = newHarness(t, []provider.Provider{p}, played(time.Minute, 24*time.Minute, "quit"))
	h.sess.Settings.SkipFillerEpisodes, h.sess.EpisodeKinds = true, kinds
	ctx := context.Background()
	h.store.SaveProgress(ctx, store.Progress{MediaID: media.ID, Episode: 1, Completed: true, Duration: 24 * time.Minute, Provider: "senshi", Mode: "sub"})
	if err := h.sess.Watch(ctx, Request{Media: media, Mode: domain.Sub}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.requests[0].Title, "Episode 3") {
		t.Fatalf("continue landed on filler: %+v", h.requests)
	}
}

func TestSubtitlePreferences(t *testing.T) {
	p := &fakeProvider{name: "senshi", eps: 3, subs: []domain.Subtitle{
		{URL: "https://s/en.vtt", Lang: "en"}, {URL: "https://s/de.vtt", Lang: "de"}, {URL: "https://s/es.vtt", Lang: "es"},
	}}
	watchOnce := func(configure func(h *harness), req Request) player.Request {
		t.Helper()
		h := newHarness(t, []provider.Provider{p}, played(time.Minute, 24*time.Minute, "quit"))
		h.sess.Settings.Subtitles = domain.SubtitlePrefs{Languages: []string{"de"}, Show: true}
		h.sess.ShowPrefs = h.store.ShowPrefs
		if configure != nil {
			configure(h)
		}
		req.Media, req.Episode, req.Mode = media, 1, domain.Sub
		if err := h.sess.Watch(context.Background(), req); err != nil {
			t.Fatal(err)
		}
		return h.requests[0]
	}

	// The config.
	r := watchOnce(nil, Request{})
	if r.Subtitles[0] != "https://s/de.vtt" || !slices.Equal(r.SubLangs, []string{"de"}) || r.HideSubs {
		t.Errorf("config: %+v", r)
	}

	// The show's languages win; its unset visibility inherits.
	r = watchOnce(func(h *harness) {
		h.store.SaveShowPrefs(context.Background(), store.ShowPrefs{MediaID: media.ID, SubLanguages: []string{"es", "en"}})
	}, Request{})
	if r.Subtitles[0] != "https://s/es.vtt" || r.Subtitles[1] != "https://s/en.vtt" || r.HideSubs {
		t.Errorf("show prefs: %+v", r)
	}

	// A request override wins over both.
	r = watchOnce(func(h *harness) {
		hidden := false
		h.store.SaveShowPrefs(context.Background(), store.ShowPrefs{MediaID: media.ID, SubLanguages: []string{"es"}, SubShow: &hidden})
	}, Request{Subtitles: &domain.SubtitlePrefs{Languages: []string{"en"}, Show: false}})
	if r.Subtitles[0] != "https://s/en.vtt" || !r.HideSubs {
		t.Errorf("request override: %+v", r)
	}
}
