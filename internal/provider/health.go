package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/EmoFa/tsuzuki/internal/domain"
)

// HealthOptions configures CheckHealth.
type HealthOptions struct {
	Query   string
	Mode    domain.Mode
	Quality string
	// Streams also resolves the first result's first episode and checks the stream.
	Streams bool
	// CheckStream verifies a stream serves media; required when Streams is set.
	CheckStream func(ctx context.Context, s domain.Stream) error
}

// HealthReport describes how far a provider got.
type HealthReport struct {
	Provider string
	Stage    string // the stage that failed, or the last one completed
	Detail   string
	Err      error
	// Limited marks a known, provider-wide limitation (ErrStreamsUnavailable)
	// rather than a breakage.
	Limited bool
	Elapsed time.Duration
}

func (r HealthReport) OK() bool { return r.Err == nil }

// CheckHealth exercises a provider the way a watch would.
func CheckHealth(ctx context.Context, p Provider, opts HealthOptions) (r HealthReport) {
	start := time.Now()
	r.Provider = p.Name()
	defer func() { r.Elapsed = time.Since(start) }()
	fail := func(stage string, err error) HealthReport {
		r.Stage, r.Err = stage, err
		r.Limited = errors.Is(err, ErrStreamsUnavailable)
		return r
	}

	shows, err := p.Search(ctx, opts.Query, opts.Mode)
	if err != nil {
		return fail("search", err)
	}
	if len(shows) == 0 {
		return fail("search", fmt.Errorf("no results for %q", opts.Query))
	}
	r.Stage, r.Detail = "search", fmt.Sprintf("%d result%s", len(shows), map[bool]string{true: "", false: "s"}[len(shows) == 1])
	if !opts.Streams {
		return r
	}

	show := shows[0]
	eps, err := p.Episodes(ctx, show.ID, opts.Mode)
	if err != nil {
		return fail("episodes", fmt.Errorf("%s: %w", show.Title, err))
	}
	if len(eps) == 0 {
		return fail("episodes", fmt.Errorf("%s lists no episodes", show.Title))
	}
	streams, err := p.Streams(ctx, show.ID, eps[0], opts.Mode)
	if err != nil {
		return fail("streams", fmt.Errorf("%s episode %s: %w", show.Title, eps[0].Label(), err))
	}
	stream, err := domain.SelectStream(streams, opts.Quality)
	if err != nil {
		return fail("streams", err)
	}
	if err := opts.CheckStream(ctx, stream); err != nil {
		return fail("playback", fmt.Errorf("%s: %w", stream.Label, err))
	}
	r.Stage = "playback"
	r.Detail = fmt.Sprintf("%s episode %s plays (%s)", show.Title, eps[0].Label(), stream.Label)
	return r
}
