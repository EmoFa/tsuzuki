// Package session orchestrates watching: it finds an episode on the configured
// providers, plays it, records progress, and moves on to the next episode.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/player"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/store"
)

const (
	// saveInterval is how often progress is written while playing.
	saveInterval = 5 * time.Second
	// minResume: positions earlier than this start the episode from the top.
	minResume = 30 * time.Second
)

var ErrUnavailable = errors.New("episode not available from any provider")

// errPlaybackFailed marks a stream that resolved but didn't play through; the
// episode is retried on the next provider.
var errPlaybackFailed = errors.New("playback failed")

const (
	// maxStreamChecks bounds how many of a provider's streams are health-checked.
	maxStreamChecks = 3
	// prematureEndMargin: a stream ending more than this before its duration was
	// cut off rather than finished.
	prematureEndMargin = 90 * time.Second
)

type Settings struct {
	ProviderOrder    []string
	Quality          string
	AutoplayNext     bool
	WatchedThreshold float64 // fraction of duration that counts as watched
	ResumeRewind     time.Duration
	CheckTimeout     time.Duration // per stream health check; default 10s
}

// Resolver maps AniList entries to provider shows (mapping.Mapper).
type Resolver interface {
	Resolve(ctx context.Context, p provider.Provider, media anilist.Media, mode domain.Mode) (domain.Show, error)
}

type ProgressStore interface {
	SaveProgress(ctx context.Context, p store.Progress) error
	EpisodeProgress(ctx context.Context, mediaID int, episode float64) (*store.Progress, error)
	ShowProgress(ctx context.Context, mediaID int) ([]store.Progress, error)
	ListEntry(ctx context.Context, mediaID int) (*store.ListEntry, error)
}

// Playback is the part of *player.Playback the session uses.
type Playback interface {
	Events() <-chan player.Event
	State() player.State
	Wait() error
	Close() error
}

// Proxy is the part of *streamproxy.Proxy the session uses.
type Proxy interface {
	Stream(s domain.Stream) string
	URL(upstream string, headers map[string]string) string
}

type Deps struct {
	Settings  Settings
	Providers *provider.Registry
	Resolver  Resolver
	Progress  ProgressStore
	Play      func(ctx context.Context, req player.Request) (Playback, error)
	Proxy     func() (Proxy, error) // started lazily, only for streams that need it
	// Check verifies a stream serves media before it is played. Nil skips checks.
	Check func(ctx context.Context, s domain.Stream) error
	// OnWatched records a watched episode on the user's list (the tracker),
	// returning a note for the UI. Nil disables tracking.
	OnWatched func(ctx context.Context, media anilist.Media, episode float64) (string, error)
	// OnStatus receives updates for the UI. May be nil.
	OnStatus func(Status)
}

type StatusKind int

const (
	StatusResolving      StatusKind = iota + 1 // looking for Episode
	StatusProviderFailed                       // Provider couldn't supply Episode; Err says why
	StatusPlaying                              // playback started; Stream, Start set
	StatusProgress                             // Position/Duration updated
	StatusWatched                              // Episode crossed the watched threshold
	StatusTracked                              // the list was updated for Episode; Reason is a note, Err a failure
	StatusStopped                              // playback ended; Reason set
	StatusNoNextEpisode                        // autoplay found nothing after Episode
)

type Status struct {
	Kind     StatusKind
	Media    anilist.Media
	Episode  float64
	Provider string
	Stream   domain.Stream
	Start    time.Duration
	Position time.Duration
	Duration time.Duration
	Reason   string // StatusStopped: mpv end reason ("eof", "quit", "error", ...)
	Err      error
}

type Session struct {
	Deps

	mu       sync.Mutex
	episodes map[string][]domain.Episode // provider|show|mode
}

func New(d Deps) *Session {
	return &Session{Deps: d, episodes: map[string][]domain.Episode{}}
}

// Request says what to watch. Episode 0 means continue where the user left off.
type Request struct {
	Media   anilist.Media
	Episode float64
	Mode    domain.Mode
	// Provider, if set, is tried before the configured order. When empty, the
	// provider that last served this show is preferred.
	Provider string
	// SkipProviders are not tried for the first episode (e.g. the user asked
	// for a different source).
	SkipProviders []string
}

