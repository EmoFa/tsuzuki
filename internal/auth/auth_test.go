package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// browserSim plays AniList + the user's browser: it opens the callback with
// the given fragment and then posts it the way the callback page's script does.
func browserSim(t *testing.T, addr, fragment string) func(string) error {
	return func(authURL string) error {
		if !strings.Contains(authURL, "client_id=123") || !strings.Contains(authURL, "response_type=token") {
			t.Errorf("authorize URL = %s", authURL)
		}
		go func() {
			resp, err := http.Get("http://" + addr + "/callback")
			if err != nil {
				t.Error(err)
				return
			}
			page, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(page), `fetch("/token"`) {
				t.Errorf("callback page lacks token forwarding")
			}
			resp, err = http.Post("http://"+addr+"/token", "text/plain", strings.NewReader(fragment))
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}
}

func TestLoginSuccessAndError(t *testing.T) {
	addr := "127.0.0.1:47299"
	g, err := Login(context.Background(), LoginOptions{
		ClientID: 123, Addrs: []string{addr}, Timeout: 5 * time.Second,
		Open: browserSim(t, addr, "access_token=abc.def&token_type=Bearer&expires_in=31536000"),
	})
	if err != nil || g.AccessToken != "abc.def" || g.ExpiresIn != 365*24*time.Hour {
		t.Fatalf("grant=%+v err=%v", g, err)
	}

	_, err = Login(context.Background(), LoginOptions{
		ClientID: 123, Addrs: []string{addr}, Timeout: 5 * time.Second,
		Open: browserSim(t, addr, "error=access_denied&error_description=The+user+denied+the+request"),
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied: The user denied the request") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginNeedsClientID(t *testing.T) {
	if _, err := Login(context.Background(), LoginOptions{}); !errors.Is(err, ErrNoClientID) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	_, err := Login(context.Background(), LoginOptions{
		ClientID: 1, Addrs: []string{"127.0.0.1:47298"}, Timeout: 100 * time.Millisecond,
		Open: func(string) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestTokenFile(t *testing.T) {
	f := TokenFile{Path: filepath.Join(t.TempDir(), "sub", "anilist-token.json")}
	if tok, err := f.Load(); tok != nil || err != nil {
		t.Fatalf("empty: %v %v", tok, err)
	}
	in := Token{AccessToken: "abc", ExpiresAt: time.Now().Add(time.Hour).Truncate(time.Second), UserID: 42, UserName: "testuser"}
	if err := f.Save(in); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(f.Path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("token file mode = %v", info.Mode().Perm())
		}
	}
	out, err := f.Load()
	if err != nil || out.AccessToken != "abc" || out.UserName != "testuser" || !out.ExpiresAt.Equal(in.ExpiresAt) || !out.Valid() {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	expired := Token{AccessToken: "x", ExpiresAt: time.Now().Add(-time.Minute)}
	if expired.Valid() {
		t.Error("expired token reported valid")
	}
	if err := f.Delete(); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}
