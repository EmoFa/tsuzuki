package discord

import (
	"testing"
	"time"

	"github.com/EmoFa/anitui/internal/anilist"
	"github.com/EmoFa/anitui/internal/session"
)

type recorder struct{ sets []*Activity }

func (r *recorder) Set(a *Activity) { r.sets = append(r.sets, a) }

func TestObserver(t *testing.T) {
	media := anilist.Media{ID: 154587, Title: anilist.Title{English: "Frieren: Beyond Journey’s End", Romaji: "Sousou no Frieren"}, Format: "TV", Episodes: 28}
	media.Cover.ExtraLarge = "https://s4.anilist.co/file/anilistcdn/media/anime/cover/large/bx154587.jpg"
	now := time.UnixMilli(1_000_000_000)
	rec := &recorder{}
	o := &Observer{Presence: rec, ShowCover: true, Now: func() time.Time { return now }}

	base := session.Status{Media: media, Episode: 3}
	with := func(k session.StatusKind, pos time.Duration, paused bool) session.Status {
		s := base
		s.Kind, s.Position, s.Duration, s.Paused = k, pos, 24*time.Minute, paused
		return s
	}

	o.Status(session.Status{Kind: session.StatusPlaying, Media: media, Episode: 3})
	a := rec.sets[0]
	if a.Type != Watching || a.Details != media.Title.English || a.State != "Episode 3 of 28" || a.Timestamps != nil {
		t.Fatalf("playing = %+v", a)
	}
	if a.Assets == nil || a.Assets.LargeImage != media.Cover.ExtraLarge || a.Assets.LargeText != "Sousou no Frieren" {
		t.Errorf("assets = %+v", a.Assets)
	}
	if len(a.Buttons) != 1 || a.Buttons[0].URL != "https://anilist.co/anime/154587" || a.DetailsURL != a.Buttons[0].URL {
		t.Errorf("buttons = %+v url = %q", a.Buttons, a.DetailsURL)
	}

	o.Status(with(session.StatusProgress, time.Minute, false))
	a = rec.sets[1]
	if a.Timestamps == nil || a.Timestamps.Start != now.Add(-time.Minute).UnixMilli() || a.Timestamps.End != now.Add(23*time.Minute).UnixMilli() {
		t.Fatalf("timestamps = %+v", a.Timestamps)
	}

	// Ordinary progress (small jitter, duration refined while loading) sends nothing.
	now = now.Add(10 * time.Second)
	o.Status(with(session.StatusProgress, time.Minute+10*time.Second+time.Second, false))
	jitter := with(session.StatusProgress, time.Minute+10*time.Second, false)
	jitter.Duration += 40 * time.Millisecond
	o.Status(jitter)
	if len(rec.sets) != 2 {
		t.Fatalf("progress resent presence: %d", len(rec.sets))
	}

	// Seeking moves the timestamps.
	o.Status(with(session.StatusProgress, 5*time.Minute, false))
	if len(rec.sets) != 3 || rec.sets[2].Timestamps.Start != now.Add(-5*time.Minute).UnixMilli() {
		t.Fatalf("seek: %+v", rec.sets[len(rec.sets)-1])
	}

	// Pausing drops the timestamps; resuming restores them.
	o.Status(with(session.StatusProgress, 5*time.Minute, true))
	if a = rec.sets[3]; a.Timestamps != nil || a.State != "Episode 3 of 28 · Paused" {
		t.Fatalf("paused = %+v", a)
	}
	now = now.Add(time.Hour)
	o.Status(with(session.StatusProgress, 5*time.Minute, false))
	if a = rec.sets[4]; a.Timestamps == nil || a.Timestamps.Start != now.Add(-5*time.Minute).UnixMilli() {
		t.Fatalf("resumed = %+v", a)
	}

	o.Status(with(session.StatusStopped, 5*time.Minute, false))
	if rec.sets[5] != nil {
		t.Fatal("stop did not clear")
	}
}

func TestObserverVariants(t *testing.T) {
	movie := anilist.Media{ID: 1, Title: anilist.Title{Romaji: "Kimi no Na wa."}, Format: "MOVIE", Episodes: 1, Season: "SUMMER", Year: 2016}
	movie.Cover.Large = "https://img/cover.jpg"
	adult := anilist.Media{ID: 2, Title: anilist.Title{Romaji: "Adult"}, IsAdult: true}
	adult.Cover.Large = "https://img/adult.jpg"
	airing := anilist.Media{ID: 3, Title: anilist.Title{Romaji: "Airing"}}

	for _, tc := range []struct {
		name      string
		media     anilist.Media
		showCover bool
		state     string
		cover     string
		text      string
	}{
		{"movie", movie, true, "Movie", movie.Cover.Large, "Summer 2016 · MOVIE"},
		{"adult cover hidden", adult, true, "Episode 1", "", ""},
		{"covers off", movie, false, "Movie", "", ""},
		{"unknown episode count", airing, true, "Episode 1", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recorder{}
			o := &Observer{Presence: rec, ShowCover: tc.showCover}
			o.Status(session.Status{Kind: session.StatusPlaying, Media: tc.media, Episode: 1})
			a := rec.sets[0]
			if a.State != tc.state {
				t.Errorf("state = %q", a.State)
			}
			switch {
			case tc.cover == "" && a.Assets != nil:
				t.Errorf("assets = %+v", a.Assets)
			case tc.cover != "" && (a.Assets == nil || a.Assets.LargeImage != tc.cover || a.Assets.LargeText != tc.text):
				t.Errorf("assets = %+v", a.Assets)
			}
		})
	}
}
