// Package domain holds the provider-independent types that flow between
// providers, the player, trackers and the UI.
package domain

import (
	"strconv"
	"time"
)

// Mode is the audio/subtitle track type.
type Mode string

const (
	Sub Mode = "sub"
	Dub Mode = "dub"
)

// Show is a series or movie as listed by one provider.
type Show struct {
	Provider  string   `json:"provider"`
	ID        string   `json:"id"` // provider-specific
	Title     string   `json:"title"`
	AltTitles []string `json:"alt_titles,omitempty"`
	Type      string   `json:"type,omitempty"` // TV, Movie, ONA, ...
	Year      int      `json:"year,omitempty"`
	Episodes  int      `json:"episodes,omitempty"` // 0 when unknown
	Poster    string   `json:"poster,omitempty"`
	AniListID int      `json:"anilist_id,omitempty"`
	MalID     int      `json:"mal_id,omitempty"`
}

// Episode is one playable entry of a show.
type Episode struct {
	ID string `json:"id"` // provider-specific
	// Number is the canonical episode number, starting at 1 for each season
	// even when the provider continues numbering across seasons.
	Number float64 `json:"number"`
	// SourceNumber is the number exactly as the provider shows it.
	SourceNumber string        `json:"source_number,omitempty"`
	Title        string        `json:"title,omitempty"`
	Filler       bool          `json:"filler,omitempty"`
	Recap        bool          `json:"recap,omitempty"`
	Duration     time.Duration `json:"duration,omitempty"`
}

// Label formats the episode number without a trailing ".0".
func (e Episode) Label() string {
	return strconv.FormatFloat(e.Number, 'f', -1, 64)
}

type StreamKind string

const (
	HLS StreamKind = "hls"
	MP4 StreamKind = "mp4"
)

// Stream is a resolved, directly fetchable video URL.
type Stream struct {
	URL    string     `json:"url"`
	Kind   StreamKind `json:"kind"`
	Height int        `json:"height,omitempty"` // 0 when unknown
	Audio  Mode       `json:"audio"`
	// Label describes the source for humans, e.g. "SEV · 1080p".
	Label string `json:"label,omitempty"`
	// Headers must accompany every request for the stream and its segments.
	Headers   map[string]string `json:"headers,omitempty"`
	Subtitles []Subtitle        `json:"subtitles,omitempty"`
	// NeedsProxy marks streams the player can't fetch directly (the host rejects
	// HTTP/1.1, or playlists need decoding); they go through the stream proxy.
	NeedsProxy bool `json:"needs_proxy,omitempty"`
	// AudioLang picks the audio rendition ("ja", "en") when one HLS stream
	// carries several.
	AudioLang string `json:"audio_lang,omitempty"`
	// VariantHeight restricts an HLS master to the video variant of this height.
	// Used when audio renditions live in the master, so it can't be bypassed by
	// playing a variant playlist directly.
	VariantHeight int `json:"variant_height,omitempty"`
	// Playlist decodes provider-obfuscated playlists. Requires NeedsProxy.
	Playlist *PlaylistCodec `json:"-"`
	// WrappedSegments marks MPEG-TS segments disguised behind a fake image
	// header, which players can't demux. The proxy strips the header.
	// Requires NeedsProxy.
	WrappedSegments bool `json:"wrapped_segments,omitempty"`
}

// PlaylistCodec recognises and decodes playlists a provider has obfuscated.
type PlaylistCodec struct {
	Prefix []byte // bodies starting with this are encoded
	Decode func(body []byte) ([]byte, error)
}

type Subtitle struct {
	URL   string `json:"url"`
	Lang  string `json:"lang"`
	Label string `json:"label,omitempty"`
}
