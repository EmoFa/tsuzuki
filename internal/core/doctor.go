package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/provider"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/streamcheck"
)

// CheckStatus is how a diagnostic check went.
type CheckStatus int

const (
	CheckOK CheckStatus = iota
	CheckWarn
	CheckFail
)

// Check is one line of a diagnosis: `tsuzuki doctor`, or the app's
// diagnostics screen.
type Check struct {
	Name   string
	Status CheckStatus
	Detail string
}

// Probe is a check that reaches out over the network. Run returns what it
// found, or an error; a Warning error marks something worth knowing that
// isn't a failure.
type Probe struct {
	Name string
	Run  func(ctx context.Context) (string, error)
}

// Warning is a probe result that isn't a failure.
type Warning string

func (w Warning) Error() string { return string(w) }

// RunProbes runs probes concurrently, each within timeout (at most 20s), and
// returns their checks in order.
func RunProbes(ctx context.Context, timeout time.Duration, probes []Probe) []Check {
	results := make([]Check, len(probes))
	var wg sync.WaitGroup
	for i, p := range probes {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(ctx, min(timeout, 20*time.Second))
			defer cancel()
			start := time.Now()
			detail, err := p.Run(ctx)
			elapsed := time.Since(start).Round(100 * time.Millisecond)
			var w Warning
			switch {
			case errors.As(err, &w):
				results[i] = Check{p.Name, CheckWarn, string(w)}
			case err != nil:
				results[i] = Check{p.Name, CheckFail, fmt.Sprintf("%s · %s", FirstLine(err.Error()), elapsed)}
			default:
				results[i] = Check{p.Name, CheckOK, fmt.Sprintf("%s · %s", detail, elapsed)}
			}
		})
	}
	wg.Wait()
	return results
}

// ServiceProbes reach AniList (verifying the login), AniSkip and the filler list.
func (a *App) ServiceProbes() []Probe {
	return []Probe{
		{"anilist api", func(ctx context.Context) (string, error) {
			al, err := a.AniList(ctx)
			if err != nil {
				return "", err
			}
			if err := al.Ping(ctx); err != nil {
				return "", err
			}
			tok, _ := a.TokenFile().Load()
			if !tok.Valid() {
				return "reachable", nil
			}
			user, err := al.WithToken(tok.AccessToken).Viewer(ctx)
			if errors.Is(err, anilist.ErrUnauthorized) {
				return "", errors.New("reachable, but AniList rejected your login: log in again")
			}
			if err != nil {
				return "", err
			}
			return "reachable, login accepted for " + user.Name, nil
		}},
		{"aniskip", func(ctx context.Context) (string, error) {
			client, err := a.HTTP(ctx)
			if err != nil {
				return "", err
			}
			// Frieren episode 1 has well-established skip times.
			ranges, err := (&skip.AniSkip{Client: client}).Ranges(ctx, 52991, 1, 0)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("reachable (%d skip times for a sample episode)", len(ranges)), nil
		}},
		{"filler list", func(ctx context.Context) (string, error) {
			client, err := a.HTTP(ctx)
			if err != nil {
				return "", err
			}
			naruto := anilist.Media{ID: 1735, Title: anilist.Title{English: "Naruto Shippuden", Romaji: "Naruto: Shippuuden"}}
			kinds, err := (&skip.FillerList{Client: client}).Kinds(ctx, naruto)
			if err != nil {
				return "", err
			}
			if len(kinds) == 0 {
				return "", errors.New("reachable, but a sample show couldn't be read (the site may have changed)")
			}
			return fmt.Sprintf("reachable (%d episodes classified for a sample show)", len(kinds)), nil
		}},
	}
}

// ProviderChecks searches every configured provider for query, concurrently,
// and with streams also resolves and checks a stream from each. They never
// solve bot checks: a provider that needs one is reported as a warning.
func (a *App) ProviderChecks(ctx context.Context, query string, streams bool, timeout time.Duration) []Check {
	st, err := a.Store(ctx)
	if err != nil {
		return []Check{{"providers", CheckFail, err.Error()}}
	}
	// Stored clearances, but no solver.
	client := httpx.New(httpx.Options{Store: st})
	reg := a.NewRegistry(client)
	checker := &streamcheck.Checker{Client: client}

	names := a.Config.ActiveProviders()
	results := make([]Check, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			results[i] = a.providerCheck(ctx, reg, name, query, streams, timeout, checker)
		})
	}
	wg.Wait()
	return results
}

func (a *App) providerCheck(ctx context.Context, reg *provider.Registry, name, query string, streams bool, timeout time.Duration, checker *streamcheck.Checker) Check {
	label := "provider " + name
	p, err := reg.Get(name)
	if err != nil {
		return Check{label, CheckFail, "not implemented"}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r := provider.CheckHealth(ctx, p, provider.HealthOptions{
		Query:       query,
		Mode:        domain.Mode(a.Config.General.Mode),
		Quality:     a.Config.General.Quality,
		Streams:     streams,
		CheckStream: checker.Check,
	})
	elapsed := r.Elapsed.Round(100 * time.Millisecond)
	if r.OK() {
		return Check{label, CheckOK, fmt.Sprintf("%s · %s", r.Detail, elapsed)}
	}

	msg := FirstLine(r.Err.Error())
	var ce *httpx.ChallengeError
	switch {
	case errors.As(r.Err, &ce):
		return Check{label, CheckWarn, fmt.Sprintf("needs %s verification: watch something from %s to verify · %s", ce.Kind, name, elapsed)}
	case r.Limited:
		return Check{label, CheckWarn, fmt.Sprintf("%s failed: %s · %s", r.Stage, msg, elapsed)}
	case errors.Is(r.Err, context.DeadlineExceeded):
		return Check{label, CheckFail, fmt.Sprintf("%s timed out after %s", r.Stage, timeout)}
	}
	return Check{label, CheckFail, fmt.Sprintf("%s failed: %s · %s", r.Stage, msg, elapsed)}
}
