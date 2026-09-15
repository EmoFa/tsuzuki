package extractor

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/EmoFa/tsuzuki/internal/httpx"
)

var kwikSource = regexp.MustCompile(`source\s*=\s*['"](https?://[^'"]+\.m3u8[^'"]*)['"]`)

// KwikSource extracts the HLS URL from a kwik embed page.
func KwikSource(page string) (string, error) {
	scripts, err := UnpackAll(page)
	if err != nil {
		return "", fmt.Errorf("kwik: %w", err)
	}
	for _, s := range scripts {
		if m := kwikSource.FindStringSubmatch(s); m != nil {
			return m[1], nil
		}
	}
	return "", errors.New("kwik: no stream source in unpacked scripts")
}

// ResolveKwik fetches a kwik embed page and returns its HLS URL. Kwik refuses
// embeds requested without the embedding site as Referer.
func ResolveKwik(ctx context.Context, client *httpx.Client, embedURL, referer string) (string, error) {
	page, err := client.Get(ctx, embedURL, map[string]string{"Referer": referer})
	if err != nil {
		return "", err
	}
	return KwikSource(string(page))
}

// KwikOrigin is the Referer stream CDNs expect for kwik-sourced playlists.
const KwikOrigin = "https://kwik.cx/"
