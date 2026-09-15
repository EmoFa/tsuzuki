package skip

import (
	"slices"
	"time"

	"github.com/EmoFa/anitui/internal/domain"
)

// Actions from config.
const (
	Auto   = "auto"
	Prompt = "prompt"
	Off    = "off"
)

// minRemaining: a range with less than this left isn't worth seeking over.
const minRemaining = 2 * time.Second

// Decision is what to do after a position update or a skip request.
type Decision struct {
	Kind   domain.SkipKind
	Seek   time.Duration // >0: seek here
	Prompt bool          // tell the user they can skip
	Until  time.Duration // how long the prompt stays relevant
}

func (d Decision) None() bool { return d.Seek == 0 && !d.Prompt }

// Skipper decides, per playback, when to skip or offer to skip each range.
type Skipper struct {
	actions map[domain.SkipKind]string
	ranges  []rangeState
}

type rangeState struct {
	domain.SkipRange
	skipped  bool
	prompted bool
}

// NewSkipper takes the action ("auto", "prompt" or "off") for each kind.
func NewSkipper(actions map[domain.SkipKind]string) *Skipper {
	return &Skipper{actions: actions}
}

// SetRanges replaces the known ranges. Ranges already skipped or prompted
// keep that state, so late-arriving duplicates don't repeat.
func (s *Skipper) SetRanges(ranges []domain.SkipRange) {
	var next []rangeState
	for _, r := range ranges {
		if r.End <= r.Start || s.action(r.Kind) == Off {
			continue
		}
		st := rangeState{SkipRange: r}
		for _, old := range s.ranges {
			if old.SkipRange == r {
				st = old
			}
		}
		next = append(next, st)
	}
	slices.SortFunc(next, func(a, b rangeState) int { return int(a.Start - b.Start) })
	s.ranges = next
}

// HasRanges reports whether any range applies.
func (s *Skipper) HasRanges() bool { return len(s.ranges) > 0 }

func (s *Skipper) action(k domain.SkipKind) string {
	if a, ok := s.actions[k]; ok {
		return a
	}
	return Off
}

// Position handles a playback position update. Each range is skipped or
// prompted at most once per playback, so seeking back into it is respected.
func (s *Skipper) Position(pos time.Duration) Decision {
	for i := range s.ranges {
		r := &s.ranges[i]
		if pos < r.Start || pos >= r.End-minRemaining {
			continue
		}
		switch s.action(r.Kind) {
		case Auto:
			if !r.skipped {
				r.skipped = true
				return Decision{Kind: r.Kind, Seek: r.End}
			}
		case Prompt:
			if !r.prompted && !r.skipped {
				r.prompted = true
				return Decision{Kind: r.Kind, Prompt: true, Until: r.End - pos}
			}
		}
	}
	return Decision{}
}

// Request handles the user pressing the skip key.
func (s *Skipper) Request(pos time.Duration) Decision {
	for i := range s.ranges {
		r := &s.ranges[i]
		if pos >= r.Start && pos < r.End-minRemaining {
			r.skipped = true
			return Decision{Kind: r.Kind, Seek: r.End}
		}
	}
	return Decision{}
}
