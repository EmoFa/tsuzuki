// Package httpx is the HTTP client every scraper uses. It adds a browser User-Agent,
// retries transient failures, and detects Cloudflare/DDoS-Guard challenges. On a
// challenge it gets a Clearance (cookies + User-Agent) from a Solver and retries.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultUserAgent is used for hosts without a clearance.
const DefaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/152.0.0.0 Safari/537.36"

// maxErrorBody bounds how much of a non-2xx body is read for detection and errors.
const maxErrorBody = 1 << 20

// Clearance lets plain HTTP requests through a host's bot protection. The
// cookies are only honoured together with the exact UserAgent that earned them.
type Clearance struct {
	UserAgent string         `json:"user_agent"`
	Cookies   []*http.Cookie `json:"cookies"`
	// RemoteIP is the server address the browser used. Cloudflare binds
	// clearance to the client IP, so requests must use the same address family
	// (Go's resolver may pick IPv6 where the browser's system resolver chose IPv4).
	RemoteIP   string    `json:"remote_ip,omitempty"`
	ObtainedAt time.Time `json:"obtained_at"`
}

// Solver obtains a clearance for the site serving pageURL, typically with a browser.
type Solver interface {
	Solve(ctx context.Context, pageURL string) (*Clearance, error)
}

// ClearanceStore persists clearances across runs. Load returns (nil, nil) when
// nothing is stored for host.
type ClearanceStore interface {
	LoadClearance(ctx context.Context, host string) (*Clearance, error)
	SaveClearance(ctx context.Context, host string, c *Clearance) error
	DeleteClearance(ctx context.Context, host string) error
}

// ChallengeError is returned when a bot challenge could not be passed.
type ChallengeError struct {
	URL  string
	Kind string // "cloudflare" or "ddos-guard"
	Err  error  // why solving failed; nil when no solver is configured
}

func (e *ChallengeError) Error() string {
	msg := fmt.Sprintf("%s challenge at %s", e.Kind, e.URL)
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *ChallengeError) Unwrap() error { return e.Err }

// StatusError is returned by the Get/Post helpers for non-2xx responses.
type StatusError struct {
	Method     string
	URL        string
	StatusCode int
	Header     http.Header
	Body       string // truncated
}

func (e *StatusError) Error() string {
	body := e.Body
	if len(body) > 200 {
		body = body[:200] + "…"
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.StatusCode, strings.TrimSpace(body))
}

type Options struct {
	// Transport defaults to an HTTP/2-capable transport that pins each cleared
	// host to its clearance's address family. A custom transport skips pinning.
	Transport http.RoundTripper
	// Timeout covers the whole request including reading the body. Default 30s;
	// negative disables it (for streaming, where the caller's context governs).
	Timeout time.Duration
	Solver  Solver         // optional
	Store   ClearanceStore // optional
	// Retries for network errors and 502/503/504 responses. Default 2.
	Retries int
	// Backoff before the first retry, doubled for each subsequent one. Default 300ms.
	Backoff time.Duration
}

type Client struct {
	http    *http.Client
	solver  Solver
	store   ClearanceStore
	retries int
	backoff time.Duration

	mu         sync.Mutex
	clearances map[string]*Clearance // by host; nil value = looked up, none stored
	solving    map[string]*solveCall
}

type solveCall struct {
	done chan struct{}
	err  error
}

func New(opts Options) *Client {
	c := &Client{
		solver:     opts.Solver,
		store:      opts.Store,
		clearances: map[string]*Clearance{},
		solving:    map[string]*solveCall{},
	}
	if opts.Transport == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		t.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if host, _, err := net.SplitHostPort(addr); err == nil && network == "tcp" {
				if family := c.familyFor(ctx, host); family != "" {
					network = family
				}
			}
			return dialer.DialContext(ctx, network, addr)
		}
		opts.Transport = t
	}
	switch {
	case opts.Timeout == 0:
		opts.Timeout = 30 * time.Second
	case opts.Timeout < 0:
		opts.Timeout = 0
	}
	if opts.Retries == 0 {
		opts.Retries = 2
	}
	if opts.Backoff == 0 {
		opts.Backoff = 300 * time.Millisecond
	}
	c.http = &http.Client{Transport: opts.Transport, Timeout: opts.Timeout}
	c.retries = opts.Retries
	c.backoff = opts.Backoff
	return c
}