// Watch plays req and, with autoplay, the following episodes until the user
// stops, an episode can't be found, or ctx ends.
func (s *Session) Watch(ctx context.Context, req Request) error {
	ep := req.Episode
	if ep == 0 {
		var err error
		if ep, err = NextToWatch(ctx, s.Progress, req.Media.ID); err != nil {
			return err
		}
	}
	prefer := req.Provider
	if prefer == "" {
		prefer = s.lastProvider(ctx, req.Media.ID)
	}
	skip := slices.Clone(req.SkipProviders)
	// failures remembers why providers were abandoned mid-episode, so the final
	// error explains them.
	var failures []error

	for {
		res, err := s.resolve(ctx, req.Media, ep, req.Mode, prefer, skip)
		if err != nil {
			return errors.Join(append([]error{err}, failures...)...)
		}
		state, err := s.play(ctx, req.Media, res, req.Mode)
		if errors.Is(err, errPlaybackFailed) && ctx.Err() == nil {
			slog.Info("playback failed; trying next provider", "provider", res.provider, "episode", ep, "err", err)
			s.status(Status{Kind: StatusProviderFailed, Media: req.Media, Episode: ep, Provider: res.provider, Err: err})
			failures = append(failures, err)
			skip, prefer = append(skip, res.provider), ""
			continue // same episode; play resumes from the saved position
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil || state.EndReason != "eof" || !s.Settings.AutoplayNext {
			return nil
		}
		next, ok := nextEpisode(res.episodes, ep)
		if !ok {
			s.status(Status{Kind: StatusNoNextEpisode, Media: req.Media, Episode: ep, Provider: res.provider})
			return nil
		}
		ep, prefer, skip, failures = next.Number, res.provider, nil, nil
	}
}

// lastProvider is the provider that most recently served the show, if any.
func (s *Session) lastProvider(ctx context.Context, mediaID int) string {
	eps, err := s.Progress.ShowProgress(ctx, mediaID)
	if err != nil || len(eps) == 0 {
		return ""
	}
	return slices.MaxFunc(eps, func(a, b store.Progress) int { return a.UpdatedAt.Compare(b.UpdatedAt) }).Provider
}

// NextToWatch returns the episode to continue with: the most recently watched
// one if unfinished, otherwise the one after it (episode 1 for new shows). A
// list entry further along (e.g. synced from AniList, watched elsewhere) wins.
func NextToWatch(ctx context.Context, progress ProgressStore, mediaID int) (float64, error) {
	eps, err := progress.ShowProgress(ctx, mediaID)
	if err != nil {
		return 0, err
	}
	next := 1.0
	if len(eps) > 0 {
		latest := slices.MaxFunc(eps, func(a, b store.Progress) int { return a.UpdatedAt.Compare(b.UpdatedAt) })
		next = latest.Episode
		if latest.Completed {
			next = math.Floor(latest.Episode) + 1
		}
	}
	entry, err := progress.ListEntry(ctx, mediaID)
	if err != nil {
		return 0, err
	}
	if entry != nil && float64(entry.Progress+1) > next {
		next = float64(entry.Progress + 1)
	}
	return next, nil
}

type resolved struct {
	provider string
	show     domain.Show
	episodes []domain.Episode
	episode  domain.Episode
	stream   domain.Stream
}

// resolve tries providers in order (prefer first) until one has a playable stream.
func (s *Session) resolve(ctx context.Context, media anilist.Media, number float64, mode domain.Mode, prefer string, skip []string) (resolved, error) {
	order := slices.DeleteFunc(slices.Clone(s.Settings.ProviderOrder), func(n string) bool { return slices.Contains(skip, n) })
	if slices.Contains(skip, prefer) {
		prefer = ""
	}
	if prefer != "" {
		order = append([]string{prefer}, slices.DeleteFunc(slices.Clone(order), func(n string) bool { return n == prefer })...)
	}

	var errs []error
	for _, name := range order {
		if err := ctx.Err(); err != nil {
			return resolved{}, err
		}
		p, err := s.Providers.Get(name)
		if err != nil {
			continue // configured but not implemented
		}
		s.status(Status{Kind: StatusResolving, Media: media, Episode: number, Provider: name})
		res, err := s.resolveOn(ctx, p, media, number, mode)
		if err == nil {
			return res, nil
		}
		if ctx.Err() != nil {
			return resolved{}, ctx.Err() // interrupted, not a provider failure
		}
		slog.Info("provider could not supply episode", "provider", name, "media", media.ID, "episode", number, "err", err)
		s.status(Status{Kind: StatusProviderFailed, Media: media, Episode: number, Provider: name, Err: err})
		errs = append(errs, err)
	}
	return resolved{}, fmt.Errorf("%s episode %v: %w", media.DisplayTitle(), number, errors.Join(append([]error{ErrUnavailable}, errs...)...))
}

func (s *Session) resolveOn(ctx context.Context, p provider.Provider, media anilist.Media, number float64, mode domain.Mode) (resolved, error) {
	show, err := s.Resolver.Resolve(ctx, p, media, mode)
	if err != nil {
		return resolved{}, err
	}
	eps, err := s.episodeList(ctx, p, show.ID, mode, number)
	if err != nil {
		return resolved{}, err
	}
	ep, err := provider.FindEpisode(eps, number)
	if err != nil {
		return resolved{}, err
	}
	streams, err := p.Streams(ctx, show.ID, ep, mode)
	if err != nil {
		return resolved{}, err
	}
	stream, err := s.pickStream(ctx, streams)
	if err != nil {
		return resolved{}, err
	}
	return resolved{provider: p.Name(), show: show, episodes: eps, episode: ep, stream: stream}, nil
}

// episodeList caches episode lists for the session, refetching when the wanted
// episode is missing (it may have aired since).
func (s *Session) episodeList(ctx context.Context, p provider.Provider, showID string, mode domain.Mode, want float64) ([]domain.Episode, error) {
	key := p.Name() + "|" + showID + "|" + string(mode)
	s.mu.Lock()
	eps, ok := s.episodes[key]
	s.mu.Unlock()
	if ok {
		if _, err := provider.FindEpisode(eps, want); err == nil {
			return eps, nil
		}
	}
	eps, err := p.Episodes(ctx, showID, mode)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.episodes[key] = eps
	s.mu.Unlock()
	return eps, nil
}

// pickStream returns the stream closest to the configured quality that passes
// a health check, trying up to maxStreamChecks candidates.
func (s *Session) pickStream(ctx context.Context, streams []domain.Stream) (domain.Stream, error) {
	remaining := slices.Clone(streams)
	var errs []error
	for range maxStreamChecks {
		if len(remaining) == 0 {
			break
		}
		cand, err := domain.SelectStream(remaining, s.Settings.Quality)
		if err != nil {
			return domain.Stream{}, err
		}
		if s.Check == nil {
			return cand, nil
		}
		checkCtx, cancel := context.WithTimeout(ctx, durationOr(s.Settings.CheckTimeout, 10*time.Second))
		err = s.Check(checkCtx, cand)
		cancel()
		if err == nil {
			return cand, nil
		}
		if ctx.Err() != nil {
			return domain.Stream{}, ctx.Err()
		}
		slog.Info("stream failed health check", "stream", cand.Label, "err", err)
		errs = append(errs, fmt.Errorf("%s: %w", cand.Label, err))
		remaining = slices.DeleteFunc(remaining, func(st domain.Stream) bool {
			return st.URL == cand.URL && st.VariantHeight == cand.VariantHeight
		})
	}
	return domain.Stream{}, fmt.Errorf("no stream passed the health check: %w", errors.Join(errs...))
}

func durationOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}

