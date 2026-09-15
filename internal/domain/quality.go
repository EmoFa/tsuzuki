package domain

import (
	"fmt"
	"slices"
	"strconv"
)

// SelectStream picks a stream for a quality preference from config:
// "best", "worst", or a height like "720". For a height, the exact match wins,
// then the nearest lower height, then the nearest higher one. Streams of
// unknown height rank below every known height.
func SelectStream(streams []Stream, pref string) (Stream, error) {
	if len(streams) == 0 {
		return Stream{}, fmt.Errorf("no streams to choose from")
	}
	byHeight := slices.Clone(streams)
	// Stable so providers' own ordering breaks ties.
	slices.SortStableFunc(byHeight, func(a, b Stream) int { return b.Height - a.Height })

	switch pref {
	case "best", "":
		return byHeight[0], nil
	case "worst":
		for i := len(byHeight) - 1; i >= 0; i-- {
			if byHeight[i].Height > 0 {
				return byHeight[i], nil
			}
		}
		return byHeight[len(byHeight)-1], nil
	}

	want, err := strconv.Atoi(pref)
	if err != nil {
		return Stream{}, fmt.Errorf("invalid quality %q", pref)
	}
	for _, s := range byHeight {
		if s.Height > 0 && s.Height <= want {
			return s, nil
		}
	}
	// Nothing at or below the wanted height: take the smallest known one.
	for i := len(byHeight) - 1; i >= 0; i-- {
		if byHeight[i].Height > 0 {
			return byHeight[i], nil
		}
	}
	return byHeight[0], nil
}