// Do sends req. It sets User-Agent (unless present) and clearance cookies,
// retries transient failures, and handles bot challenges. Request bodies must be
// replayable (req.GetBody set, as http.NewRequest does for bytes readers).
// Any response returned has a readable body, including non-2xx ones.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	host := req.URL.Hostname()
	solved := false

	for attempt := 0; ; attempt++ {
		clearance := c.clearance(ctx, host)
		r, err := cloneRequest(req)
		if err != nil {
			return nil, err
		}
		applyClearance(r, clearance)

		resp, err := c.http.Do(r)
		if err != nil {
			if ctx.Err() == nil && attempt < c.retries {
				slog.Debug("http retry", "url", req.URL.String(), "err", err, "attempt", attempt+1)
				if c.sleep(ctx, attempt) == nil {
					continue
				}
			}
			return nil, err
		}

		if isChallengeStatus(resp.StatusCode) {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
			resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(body))
			if readErr != nil {
				return nil, readErr
			}

			if kind := detectChallenge(resp, body); kind != "" {
				slog.Info("bot challenge", "kind", kind, "url", req.URL.String(), "had_clearance", clearance != nil)
				if c.solver == nil || solved {
					return nil, &ChallengeError{URL: req.URL.String(), Kind: kind}
				}
				if clearance != nil {
					c.forget(ctx, host)
				}
				if err := c.solve(ctx, host, req.URL.String()); err != nil {
					return nil, &ChallengeError{URL: req.URL.String(), Kind: kind, Err: err}
				}
				solved = true
				// Pooled connections may use the wrong address family for the
				// new clearance.
				c.http.CloseIdleConnections()
				continue
			}
		}

		if isRetryableStatus(resp.StatusCode) && attempt < c.retries {
			resp.Body.Close()
			slog.Debug("http retry", "url", req.URL.String(), "status", resp.StatusCode, "attempt", attempt+1)
			if err := c.sleep(ctx, attempt); err != nil {
				return nil, err
			}
			continue
		}
		return resp, nil
	}
}

// Get fetches url and returns the body, or a *StatusError for non-2xx responses.
func (c *Client) Get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.send(req, headers)
}

// GetJSON fetches url and decodes the JSON body into v.
func (c *Client) GetJSON(ctx context.Context, url string, headers map[string]string, v any) error {
	body, err := c.Get(ctx, url, headers)
	if err != nil {
		return err
	}
	return decodeJSON(url, body, v)
}

// PostJSON sends payload as JSON and decodes the JSON response into v.
func (c *Client) PostJSON(ctx context.Context, url string, headers map[string]string, payload, v any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	body, err := c.send(req, headers)
	if err != nil {
		return err
	}
	return decodeJSON(url, body, v)
}

func (c *Client) send(req *http.Request, headers map[string]string) ([]byte, error) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &StatusError{Method: req.Method, URL: req.URL.String(), StatusCode: resp.StatusCode, Header: resp.Header, Body: string(body)}
	}
	return io.ReadAll(resp.Body)
}

func decodeJSON(url string, body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		snippet := string(body)
		if len(snippet) > 120 {
			snippet = snippet[:120] + "…"
		}
		return fmt.Errorf("decoding JSON from %s: %w (body: %q)", url, err, snippet)
	}
	return nil
}

// clearance returns the clearance for host (or a parent domain), loading it from
// the store on first use.
func (c *Client) clearance(ctx context.Context, host string) *Clearance {
	c.mu.Lock()
	defer c.mu.Unlock()
	for h := host; h != ""; h = parentDomain(h) {
		cl, seen := c.clearances[h]
		if !seen && c.store != nil {
			loaded, err := c.store.LoadClearance(ctx, h)
			if err != nil {
				slog.Warn("loading clearance", "host", h, "err", err)
			}
			cl = loaded
			c.clearances[h] = cl
		}
		if cl != nil {
			return cl
		}
	}
	return nil
}

