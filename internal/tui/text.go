package tui

import (
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/store"
)

var (
	htmlBreak = regexp.MustCompile(`(?i)<br\s*/?>`)
	htmlTag   = regexp.MustCompile(`<[^>]+>`)
	blankRuns = regexp.MustCompile(`\n{3,}`)
)

// plainDescription turns AniList's HTML-ish description into plain text.
func plainDescription(s string) string {
	s = htmlBreak.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = blankRuns.ReplaceAllString(strings.ReplaceAll(s, "\r", ""), "\n\n")
	return strings.TrimSpace(s)
}

func clock(d time.Duration) string {
	d = d.Round(time.Second)
	h, m, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

func episodeLabel(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }

// truncate shortens s to width terminal cells, adding an ellipsis.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

// clampLines keeps at most n lines, marking the cut with an ellipsis.
func clampLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	lines = lines[:n]
	lines[n-1] = strings.TrimRight(lines[n-1], " ") + " …"
	return strings.Join(lines, "\n")
}

// mediaMeta summarises format, year, episodes and score.
func mediaMeta(m anilist.Media) string {
	var parts []string
	if m.Format != "" {
		parts = append(parts, strings.ReplaceAll(m.Format, "_", " "))
	}
	if m.Year > 0 {
		parts = append(parts, strconv.Itoa(m.Year))
	}
	switch aired := m.AiredEpisodes(); {
	case m.NextAiringEpisode != nil && m.Episodes > 0:
		parts = append(parts, fmt.Sprintf("%d/%d eps", aired, m.Episodes))
	case m.NextAiringEpisode != nil:
		parts = append(parts, fmt.Sprintf("%d eps so far", aired))
	case m.Episodes > 0:
		parts = append(parts, fmt.Sprintf("%d eps", m.Episodes))
	}
	switch m.Status {
	case "RELEASING":
		parts = append(parts, "airing")
	case "NOT_YET_RELEASED":
		parts = append(parts, "upcoming")
	}
	if m.AverageScore > 0 {
		parts = append(parts, fmt.Sprintf("%d%%", m.AverageScore))
	}
	return strings.Join(parts, " · ")
}

// nextEpisode is the episode to continue with after p: p itself if unfinished,
// otherwise the following one.
func nextEpisode(p store.Progress) float64 {
	if !p.Completed {
		return p.Episode
	}
	return math.Floor(p.Episode) + 1
}

func ago(t time.Time) string {
	switch d := time.Since(t); {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
