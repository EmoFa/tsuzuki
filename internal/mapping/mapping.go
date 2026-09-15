// Package mapping finds the provider show that corresponds to an AniList entry.
package mapping

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/domain"
	"github.com/EmoFa/anitui/internal/provider"
	"github.com/EmoFa/anitui/internal/store"
)

var ErrNoMatch = errors.New("no matching show")

const (
	// resolveCandidates is how many ranked candidates get their IDs looked up on
	// providers whose search results lack them. Sites leave some entries without
	// IDs, so a few more than the obvious one or two are worth a request each.
	resolveCandidates = 5
	// minResolveRank skips candidates not worth a lookup, such as films when
	// looking for a series.
	minResolveRank = 0.3
	// minTitleScore is the similarity needed to accept a match on title alone.
	minTitleScore = 0.92
	// missTTL avoids repeating a failed search in the same session.
	missTTL = 10 * time.Minute
)

type Store interface {
	Mapping(ctx context.Context, mediaID int, provider string) (*store.Mapping, error)
	SaveMapping(ctx context.Context, m store.Mapping) error
}

type Mapper struct {
	store Store

	mu     sync.Mutex
	misses map[missKey]time.Time
}

type missKey struct {
	mediaID  int
	provider string
	mode     domain.Mode
}

func New(st Store) *Mapper {
	return &Mapper{store: st, misses: map[missKey]time.Time{}}
}

// Resolve returns the provider's show for media, from the saved mapping when
// there is one. It returns ErrNoMatch when the provider doesn't have the show.
func (m *Mapper) Resolve(ctx context.Context, p provider.Provider, media anilist.Media, mode domain.Mode) (domain.Show, error) {
	saved, err := m.store.Mapping(ctx, media.ID, p.Name())
	if err != nil {
		return domain.Show{}, err
	}
	if saved != nil {
		return domain.Show{Provider: p.Name(), ID: saved.ShowID, Title: saved.ShowTitle}, nil
	}

	key := missKey{media.ID, p.Name(), mode}
	m.mu.Lock()
	missedAt, missed := m.misses[key]
	m.mu.Unlock()
	if missed && time.Since(missedAt) < missTTL {
		return domain.Show{}, fmt.Errorf("%s: %w", p.Name(), ErrNoMatch)
	}

	show, err := m.match(ctx, p, media, mode)
	if errors.Is(err, ErrNoMatch) {
		m.mu.Lock()
		m.misses[key] = time.Now()
		m.mu.Unlock()
	}
	if err != nil {
		return domain.Show{}, fmt.Errorf("%s: %w", p.Name(), err)
	}
	slog.Info("mapped show", "media", media.ID, "provider", p.Name(), "show", show.ID, "title", show.Title)
	if err := m.store.SaveMapping(ctx, store.Mapping{MediaID: media.ID, Provider: p.Name(), ShowID: show.ID, ShowTitle: show.Title}); err != nil {
		slog.Warn("saving mapping", "err", err)
	}
	return show, nil
}

type candidate struct {
	show     domain.Show
	score    float64 // title similarity
	rank     float64 // score adjusted for episode count, year and format
	rejected bool    // IDs prove it's a different show
}

func (m *Mapper) match(ctx context.Context, p provider.Provider, media anilist.Media, mode domain.Mode) (domain.Show, error) {
	var cands []*candidate
	seen := map[string]bool{}
	var searchErr error
	searched := 0

	for _, q := range searchQueries(media) {
		shows, err := p.Search(ctx, q, mode)
		if err != nil {
			searchErr = err
			continue
		}
		searched++
		for _, s := range shows {
			if seen[s.ID] {
				continue
			}
			seen[s.ID] = true
			c := &candidate{show: s, score: titleScore(media, s)}
			c.rank = rank(media, s, c.score)
			switch idMatch(media, s) {
			case matchYes:
				return s, nil
			case matchNo:
				c.rejected = true
			}
			cands = append(cands, c)
		}
	}
	if searched == 0 && searchErr != nil {
		return domain.Show{}, searchErr
	}

	// Verify the most plausible candidates first. Title alone isn't enough: a
	// series' synonyms often name its arcs, which are also films ("JoJo's
	// Bizarre Adventure: Phantom Blood"), and those would crowd out the series.
	slices.SortStableFunc(cands, func(a, b *candidate) int { return cmp.Compare(b.rank, a.rank) })

	if r, ok := p.(provider.IDResolver); ok {
		tried := 0
		for _, c := range cands {
			if tried == resolveCandidates {
				break
			}
			if c.rejected || c.rank < minResolveRank {
				continue
			}
			tried++
			al, mal, err := r.ExternalIDs(ctx, c.show.ID)
			if err != nil {
				slog.Warn("resolving show IDs", "provider", p.Name(), "show", c.show.ID, "err", err)
				continue
			}
			c.show.AniListID, c.show.MalID = al, mal
			switch idMatch(media, c.show) {
			case matchYes:
				return c.show, nil
			case matchNo:
				c.rejected = true
			}
		}
	}

	// Last resort: an unambiguous, near-exact title match without contrary IDs.
	var viable []*candidate
	for _, c := range cands {
		if !c.rejected && compatible(media, c.show) {
			viable = append(viable, c)
		}
	}
	slices.SortStableFunc(viable, func(a, b *candidate) int { return cmp.Compare(b.score, a.score) })
	if len(viable) > 0 && viable[0].score >= minTitleScore &&
		(len(viable) == 1 || viable[1].score < viable[0].score-0.05) {
		return viable[0].show, nil
	}
	return domain.Show{}, ErrNoMatch
}

