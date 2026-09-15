// Package provider defines the interface every stream source implements.
package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/EmoFa/tsuzuki/internal/domain"
)

type Provider interface {
	Name() string
	// Search finds shows matching query that have episodes in mode.
	Search(ctx context.Context, query string, mode domain.Mode) ([]domain.Show, error)
	// Episodes lists a show's episodes available in mode, sorted by Number.
	Episodes(ctx context.Context, showID string, mode domain.Mode) ([]domain.Episode, error)
	// Streams resolves playable streams for one episode in mode.
	Streams(ctx context.Context, showID string, ep domain.Episode, mode domain.Mode) ([]domain.Stream, error)
}

// IDResolver is implemented by providers that can map a show to AniList/MAL
// IDs when search results don't already include them.
type IDResolver interface {
	ExternalIDs(ctx context.Context, showID string) (anilistID, malID int, err error)
}

var (
	// ErrNotFound means the show or episode doesn't exist on this provider.
	ErrNotFound = errors.New("not found")
	// ErrStreamsUnavailable means the provider is reachable but currently cannot
	// resolve streams at all (e.g. a protection change it doesn't support yet).
	ErrStreamsUnavailable = errors.New("streams unavailable from this provider")
	// ErrNoStreams means the episode exists but has nothing playable in the mode.
	ErrNoStreams = errors.New("no streams for this episode")
)

// Registry holds providers by name.
type Registry struct {
	byName map[string]Provider
	order  []string
}

func NewRegistry(ps ...Provider) *Registry {
	r := &Registry{byName: map[string]Provider{}}
	for _, p := range ps {
		r.byName[p.Name()] = p
		r.order = append(r.order, p.Name())
	}
	return r
}

func (r *Registry) Get(name string) (Provider, error) {
	p, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("unknown or unavailable provider %q (available: %v)", name, r.Names())
	}
	return p, nil
}

func (r *Registry) Names() []string { return slices.Clone(r.order) }

// FindEpisode returns the episode with the given canonical number.
func FindEpisode(eps []domain.Episode, number float64) (domain.Episode, error) {
	for _, e := range eps {
		if e.Number == number {
			return e, nil
		}
	}
	return domain.Episode{}, fmt.Errorf("episode %v: %w", number, ErrNotFound)
}
