package httpx

import (
	"bufio"
	"io"
	"net/http"
	"os"
	"testing"
)

// TestDetectChallengeOnCapturedPages uses real responses captured 2026-09-15
// (the DDoS-Guard challenge is synthetic: it can't be triggered on demand, so
// it's built from the markers observed on live DDoS-Guard sites).
func TestDetectChallengeOnCapturedPages(t *testing.T) {
	for file, want := range map[string]string{
		"cloudflare_challenge.http":          "cloudflare", // animepahe over HTTP/2: managed challenge
		"cloudflare_block_http11.http":       "",           // animepahe over HTTP/1.1: hard block
		"cloudflare_waf_block.http":          "",           // stream CDN rejecting HTTP/1.1
		"ddosguard_app_403.http":             "",           // site's own 403 behind DDoS-Guard
		"ddosguard_error_403.http":           "",           // DDoS-Guard's generic 403 page
		"ddosguard_ok_200.http":              "",
		"ddosguard_challenge_synthetic.http": "ddos-guard",
	} {
		t.Run(file, func(t *testing.T) {
			f, err := os.Open("testdata/" + file)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			resp, err := http.ReadResponse(bufio.NewReader(f), nil)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := detectChallenge(resp, body); got != want {
				t.Fatalf("detectChallenge = %q, want %q", got, want)
			}
		})
	}
}
