package extractor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/EmoFa/tsuzuki/internal/browser"
	"github.com/EmoFa/tsuzuki/internal/hls"
	"github.com/EmoFa/tsuzuki/internal/httpx"
)

// MegaplayOrigin is the Referer megaplay's CDNs require for playlists,
// segments and subtitles.
const MegaplayOrigin = "https://megaplay.buzz/"

var (
	megaplaySources = regexp.MustCompile(`/stream/getSources\?`)
	megaplayMaster  = regexp.MustCompile(`\.m3u8(\?|$)`)
)

// Sniffer runs an embed page in a browser (browser.Sniffer).
type Sniffer interface {
	Sniff(ctx context.Context, req browser.SniffRequest) (map[string]browser.Captured, error)
}

type MegaplayTrack struct {
	File    string `json:"file"`
	Label   string `json:"label"`
	Kind    string `json:"kind"`
	Default bool   `json:"default"`
}

type MegaplaySkip struct {
	Start int `json:"start"` // seconds
	End   int `json:"end"`
}

type MegaplayResult struct {
	Master   string
	Variants []hls.Variant
	Tracks   []MegaplayTrack // subtitles/captions
	Intro    MegaplaySkip
	Outro    MegaplaySkip
}

// ResolveMegaplay plays a megaplay embed in a headless browser to obtain its
// HLS master: getSources returns the URL encrypted, and only the player's
// obfuscated script can decrypt it. The master is then fetched directly.
func ResolveMegaplay(ctx context.Context, sn Sniffer, client *httpx.Client, embedURL, referer string) (MegaplayResult, error) {
	got, err := sn.Sniff(ctx, browser.SniffRequest{
		URL:     embedURL,
		Referer: referer,
		Matches: []browser.Match{
			{Name: "sources", URL: megaplaySources, Body: true},
			{Name: "master", URL: megaplayMaster},
		},
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return MegaplayResult{}, fmt.Errorf("megaplay: %w", err)
	}
	return megaplayFromCapture(ctx, client, got["sources"].Body, got["master"].URL)
}

func megaplayFromCapture(ctx context.Context, client *httpx.Client, sources []byte, master string) (MegaplayResult, error) {
	var src struct {
		Tracks []MegaplayTrack `json:"tracks"`
		Intro  MegaplaySkip    `json:"intro"`
		Outro  MegaplaySkip    `json:"outro"`
	}
	if err := json.Unmarshal(sources, &src); err != nil {
		return MegaplayResult{}, fmt.Errorf("megaplay getSources: %w", err)
	}
	res := MegaplayResult{Master: master, Intro: src.Intro, Outro: src.Outro}
	for _, t := range src.Tracks {
		if t.Kind == "captions" || t.Kind == "subtitles" {
			res.Tracks = append(res.Tracks, t)
		}
	}

	body, err := client.Get(ctx, master, map[string]string{"Referer": MegaplayOrigin})
	if err != nil {
		return MegaplayResult{}, fmt.Errorf("megaplay master: %w", err)
	}
	base, err := url.Parse(master)
	if err != nil {
		return MegaplayResult{}, err
	}
	res.Variants = hls.Variants(body, base)
	if len(res.Variants) == 0 {
		if !hls.IsPlaylist(body) {
			return MegaplayResult{}, errors.New("megaplay master is not a playlist")
		}
		// A media playlist: play the URL itself.
		res.Variants = []hls.Variant{{URI: master}}
	}
	return res, nil
}
