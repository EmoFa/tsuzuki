// Package skip finds and applies skippable parts of episodes: openings,
// endings and recaps, plus filler and recap episodes.
package skip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/httpx"
)

const (
	DefaultAniSkipURL = "https://api.aniskip.com/v2"
	aniskipTTL        = 7 * 24 * time.Hour
	aniskipMissTTL    = 24 * time.Hour
	// lengthTolerance: times submitted for an episode length this different
	// belong to another cut of the episode and don't line up.
	lengthTolerance = 45 * time.Second
)

// Cache stores small values by key (the store's kv table).
type Cache interface {
	GetKV(ctx context.Context, key string) (value string, updated time.Time, ok bool, err error)
	PutKV(ctx context.Context, key, value string) error
}

// AniSkip looks up community-submitted skip times by MAL ID.
type AniSkip struct {
	Client  *httpx.Client
	Cache   Cache // optional
	BaseURL string
}

type aniskipResponse struct {
	Found   bool `json:"found"`
	Results []struct {
		Interval struct {
			Start float64 `json:"startTime"`
			End   float64 `json:"endTime"`
		} `json:"interval"`
		SkipType      string  `json:"skipType"`
		EpisodeLength float64 `json:"episodeLength"`
	} `json:"results"`
}

type cachedRanges struct {
	Ranges []aniskipRange `json:"ranges"`
}

type aniskipRange struct {
	domain.SkipRange
	Length time.Duration `json:"length"`
}

// Ranges returns skip ranges for an episode that fit an episode of length
// (0 when unknown). No known times is not an error.
func (a *AniSkip) Ranges(ctx context.Context, malID, episode int, length time.Duration) ([]domain.SkipRange, error) {
	if malID == 0 || episode <= 0 {
		return nil, nil
	}
	all, err := a.lookup(ctx, malID, episode)
	if err != nil {
		return nil, err
	}
	var out []domain.SkipRange
	for _, r := range all {
		if length > 0 && r.Length > 0 && absDuration(r.Length-length) > lengthTolerance {
			continue
		}
		out = append(out, r.SkipRange)
	}
	return out, nil
}

func (a *AniSkip) lookup(ctx context.Context, malID, episode int) ([]aniskipRange, error) {
	key := fmt.Sprintf("aniskip:%d:%d", malID, episode)
	if a.Cache != nil {
		if v, updated, ok, err := a.Cache.GetKV(ctx, key); err == nil && ok {
			var c cachedRanges
			ttl := aniskipTTL
			if json.Unmarshal([]byte(v), &c) == nil {
				if len(c.Ranges) == 0 {
					ttl = aniskipMissTTL
				}
				if time.Since(updated) < ttl {
					return c.Ranges, nil
				}
			}
		}
	}

	base := a.BaseURL
	if base == "" {
		base = DefaultAniSkipURL
	}
	q := url.Values{"types": {"op", "ed", "mixed-op", "mixed-ed", "recap"}, "episodeLength": {"0"}}
	u := fmt.Sprintf("%s/skip-times/%d/%d?%s", base, malID, episode, q.Encode())
	var resp aniskipResponse
	err := a.Client.GetJSON(ctx, u, nil, &resp)
	var se *httpx.StatusError
	if errors.As(err, &se) && se.StatusCode == http.StatusNotFound {
		err, resp = nil, aniskipResponse{}
	}
	if err != nil {
		return nil, fmt.Errorf("aniskip: %w", err)
	}

	var ranges []aniskipRange
	for _, r := range resp.Results {
		kind, ok := aniskipKind(r.SkipType)
		if !ok || r.Interval.End <= r.Interval.Start {
			continue
		}
		ranges = append(ranges, aniskipRange{
			SkipRange: domain.SkipRange{Kind: kind, Start: seconds(r.Interval.Start), End: seconds(r.Interval.End)},
			Length:    seconds(r.EpisodeLength),
		})
	}
	if a.Cache != nil {
		if data, err := json.Marshal(cachedRanges{ranges}); err == nil {
			if err := a.Cache.PutKV(ctx, key, string(data)); err != nil {
				slog.Warn("aniskip: caching", "err", err)
			}
		}
	}
	return ranges, nil
}

func aniskipKind(t string) (domain.SkipKind, bool) {
	switch t {
	case "op", "mixed-op":
		return domain.SkipOpening, true
	case "ed", "mixed-ed":
		return domain.SkipEnding, true
	case "recap":
		return domain.SkipRecap, true
	}
	return "", false
}

func seconds(s float64) time.Duration {
	return time.Duration(math.Round(s * float64(time.Second)))
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
