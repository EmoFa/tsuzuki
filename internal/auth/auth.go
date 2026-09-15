// Package auth logs in to AniList with its implicit grant: the user approves
// tsuzuki in their browser and AniList redirects to a local callback with the
// access token in the URL fragment.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	// CallbackPort and RedirectURL must match the AniList API client's settings.
	CallbackPort = 47281
	RedirectURL  = "http://localhost:47281/callback"

	loginTimeout = 5 * time.Minute
)

var ErrNoClientID = errors.New("no AniList client ID configured")

// AuthorizeURL is the page where the user approves tsuzuki.
func AuthorizeURL(clientID int) string {
	return "https://anilist.co/api/v2/oauth/authorize?" + url.Values{
		"client_id":     {strconv.Itoa(clientID)},
		"response_type": {"token"},
	}.Encode()
}

// Token is a stored AniList login.
type Token struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
	UserID      int       `json:"user_id"`
	UserName    string    `json:"user_name"`
}

// Valid reports whether the token exists and hasn't expired.
func (t *Token) Valid() bool {
	return t != nil && t.AccessToken != "" && (t.ExpiresAt.IsZero() || time.Now().Before(t.ExpiresAt))
}

// TokenFile stores the token readable only by the user.
type TokenFile struct{ Path string }

// Load returns the stored token, or nil when not logged in.
func (f TokenFile) Load() (*Token, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("reading %s: %w", f.Path, err)
	}
	return &t, nil
}

func (f TokenFile) Save(t Token) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	// Write-and-rename so a crash never leaves a truncated token file.
	tmp, err := os.CreateTemp(filepath.Dir(f.Path), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.Path)
}

// Delete logs out. A missing file is not an error.
func (f TokenFile) Delete() error {
	if err := os.Remove(f.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

type LoginOptions struct {
	ClientID int
	// Open shows the authorize URL to the user, normally in their browser.
	Open func(url string) error
	// Addrs to listen on; default the callback port on IPv4 and IPv6 localhost.
	Addrs   []string
	Timeout time.Duration
}

// Grant is what AniList returned.
type Grant struct {
	AccessToken string
	ExpiresIn   time.Duration
}

// Login runs the browser flow and returns the access token.
func Login(ctx context.Context, opts LoginOptions) (Grant, error) {
	if opts.ClientID == 0 {
		return Grant{}, ErrNoClientID
	}
	addrs := opts.Addrs
	if len(addrs) == 0 {
		addrs = []string{fmt.Sprintf("127.0.0.1:%d", CallbackPort), fmt.Sprintf("[::1]:%d", CallbackPort)}
	}

	result := make(chan loginResult, 1)
	var once sync.Once
	deliver := func(r loginResult) { once.Do(func() { result <- r }) }

	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, callbackPage)
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		g, err := parseGrant(string(body))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
		}
		deliver(loginResult{g, err})
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	var listening int
	var listenErr error
	for _, addr := range addrs {
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			listenErr = err
			continue
		}
		listening++
		go srv.Serve(ln)
	}
	if listening == 0 {
		return Grant{}, fmt.Errorf("starting login callback on port %d (is another login running?): %w", CallbackPort, listenErr)
	}
	defer srv.Close()

	authURL := AuthorizeURL(opts.ClientID)
	if err := opts.Open(authURL); err != nil {
		slog.Warn("opening browser", "err", err)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = loginTimeout
	}
	select {
	case r := <-result:
		return r.grant, r.err
	case <-ctx.Done():
		return Grant{}, ctx.Err()
	case <-time.After(timeout):
		return Grant{}, errors.New("timed out waiting for AniList approval")
	}
}

type loginResult struct {
	grant Grant
	err   error
}

// parseGrant reads the fragment (or query, on errors) AniList redirected with.
func parseGrant(raw string) (Grant, error) {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return Grant{}, fmt.Errorf("unreadable AniList response: %w", err)
	}
	if e := v.Get("error"); e != "" {
		if d := v.Get("error_description"); d != "" {
			e += ": " + d
		}
		return Grant{}, fmt.Errorf("AniList login failed: %s", e)
	}
	token := v.Get("access_token")
	if token == "" {
		return Grant{}, errors.New("AniList did not return an access token")
	}
	g := Grant{AccessToken: token}
	if secs, err := strconv.Atoi(v.Get("expires_in")); err == nil && secs > 0 {
		g.ExpiresIn = time.Duration(secs) * time.Second
	}
	return g, nil
}

// callbackPage forwards the URL fragment (never sent to servers) to tsuzuki.
const callbackPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><title>tsuzuki login</title>
<style>body{font:16px system-ui,sans-serif;max-width:32rem;margin:15vh auto;padding:0 1rem;line-height:1.5}</style>
</head><body>
<h1>tsuzuki</h1><p id="msg">Finishing login…</p>
<script>
const payload = location.hash.length > 1 ? location.hash.slice(1) : location.search.slice(1);
const msg = document.getElementById("msg");
fetch("/token", {method: "POST", body: payload})
  .then(r => r.ok ? r.text() : r.text().then(t => Promise.reject(new Error(t))))
  .then(() => { msg.textContent = "You're logged in. You can close this tab and return to tsuzuki."; })
  .catch(e => { msg.textContent = "Login failed: " + e.message; });
history.replaceState(null, "", location.pathname);
</script>
</body></html>
`