func (s *Session) play(ctx context.Context, media anilist.Media, res resolved, mode domain.Mode) (player.State, error) {
	var proxy Proxy
	if res.stream.NeedsProxy {
		var err error
		if proxy, err = s.Proxy(); err != nil {
			return player.State{}, err
		}
	}
	req, err := PlayerRequest(res.stream, proxy)
	if err != nil {
		return player.State{}, err
	}
	req.Title = fmt.Sprintf("%s - Episode %s", media.DisplayTitle(), res.episode.Label())

	prev, err := s.Progress.EpisodeProgress(ctx, media.ID, res.episode.Number)
	if err != nil {
		return player.State{}, err
	}
	if prev != nil && !prev.Completed && prev.Position >= minResume {
		req.Start = max(prev.Position-s.Settings.ResumeRewind, 0)
	}

	pb, err := s.Play(ctx, req)
	if err != nil {
		return player.State{}, err
	}
	stopped := make(chan struct{})
	defer close(stopped)
	defer pb.Close()
	go func() {
		select {
		case <-ctx.Done():
			pb.Close()
		case <-stopped:
		}
	}()

	base := Status{Media: media, Episode: res.episode.Number, Provider: res.provider, Stream: res.stream}
	started := base
	started.Kind, started.Start = StatusPlaying, req.Start
	s.status(started)

	var lastSave time.Time
	watched := prev != nil && prev.Completed
	record := func(st player.State, final bool) {
		if st.Duration <= 0 && st.EndReason != "eof" {
			return // never really started
		}
		done := (st.EndReason == "eof" && !prematureEnd(st)) ||
			(st.Duration > 0 && st.Position.Seconds() >= s.Settings.WatchedThreshold*st.Duration.Seconds())
		if done && !watched {
			watched = true
			w := base
			w.Kind, w.Position, w.Duration = StatusWatched, st.Position, st.Duration
			s.status(w)
			if s.OnWatched != nil {
				// Survives an interrupt: quitting right after an episode still tracks it.
				tctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
				note, err := s.OnWatched(tctx, media, res.episode.Number)
				cancel()
				tr := base
				tr.Kind, tr.Reason, tr.Err = StatusTracked, note, err
				s.status(tr)
			}
		}
		p := store.Progress{
			MediaID: media.ID, Episode: res.episode.Number,
			Position: st.Position, Duration: st.Duration, Completed: done,
			Provider: res.provider, Mode: string(mode),
		}
		// Use a fresh context for the final save so an interrupt still records it.
		saveCtx := ctx
		if final {
			var cancel context.CancelFunc
			saveCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
		}
		if err := s.Progress.SaveProgress(saveCtx, p); err != nil {
			slog.Warn("saving progress", "err", err)
		}
		lastSave = time.Now()
	}

	for e := range pb.Events() {
		switch e.Kind {
		case player.EventPosition:
			u := base
			u.Kind, u.Position, u.Duration = StatusProgress, e.Position, e.Duration
			s.status(u)
			if time.Since(lastSave) >= saveInterval {
				record(pb.State(), false)
			}
		case player.EventPause, player.EventSeek:
			record(pb.State(), false)
		}
	}
	waitErr := pb.Wait()
	st := pb.State()
	record(st, true)

	end := base
	end.Kind, end.Reason, end.Position, end.Duration = StatusStopped, st.EndReason, st.Position, st.Duration
	s.status(end)

	switch {
	case ctx.Err() != nil:
	case st.EndReason == "error" || (st.EndReason == "" && waitErr != nil):
		return st, fmt.Errorf("episode %s from %s: %w: %v", res.episode.Label(), res.provider, errPlaybackFailed, playbackErr(waitErr))
	case prematureEnd(st):
		return st, fmt.Errorf("episode %s from %s: %w: stream ended at %s of %s", res.episode.Label(), res.provider,
			errPlaybackFailed, st.Position.Round(time.Second), st.Duration.Round(time.Second))
	}
	return st, nil
}

