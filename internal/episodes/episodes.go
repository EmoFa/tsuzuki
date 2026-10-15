// Package episodes works out a show's episode list as tsuzuki's interfaces
// present it: which episodes there are, which have been watched, and which one
// is next.
package episodes

import (
	"math"
	"slices"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/domain"
	"github.com/EmoFa/tsuzuki/internal/skip"
	"github.com/EmoFa/tsuzuki/internal/store"
	"github.com/EmoFa/tsuzuki/internal/tracker"
)

// Show is what's known about a show's episodes. Any part may be missing while
// it loads.
type Show struct {
	Media anilist.Media
	// Progress is the current round's history, by episode.
	Progress map[float64]store.Progress
	// Provider lists episodes from a provider, for titles and filler flags,
	// and for shows whose aired count AniList doesn't know.
	Provider map[float64]domain.Episode
	// Listed is AniList's titles and thumbnails, from streaming sites.
	Listed map[float64]anilist.EpisodeInfo
	Entry  *store.ListEntry
	Kinds  map[int]skip.EpisodeKind
	// Round is the watch-through, from 1.
	Round int
	// BeforeThrough is how far the show had been watched when this round started.
	BeforeThrough int
	// WatchedBefore holds episodes completed in earlier rounds.
	WatchedBefore map[float64]bool
}

// NeedsProviderEpisodes reports that AniList doesn't say how many episodes
// have aired, so the list has to come from a provider.
func NeedsProviderEpisodes(m anilist.Media) bool {
	return m.AiredEpisodes() == 0 && !m.NotYetAired()
}

// WantsProviderEpisodes reports that the provider's list is worth fetching: the
// aired count is unknown, or the show is still airing and AniList can lag
// behind what providers have.
func WantsProviderEpisodes(m anilist.Media) bool {
	return NeedsProviderEpisodes(m) || (m.Status == "RELEASING" && !m.NotYetAired())
}

// Numbers lists the episodes to show: AniList's aired count, the provider's
// list, and any watched episodes beyond both, in order.
func (s *Show) Numbers() []float64 {
	seen := map[float64]bool{}
	var nums []float64
	add := func(n float64) {
		if !seen[n] {
			seen[n] = true
			nums = append(nums, n)
		}
	}
	for i := 1; i <= s.Media.AiredEpisodes(); i++ {
		add(float64(i))
	}
	for n := range s.Provider {
		add(n)
	}
	for n := range s.Progress {
		add(n)
	}
	// AniList's streaming list can run ahead of its aired count, but may also
	// number a later season from where the last one ended, so only take
	// episodes that continue on from the ones already known.
	if len(s.Listed) > 0 {
		top := 0
		for _, n := range nums {
			if n == math.Trunc(n) {
				top = max(top, int(n))
			}
		}
		for {
			if _, ok := s.Listed[float64(top+1)]; !ok {
				break
			}
			top++
			add(float64(top))
		}
	}
	slices.Sort(nums)
	return nums
}

// ListThrough is how far the user's list says they've watched. Episodes
// watched elsewhere show as watched here without inventing any history.
func (s *Show) ListThrough() float64 {
	return float64(tracker.ListThrough(s.Entry, s.Media.Episodes, s.Round))
}

// Watched reports whether an episode shows as watched in this round, played
// here or according to the list.
func (s *Show) Watched(n float64) bool {
	if p, ok := s.Progress[n]; ok && p.Completed {
		return true
	}
	return n <= s.ListThrough()
}

// Started reports whether the show has been begun at all, here or on the
// user's list, or is being rewatched: only then is an episode "next up".
func (s *Show) Started() bool {
	return len(s.Progress) > 0 || s.Round > 1 || s.ListThrough() > 0
}

// WatchedEarlier reports an episode watched before this rewatch started: here
// in an earlier round, or as far as the show had been watched at the time.
func (s *Show) WatchedEarlier(n float64) bool {
	return s.Round > 1 && (s.WatchedBefore[n] || n <= float64(s.BeforeThrough))
}

