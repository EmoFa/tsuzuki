package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/EmoFa/tsuzuki/internal/httpx"
	"github.com/EmoFa/tsuzuki/internal/sniff"
)

// sniffRequest is a sniff.Request as the app's WebView takes it.
type sniffRequest struct {
	URL     string
	Referer string
	Matches []sniffMatch
	// TimeoutMillis bounds the whole sniff.
	TimeoutMillis int64
	// Blocked are URL patterns not worth loading, with * as a wildcard.
	Blocked []string
}

type sniffMatch struct {
	Name string
	// Pattern is a regular expression for the request URL.
	Pattern string
	Body    bool
}

// hostSniffer runs pages in the app's WebView (Host.Sniff).
type hostSniffer struct{ host Host }

func (s hostSniffer) Sniff(ctx context.Context, req sniff.Request) (map[string]sniff.Captured, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	r := sniffRequest{URL: req.URL, Referer: req.Referer, TimeoutMillis: timeout.Milliseconds(), Blocked: sniff.BlockedURLs}
	for _, m := range req.Matches {
		r.Matches = append(r.Matches, sniffMatch{Name: m.Name, Pattern: m.URL.String(), Body: m.Body})
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	type result struct {
		got map[string]sniff.Captured
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := s.host.Sniff(string(b))
		if err != nil {
			done <- result{err: err}
			return
		}
		var got map[string]sniff.Captured
		err = json.Unmarshal([]byte(out), &got)
		done <- result{got, err}
	}()
	// The WebView gives up by itself at the timeout; a cancelled watch
	// doesn't wait for it.
	select {
	case r := <-done:
		return r.got, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// clearance is what the app's WebView returns from Host.Solve.
type clearance struct {
	UserAgent string
	Cookies   []struct{ Name, Value string }
}

// hostSolver gets past bot checks in the app's WebView (Host.Solve).
type hostSolver struct{ host Host }

func (s hostSolver) Solve(ctx context.Context, pageURL string) (*httpx.Clearance, error) {
	type result struct {
		c   *httpx.Clearance
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := s.host.Solve(pageURL)
		if err != nil {
			done <- result{err: err}
			return
		}
		var c clearance
		if err := json.Unmarshal([]byte(out), &c); err != nil {
			done <- result{err: err}
			return
		}
		if c.UserAgent == "" || len(c.Cookies) == 0 {
			done <- result{err: fmt.Errorf("the check at %s didn't finish", pageURL)}
			return
		}
		cl := &httpx.Clearance{UserAgent: c.UserAgent, ObtainedAt: time.Now()}
		for _, ck := range c.Cookies {
			cl.Cookies = append(cl.Cookies, &http.Cookie{Name: ck.Name, Value: ck.Value})
		}
		done <- result{c: cl}
	}()
	select {
	case r := <-done:
		return r.c, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ChallengeScript is a JavaScript function, for Host.Solve to run in its
// WebView, that returns true while the page is still a bot check.
const ChallengeScript = sniff.ChallengeJS