// familyFor returns "tcp4" or "tcp6" when host has a clearance that recorded
// the browser's remote IP, or "" to dial normally.
func (c *Client) familyFor(ctx context.Context, host string) string {
	cl := c.clearance(ctx, host)
	if cl == nil || cl.RemoteIP == "" {
		return ""
	}
	ip := net.ParseIP(strings.Trim(cl.RemoteIP, "[]"))
	switch {
	case ip == nil:
		return ""
	case ip.To4() != nil:
		return "tcp4"
	default:
		return "tcp6"
	}
}

func (c *Client) forget(ctx context.Context, host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for h := host; h != ""; h = parentDomain(h) {
		if c.clearances[h] != nil && c.store != nil {
			if err := c.store.DeleteClearance(ctx, h); err != nil {
				slog.Warn("deleting clearance", "host", h, "err", err)
			}
		}
		c.clearances[h] = nil
	}
}

// solve runs the solver once per host even when many requests hit the challenge
// concurrently; callers that arrive mid-solve wait for its result.
func (c *Client) solve(ctx context.Context, host, pageURL string) error {
	c.mu.Lock()
	if call, ok := c.solving[host]; ok {
		c.mu.Unlock()
		select {
		case <-call.done:
			return call.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	call := &solveCall{done: make(chan struct{})}
	c.solving[host] = call
	c.mu.Unlock()

	cl, err := c.solver.Solve(ctx, pageURL)
	if err == nil && cl == nil {
		err = errors.New("solver returned no clearance")
	}

	c.mu.Lock()
	if err == nil {
		c.clearances[host] = cl
	}
	delete(c.solving, host)
	c.mu.Unlock()

	if err == nil && c.store != nil {
		if serr := c.store.SaveClearance(ctx, host, cl); serr != nil {
			slog.Warn("saving clearance", "host", host, "err", serr)
		}
	}
	call.err = err
	close(call.done)
	return err
}

func (c *Client) sleep(ctx context.Context, attempt int) error {
	t := time.NewTimer(c.backoff << attempt)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cloneRequest(req *http.Request) (*http.Request, error) {
	r := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		if req.GetBody == nil {
			return nil, errors.New("httpx: request body is not replayable")
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		r.Body = body
	}
	return r, nil
}

func applyClearance(r *http.Request, cl *Clearance) {
	if cl != nil {
		r.Header.Set("User-Agent", cl.UserAgent)
		now := time.Now()
		for _, ck := range cl.Cookies {
			if ck.Expires.IsZero() || ck.Expires.After(now) {
				r.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
			}
		}
	} else if r.Header.Get("User-Agent") == "" {
		r.Header.Set("User-Agent", DefaultUserAgent)
	}
}

func isChallengeStatus(code int) bool {
	return code == http.StatusForbidden || code == http.StatusServiceUnavailable || code == http.StatusTooManyRequests
}

func isRetryableStatus(code int) bool {
	return code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout
}

// detectChallenge distinguishes solvable browser challenges from plain blocks
// (e.g. a Cloudflare WAF 403), which a browser would not get past either.
func detectChallenge(resp *http.Response, body []byte) string {
	server := strings.ToLower(resp.Header.Get("Server"))
	switch {
	case resp.Header.Get("cf-mitigated") == "challenge":
		return "cloudflare"
	case strings.Contains(server, "cloudflare") &&
		(bytes.Contains(body, []byte("challenges.cloudflare.com")) || bytes.Contains(body, []byte("<title>Just a moment"))):
		return "cloudflare"
	case strings.Contains(server, "ddos-guard") || bytes.Contains(body, []byte("DDoS-Guard")):
		return "ddos-guard"
	}
	return ""
}

// parentDomain returns "b.c" for "a.b.c", and "" once only two labels remain
// or for IP addresses.
func parentDomain(host string) string {
	if net.ParseIP(host) != nil {
		return ""
	}
	_, rest, ok := strings.Cut(host, ".")
	if !ok || !strings.Contains(rest, ".") {
		return ""
	}
	return rest
}
