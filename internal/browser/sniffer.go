package browser

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"

	"github.com/EmoFa/tsuzuki/internal/procfs"
)

// Sniffer loads pages in a shared headless browser and reports the network
// requests they make. It lets tsuzuki run a site's own player when the stream URL
// is only produced by obfuscated JavaScript.
type Sniffer struct {
	BinPath      string
	AutoDownload bool
	DownloadDir  string

	mu      sync.Mutex
	proc    *devToolsProc
	browser *rod.Browser
	dir     string
}

// Match selects a request to capture.
type Match struct {
	Name string
	URL  *regexp.Regexp
	Body bool // also capture the response body
}

type SniffRequest struct {
	URL     string
	Referer string
	Matches []Match
	Timeout time.Duration // default 30s
}

type Captured struct {
	URL    string
	Status int
	Body   []byte
}

// blockedURLs keep the page light: artwork, fonts and ad trackers aren't needed
// to get a player to request its stream.
var blockedURLs = []string{"*.png*", "*.jpg*", "*.jpeg*", "*.gif*", "*.webp*", "*.woff*", "*.ttf*", "*tiktokcdn*", "*doubleclick*", "*googlesyndication*"}

// Sniff loads req.URL and waits until every match has been captured.
func (s *Sniffer) Sniff(ctx context.Context, req SniffRequest) (map[string]Captured, error) {
	if len(req.Matches) == 0 {
		return nil, errors.New("sniff request has no matches")
	}
	b, err := s.ensure(ctx)
	if err != nil {
		return nil, err
	}
	timeout := durationOr(req.Timeout, 30*time.Second)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	page, err := b.Context(ctx).Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("opening page: %w", err)
	}
	defer page.Close()

	ver, err := proto.BrowserGetVersion{}.Call(b)
	if err != nil {
		return nil, err
	}
	ua := strings.Replace(ver.UserAgent, "HeadlessChrome", "Chrome", 1)
	if err := (proto.NetworkSetUserAgentOverride{UserAgent: ua}).Call(page); err != nil {
		return nil, err
	}
	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		return nil, err
	}
	_ = proto.NetworkSetBlockedURLs{Urls: blockedURLs}.Call(page)

	type inFlight struct {
		match  Match
		url    string
		status int
	}
	var mu sync.Mutex
	got := map[string]Captured{}
	pending := map[proto.NetworkRequestID]*inFlight{}
	done := make(chan struct{})
	var finishOnce sync.Once
	finish := func() { finishOnce.Do(func() { close(done) }) }
	var pageErr error                         // set when the page itself fails to load; mu held
	record := func(name string, c Captured) { // mu held
		if _, have := got[name]; have {
			return
		}
		got[name] = c
		if len(got) == len(req.Matches) {
			finish()
		}
	}

	go page.EachEvent(func(e *proto.NetworkRequestWillBeSent) {
		mu.Lock()
		defer mu.Unlock()
		for _, m := range req.Matches {
			if _, have := got[m.Name]; have || !m.URL.MatchString(e.Request.URL) {
				continue
			}
			if m.Body {
				pending[e.RequestID] = &inFlight{match: m, url: e.Request.URL}
			} else {
				record(m.Name, Captured{URL: e.Request.URL})
			}
			return
		}
	}, func(e *proto.NetworkResponseReceived) {
		mu.Lock()
		defer mu.Unlock()
		if f, ok := pending[e.RequestID]; ok {
			f.status = e.Response.Status
		}
		// Don't wait out the timeout for a page that failed to load.
		if e.Type == proto.NetworkResourceTypeDocument && e.Response.URL == req.URL && e.Response.Status >= 400 {
			pageErr = fmt.Errorf("page %s returned HTTP %d", req.URL, e.Response.Status)
			finish()
		}
	}, func(e *proto.NetworkLoadingFinished) {
		mu.Lock()
		f, ok := pending[e.RequestID]
		delete(pending, e.RequestID)
		mu.Unlock()
		if !ok {
			return
		}
		// Reading the body is another browser call; don't block the event loop.
		go func() {
			res, err := proto.NetworkGetResponseBody{RequestID: e.RequestID}.Call(page)
			if err != nil {
				slog.Debug("sniffer: reading body", "match", f.match.Name, "err", err)
				return
			}
			body := []byte(res.Body)
			if res.Base64Encoded {
				if body, err = base64.StdEncoding.DecodeString(res.Body); err != nil {
					return
				}
			}
			mu.Lock()
			record(f.match.Name, Captured{URL: f.url, Status: f.status, Body: body})
			mu.Unlock()
		}()
	})()

	if _, err := (proto.PageNavigate{URL: req.URL, Referrer: req.Referer}).Call(page); err != nil {
		return nil, fmt.Errorf("loading %s: %w", req.URL, err)
	}

	select {
	case <-done:
	case <-ctx.Done():
	}
	mu.Lock()
	defer mu.Unlock()
	if pageErr != nil {
		return nil, pageErr
	}
	out := make(map[string]Captured, len(got))
	for k, v := range got {
		out[k] = v
	}
	if len(out) < len(req.Matches) {
		var missing []string
		for _, m := range req.Matches {
			if _, ok := out[m.Name]; !ok {
				missing = append(missing, m.Name)
			}
		}
		return out, fmt.Errorf("page %s never requested %s", req.URL, strings.Join(missing, ", "))
	}
	return out, nil
}

// ensure starts the shared browser on first use.
func (s *Sniffer) ensure(ctx context.Context) (*rod.Browser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.browser != nil {
		select {
		case <-s.proc.done:
			s.browser = nil // crashed; start again
		default:
			return s.browser, nil
		}
	}
	bin, err := findBinary(s.BinPath, s.AutoDownload, s.DownloadDir, nil)
	if err != nil {
		return nil, err
	}
	procfs.SweepStale(os.TempDir(), "tsuzuki-sniffer-") // profiles of killed runs
	dir, err := os.MkdirTemp("", procfs.Name("tsuzuki-sniffer-"))
	if err != nil {
		return nil, err
	}
	proc, err := startDevTools(context.WithoutCancel(ctx), bin,
		"--headless=new",
		"--user-data-dir="+dir,
		"--disable-blink-features=AutomationControlled",
		"--autoplay-policy=no-user-gesture-required",
		"--mute-audio",
		"--no-first-run", "--no-default-browser-check",
		"about:blank",
	)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	b := rod.New().ControlURL(proc.wsURL)
	if err := b.Connect(); err != nil {
		proc.kill()
		os.RemoveAll(dir)
		return nil, fmt.Errorf("connecting to browser: %w", err)
	}
	proc.browser = b
	s.proc, s.browser, s.dir = proc, b, dir
	return b, nil
}

// Close shuts the browser down and removes its temporary profile.
func (s *Sniffer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return nil
	}
	s.proc.stop()
	err := os.RemoveAll(s.dir)
	s.proc, s.browser, s.dir = nil, nil, ""
	return err
}