// prematureEnd reports a stream that hit end-of-file well before its duration,
// which is how a dropped connection looks to mpv.
func prematureEnd(st player.State) bool {
	return st.EndReason == "eof" && st.Duration > 2*prematureEndMargin && st.Position < st.Duration-prematureEndMargin
}

func playbackErr(err error) error {
	if err == nil {
		return errors.New("mpv could not play the stream")
	}
	return err
}

func (s *Session) status(st Status) {
	if s.OnStatus != nil {
		s.OnStatus(st)
	}
}

// nextEpisode returns the lowest-numbered episode after number.
func nextEpisode(eps []domain.Episode, number float64) (domain.Episode, bool) {
	var best domain.Episode
	found := false
	for _, e := range eps {
		if e.Number > number && (!found || e.Number < best.Number) {
			best, found = e, true
		}
	}
	return best, found
}

// PlayerRequest turns a stream into what mpv needs, routing it through proxy
// when required.
func PlayerRequest(s domain.Stream, proxy Proxy) (player.Request, error) {
	req := player.Request{URL: s.URL, Headers: s.Headers, AudioLang: s.AudioLang}
	subURL := func(u string) string { return u }
	if s.NeedsProxy {
		if proxy == nil {
			return req, errors.New("stream requires the stream proxy")
		}
		req.URL = proxy.Stream(s)
		req.Headers = nil // the proxy adds them
		subURL = func(u string) string { return proxy.URL(u, s.Headers) }
	}
	for _, sub := range s.Subtitles {
		req.Subtitles = append(req.Subtitles, subURL(sub.URL))
	}
	return req, nil
}