// NextUp is the episode "continue" plays: after the most recently watched one,
// or after what the user's list has reached, whichever is further.
func (s *Show) NextUp() float64 {
	var latest *store.Progress
	for _, p := range s.Progress {
		if latest == nil || p.UpdatedAt.After(latest.UpdatedAt) {
			latest = &p
		}
	}
	next := 1.0
	switch {
	case latest != nil:
		next = NextEpisode(*latest)
	default:
		if nums := s.Numbers(); len(nums) > 0 {
			next = nums[0]
		}
	}
	if fromList := s.ListThrough() + 1; fromList > next {
		next = fromList
	}
	return next
}

// NextEpisode is the episode to continue with after p: p itself if it's
// unfinished, otherwise the one after.
func NextEpisode(p store.Progress) float64 {
	if !p.Completed {
		return p.Episode
	}
	return math.Floor(p.Episode) + 1
}

// State is how an episode stands, for its marker.
type State string

const (
	Unwatched State = ""
	// Done was played to the end here.
	Done State = "watched"
	// Partial was started here; Position and Duration say how far.
	Partial State = "partial"
	// Next is what "continue" plays.
	Next State = "next"
	// OnList is watched according to the user's list, though not played here.
	OnList State = "list"
	// Earlier was watched before this rewatch.
	Earlier State = "earlier"
)

// Row is one episode as a list shows it.
type Row struct {
	Number float64
	// Title is the episode's title, from the provider or else AniList.
	Title string
	// Thumbnail is a still from the episode, when AniList has one.
	Thumbnail string
	State     State
	Position  time.Duration // Partial only
	Duration  time.Duration // Partial only
	// Kind is "filler", "mixed" (canon and filler) or "recap", or empty.
	Kind string
}

// Row describes episode n. next is NextUp(), passed in so a list computes it
// once; progressKnown says whether Progress has loaded, since until then no
// episode can be called next.
func (s *Show) Row(n, next float64, progressKnown bool) Row {
	r := Row{Number: n}
	e, fromProvider := s.Provider[n]
	if fromProvider {
		r.Title = e.Title
	}
	if l, ok := s.Listed[n]; ok {
		r.Thumbnail = l.Thumbnail
		if r.Title == "" {
			r.Title = l.Title
		}
	}
	if r.Thumbnail == "" && fromProvider {
		r.Thumbnail = e.Thumbnail
	}
	p, played := s.Progress[n]
	switch {
	case played && p.Completed:
		r.State = Done
	case played && p.Duration > 0:
		r.State, r.Position, r.Duration = Partial, p.Position, p.Duration
	case n == next && progressKnown && s.Started():
		r.State = Next
	case n <= s.ListThrough():
		r.State = OnList
	case s.WatchedEarlier(n):
		r.State = Earlier
	}
	switch kind := s.Kinds[int(n)]; {
	case kind == skip.Filler || (fromProvider && e.Filler):
		r.Kind = "filler"
	case kind == skip.Mixed:
		r.Kind = "mixed"
	case fromProvider && e.Recap:
		r.Kind = "recap"
	}
	return r
}

// Rows describes every episode, in order.
func (s *Show) Rows(progressKnown bool) []Row {
	next := s.NextUp()
	nums := s.Numbers()
	rows := make([]Row, len(nums))
	for i, n := range nums {
		rows[i] = s.Row(n, next, progressKnown)
	}
	return rows
}

// ContinueWatching picks the shows for "continue watching" from the most
// recently watched first: at most limit, leaving out the ones the list has as
// completed, which belong on the Completed tab instead. allFinished says
// there was history but every show in it is finished.
func ContinueWatching(recent []store.Progress, completed []store.ListEntry, limit int) (shows []store.Progress, allFinished bool) {
	done := map[int]bool{}
	for _, e := range completed {
		done[e.MediaID] = true
	}
	for _, p := range recent {
		if done[p.MediaID] || len(shows) == limit {
			continue
		}
		shows = append(shows, p)
	}
	return shows, len(shows) == 0 && len(recent) > 0
}

// CaughtUp reports that nothing after what's been watched has aired, going by
// the show's latest progress, or failing that its list entry.
func CaughtUp(m anilist.Media, progress *store.Progress, entry *store.ListEntry) bool {
	aired := float64(m.AiredEpisodes())
	switch {
	case aired == 0:
		return false
	case progress != nil:
		return NextEpisode(*progress) > aired
	case entry != nil:
		return float64(entry.Progress) >= aired
	}
	return false
}
