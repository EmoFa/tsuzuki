package discord

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EmoFa/tsuzuki/internal/anilist"
	"github.com/EmoFa/tsuzuki/internal/session"
)

// Observer turns playback updates from a session into presence activities.
type Observer struct {
	Presence interface{ Set(*Activity) }
	// ShowCover includes the anime's cover art (never for adult titles).
	ShowCover bool
	Now       func() time.Time // defaults to time.Now

	mu  sync.Mutex
	cur *nowPlaying
}

type nowPlaying struct {
	media    anilist.Media
	episode  float64
	paused   bool
	start    time.Time // when playback would have started, given the position
	duration time.Duration
}

// driftTolerance is how far the computed start or the duration may move before
// timestamps are resent; both jitter a little while a stream loads, and Discord
// rate-limits updates.
const driftTolerance = 3 * time.Second

func near(d time.Duration) bool { return d < driftTolerance && d > -driftTolerance }

// Status handles one session update. It is safe to call from any goroutine.
func (o *Observer) Status(st session.Status) {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := time.Now
	if o.Now != nil {
		now = o.Now
	}

	switch st.Kind {
	case session.StatusPlaying:
		o.cur = &nowPlaying{media: st.Media, episode: st.Episode}
		o.Presence.Set(o.activity())
	case session.StatusProgress:
		if o.cur == nil || o.cur.media.ID != st.Media.ID || o.cur.episode != st.Episode {
			o.cur = &nowPlaying{media: st.Media, episode: st.Episode}
		}
		if st.Duration <= 0 {
			return
		}
		start := now().Add(-st.Position)
		c := o.cur
		if c.paused == st.Paused && !c.start.IsZero() &&
			near(start.Sub(c.start)) && near(st.Duration-c.duration) {
			return
		}
		c.start, c.duration, c.paused = start, st.Duration, st.Paused
		o.Presence.Set(o.activity())
	case session.StatusStopped:
		o.cur = nil
		o.Presence.Set(nil)
	}
}

func (o *Observer) activity() *Activity {
	c := o.cur
	m := c.media
	a := &Activity{
		Type:              Watching,
		StatusDisplayType: StatusDisplayDetails,
		Details:           m.DisplayTitle(),
		State:             episodeState(m, c.episode),
	}
	if m.ID > 0 {
		url := "https://anilist.co/anime/" + strconv.Itoa(m.ID)
		a.DetailsURL = url
		a.Buttons = []Button{{Label: "View on AniList", URL: url}}
	}
	if c.paused {
		a.State += " · Paused"
	} else if !c.start.IsZero() && c.duration > 0 {
		a.Timestamps = &Timestamps{Start: c.start.UnixMilli(), End: c.start.Add(c.duration).UnixMilli()}
	}
	cover := m.Cover.ExtraLarge
	if cover == "" {
		cover = m.Cover.Large
	}
	if o.ShowCover && !m.IsAdult && strings.HasPrefix(cover, "https://") {
		a.Assets = &Assets{LargeImage: cover, LargeText: coverText(m)}
	}
	return a
}

func episodeState(m anilist.Media, episode float64) string {
	if m.Format == "MOVIE" && m.Episodes <= 1 {
		return "Movie"
	}
	ep := strconv.FormatFloat(episode, 'f', -1, 64)
	if m.Episodes > 0 {
		return fmt.Sprintf("Episode %s of %d", ep, m.Episodes)
	}
	return "Episode " + ep
}

// coverText is shown when hovering the cover: the original title when the
// display title is English, otherwise the season.
func coverText(m anilist.Media) string {
	if m.Title.Romaji != "" && m.Title.Romaji != m.DisplayTitle() {
		return m.Title.Romaji
	}
	var parts []string
	if m.Season != "" && m.Year > 0 {
		parts = append(parts, strings.ToUpper(m.Season[:1])+strings.ToLower(m.Season[1:])+" "+strconv.Itoa(m.Year))
	}
	if m.Format != "" {
		parts = append(parts, strings.ReplaceAll(m.Format, "_", " "))
	}
	return strings.Join(parts, " · ")
}
