package domain

import (
	"slices"
	"strings"
)

// SubtitlePrefs says which subtitle tracks to prefer and whether to show them.
type SubtitlePrefs struct {
	Languages []string // language codes, most preferred first
	Show      bool     // false loads the tracks but starts with subtitles hidden
}

// SortSubtitles orders tracks for players, which select the first: preferred
// languages in order, then other human translations, then machine ("(AI)")
// translations, keeping the provider's order otherwise.
func SortSubtitles(subs []Subtitle, languages []string) []Subtitle {
	rank := func(s Subtitle) int {
		if isMachineTranslated(s) {
			return len(languages) + 1
		}
		if i := slices.Index(languages, strings.ToLower(s.Lang)); i >= 0 && s.Lang != "" {
			return i
		}
		return len(languages)
	}
	out := slices.Clone(subs)
	slices.SortStableFunc(out, func(a, b Subtitle) int { return rank(a) - rank(b) })
	return out
}

func isMachineTranslated(s Subtitle) bool {
	return strings.Contains(s.Label, "(AI)")
}
