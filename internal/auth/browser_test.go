package auth

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/launcher"

	"github.com/EmoFa/anitui/internal/browser"
)

// TestCallbackPageInRealBrowser checks the page's script really forwards the
// fragment, which only a browser can run.
func TestCallbackPageInRealBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if _, ok := launcher.LookPath(); !ok {
		t.Skip("no Chrome/Chromium installed")
	}
	sn := &browser.Sniffer{}
	defer sn.Close()

	addr := "127.0.0.1:47297"
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	g, err := Login(ctx, LoginOptions{
		ClientID: 7, Addrs: []string{addr}, Timeout: 45 * time.Second,
		Open: func(string) error {
			// Stand in for AniList's redirect back to the callback.
			go sn.Sniff(ctx, browser.SniffRequest{
				URL:     "http://" + addr + "/callback#access_token=jwt.token.value&token_type=Bearer&expires_in=3600",
				Matches: []browser.Match{{Name: "never", URL: regexp.MustCompile(`^never$`)}},
				Timeout: 15 * time.Second,
			})
			return nil
		},
	})
	if err != nil || g.AccessToken != "jwt.token.value" || g.ExpiresIn != time.Hour {
		t.Fatalf("grant=%+v err=%v", g, err)
	}
}