type idResult int

const (
	matchUnknown idResult = iota
	matchYes
	matchNo
)

func idMatch(media anilist.Media, s domain.Show) idResult {
	if s.AniListID != 0 {
		if s.AniListID == media.ID {
			return matchYes
		}
		return matchNo
	}
	if s.MalID != 0 && media.IDMal != 0 {
		if s.MalID == media.IDMal {
			return matchYes
		}
		return matchNo
	}
	return matchUnknown
}

// rank orders candidates for ID verification: title similarity, demoted when
// the episode count, year or format contradicts the AniList entry.
func rank(media anilist.Media, s domain.Show, score float64) float64 {
	r := score
	if !compatible(media, s) {
		r -= 0.5
	}
	switch formatMatch(media.Format, s.Type) {
	case matchYes:
		r += 0.05
	case matchNo:
		r -= 0.3
	}
	return r
}

// formatMatch compares an AniList format (TV, TV_SHORT, MOVIE, OVA, ONA,
// SPECIAL, MUSIC) with a provider's type label. Sites disagree on TV vs ONA and
// OVA vs special, so only a film against anything else counts as a mismatch.
func formatMatch(format, typ string) idResult {
	f := strings.ToUpper(format)
	t := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(typ), " ", "_"))
	if f == "" || t == "" {
		return matchUnknown
	}
	if f == t || (f == "TV_SHORT" && t == "TV") {
		return matchYes
	}
	if (f == "MOVIE") != (t == "MOVIE") {
		return matchNo
	}
	return matchUnknown
}

// compatible rejects title matches whose episode count, year or format clearly differ.
func compatible(media anilist.Media, s domain.Show) bool {
	if formatMatch(media.Format, s.Type) == matchNo {
		return false
	}
	if aired := media.AiredEpisodes(); aired > 0 && s.Episodes > 0 {
		diff := aired - s.Episodes
		if diff < 0 {
			diff = -diff
		}
		if diff > max(1, aired/10) {
			return false
		}
	}
	if media.Year > 0 && s.Year > 0 && (media.Year-s.Year > 1 || s.Year-media.Year > 1) {
		return false
	}
	return true
}

// searchQueries returns the titles to search with, most useful first.
func searchQueries(media anilist.Media) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range []string{media.Title.English, media.Title.Romaji} {
		q := strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`).Replace(strings.TrimSpace(t))
		if q != "" && !seen[normalize(q)] {
			seen[normalize(q)] = true
			out = append(out, q)
		}
	}
	return out
}

// titleScore is the best similarity between any of the media's titles and the
// show's titles, from 0 to 1.
func titleScore(media anilist.Media, s domain.Show) float64 {
	best := 0.0
	for _, a := range media.Titles() {
		na := normalize(a)
		for _, b := range append([]string{s.Title}, s.AltTitles...) {
			if sc := similarity(na, normalize(b)); sc > best {
				best = sc
			}
		}
	}
	return best
}

var (
	// qualifier matches disambiguators AniList adds to titles: "(TV)", "(2012)".
	qualifier     = regexp.MustCompile(`(?i)\s*\((?:tv|\d{4})\)`)
	ordinalSeason = regexp.MustCompile(`\b(\d+)(?:st|nd|rd|th) season\b`)
	wordSeason    = regexp.MustCompile(`\b(first|second|third|fourth|fifth) season\b`)
	seasonWords   = map[string]string{"first": "1", "second": "2", "third": "3", "fourth": "4", "fifth": "5"}
)

// NormalizeTitle lowercases, drops punctuation, apostrophes and "(TV)"/"(2012)"
// qualifiers, and writes season numbers one way ("2nd Season" and "Season 2"
// both become "season 2").
func NormalizeTitle(s string) string { return normalize(s) }

func normalize(s string) string {
	s = qualifier.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = strings.NewReplacer("’", "", "'", "", "&", " and ").Replace(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = ordinalSeason.ReplaceAllString(s, "season $1")
	s = wordSeason.ReplaceAllStringFunc(s, func(m string) string {
		return "season " + seasonWords[strings.Fields(m)[0]]
	})
	return s
}

// similarity is 1 for equal strings, otherwise the Dice coefficient of their
// word sets.
func similarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	ta, tb := strings.Fields(a), strings.Fields(b)
	setB := map[string]bool{}
	for _, w := range tb {
		setB[w] = true
	}
	common, seen := 0, map[string]bool{}
	for _, w := range ta {
		if setB[w] && !seen[w] {
			common++
			seen[w] = true
		}
	}
	return 2 * float64(common) / float64(len(uniq(ta))+len(setB))
}

func uniq(ws []string) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		m[w] = true
	}
	return m
}
