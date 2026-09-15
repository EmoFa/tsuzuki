// Package browser gets past bot protection using a real Chrome/Chromium.
//
// Automatic challenges (JavaScript checks) are solved in a headless browser.
// Interactive ones (Cloudflare Turnstile checkboxes) need a person: a normal
// browser window opens with no automation attached (Turnstile rejects
// DevTools-controlled browsers even when a human clicks), the user completes
// the check and closes the window, and the cookies are then read back from the
// shared profile. anitui never clicks a verification checkbox itself.
package browser

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/EmoFa/anitui/internal/httpx"
)

var ErrNoBrowser = errors.New("no Chrome/Chromium found: install one or set browser.path in the config")

// challengeJS reports whether the page is still showing a bot challenge (or
// hasn't finished loading the target yet).
const challengeJS = `() => {
	if (location.href === "about:blank" || document.readyState !== "complete") return true;
	const t = document.title || "";
	if (/just a moment|checking your browser|ddos-guard|attention required/i.test(t)) return true;
	return !!document.querySelector("#challenge-form, #challenge-error-text, #challenge-running");
}`

var devtoolsURL = regexp.MustCompile(`DevTools listening on (ws://\S+)`)

type Solver struct {
	BinPath      string // empty: auto-detect
	ProfileDir   string // persistent; holds the clearance cookies
	AutoDownload bool   // download Chromium when none is installed
	DownloadDir  string
	// Headless tries automatic challenges invisibly before asking the user.
	Headless bool
	// Notify tells the user an interactive check is needed.
	Notify func(msg string)

	HeadlessWait    time.Duration // default 15s
	InteractiveWait time.Duration // default 5m

	mu sync.Mutex // the profile can only be open in one browser at a time
}

// Solve implements httpx.Solver.
func (s *Solver) Solve(ctx context.Context, pageURL string) (*httpx.Clearance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bin, err := s.findBinary()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.ProfileDir, 0o700); err != nil {
		return nil, err
	}

	if s.Headless {
		cl, err := s.headless(ctx, bin, pageURL, durationOr(s.HeadlessWait, 15*time.Second))
		if err != nil || cl != nil {
			return cl, err
		}
		slog.Info("headless browser did not pass challenge; asking user", "url", pageURL)
	}

	host := pageURL
	if u, err := url.Parse(pageURL); err == nil {
		host = u.Host
	}
	if s.Notify != nil {
		s.Notify(fmt.Sprintf("%s needs a one-time human verification. Complete it in the browser window that just opened, wait for the site to load, then close the window.", host))
	}
	if err := s.interactive(ctx, bin, pageURL); err != nil {
		return nil, err
	}

	// Read the cookies the user earned back out of the profile.
	cl, err := s.headless(ctx, bin, pageURL, 20*time.Second)
	if err != nil {
		return nil, err
	}
	if cl == nil {
		return nil, fmt.Errorf("%s: verification was not completed", host)
	}
	return cl, nil
}

func (s *Solver) findBinary() (string, error) {
	return findBinary(s.BinPath, s.AutoDownload, s.DownloadDir, s.Notify)
}

// findBinary resolves Chrome/Chromium from a configured path, the system, or
// (optionally) a download.
func findBinary(configured string, autoDownload bool, downloadDir string, notify func(string)) (string, error) {
	if configured != "" {
		p, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("browser.path %q: %w", configured, err)
		}
		return p, nil
	}
	if p, ok := launcher.LookPath(); ok {
		return p, nil
	}
	if !autoDownload {
		return "", ErrNoBrowser
	}
	b := launcher.NewBrowser()
	if downloadDir != "" {
		b.RootDir = downloadDir
	}
	if notify != nil {
		notify("Downloading Chromium for bot-protection checks…")
	}
	return b.Get()
}

// headless loads pageURL in a headless browser on the shared profile and
// returns a clearance if no challenge remains after wait, or nil if one does.
func (s *Solver) headless(ctx context.Context, bin, pageURL string, wait time.Duration) (*httpx.Clearance, error) {
	proc, err := startDevTools(ctx, bin,
		"--headless=new",
		"--user-data-dir="+s.ProfileDir,
		"--disable-blink-features=AutomationControlled",
		"--no-first-run", "--no-default-browser-check",
		"about:blank",
	)
	if err != nil {
		return nil, err
	}
	defer proc.stop()

	b := rod.New().ControlURL(proc.wsURL).Context(ctx)
	if err := b.Connect(); err != nil {
		return nil, fmt.Errorf("connecting to browser: %w", err)
	}
	proc.browser = b

	ver, err := proto.BrowserGetVersion{}.Call(b)
	if err != nil {
		return nil, err
	}
	// Must match the UA the interactive (non-headless) window presents, since
	// clearance cookies are tied to it.
	ua := strings.Replace(ver.UserAgent, "HeadlessChrome", "Chrome", 1)

	page, err := b.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, err
	}
	if err := (proto.NetworkSetUserAgentOverride{UserAgent: ua}).Call(page); err != nil {
		return nil, err
	}
	// Record which server address the document came from (see Clearance.RemoteIP).
	if err := (proto.NetworkEnable{}).Call(page); err != nil {
		return nil, err
	}
	var ipMu sync.Mutex
	var remoteIP string
	target := hostname(pageURL)
	go page.EachEvent(func(e *proto.NetworkResponseReceived) {
		if e.Type == proto.NetworkResourceTypeDocument && hostname(e.Response.URL) == target && e.Response.RemoteIPAddress != "" {
			ipMu.Lock()
			remoteIP = e.Response.RemoteIPAddress
			ipMu.Unlock()
		}
	})()

	waitLoad := page.Timeout(wait).WaitNavigation(proto.PageLifecycleEventNameLoad)
	if err := page.Navigate(pageURL); err != nil {
		return nil, fmt.Errorf("loading %s: %w", pageURL, err)
	}
	waitLoad()

	deadline := time.Now().Add(wait)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := page.Eval(challengeJS)
		// Evaluation fails transiently while the challenge reloads the page.
		if err == nil && !res.Value.Bool() {
			break
		}
		if time.Now().After(deadline) {
			return nil, nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	raw, err := page.Cookies([]string{pageURL})
	if err != nil {
		return nil, err
	}
	ipMu.Lock()
	cl := &httpx.Clearance{UserAgent: ua, RemoteIP: remoteIP, ObtainedAt: time.Now()}
	ipMu.Unlock()
	for _, c := range raw {
		ck := &http.Cookie{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path, Secure: c.Secure, HttpOnly: c.HTTPOnly}
		if c.Expires > 0 {
			ck.Expires = c.Expires.Time()
		}
		cl.Cookies = append(cl.Cookies, ck)
	}
	slog.Info("browser clearance obtained", "url", pageURL, "cookies", len(cl.Cookies), "remote_ip", cl.RemoteIP)
	return cl, nil
}

// interactive opens a plain browser window on the shared profile and waits for
// the user to close it.
func (s *Solver) interactive(ctx context.Context, bin, pageURL string) error {
	cmd := exec.Command(bin,
		"--user-data-dir="+s.ProfileDir,
		"--no-first-run", "--no-default-browser-check",
		"--new-window", "--window-size=1000,760",
		pageURL,
	)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timeout := time.NewTimer(durationOr(s.InteractiveWait, 5*time.Minute))
	defer timeout.Stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return ctx.Err()
	case <-timeout.C:
		cmd.Process.Kill()
		<-done
		return errors.New("timed out waiting for the verification window to close")
	}
}

type devToolsProc struct {
	cmd     *exec.Cmd
	done    chan struct{}
	wsURL   string
	browser *rod.Browser
}

// startDevTools launches the browser with a DevTools port and waits for its URL.
// We manage the process ourselves (not rod's launcher) so we can wait for it to
// exit and release the profile lock, and so the profile is never deleted.
func startDevTools(ctx context.Context, bin string, args ...string) (*devToolsProc, error) {
	cmd := exec.Command(bin, append([]string{"--remote-debugging-port=0"}, args...)...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting browser: %w", err)
	}
	p := &devToolsProc{cmd: cmd, done: make(chan struct{})}
	go func() { cmd.Wait(); close(p.done) }()

	found := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			if m := devtoolsURL.FindStringSubmatch(sc.Text()); m != nil {
				found <- m[1]
				break
			}
		}
		io.Copy(io.Discard, stderr)
	}()

	select {
	case p.wsURL = <-found:
		return p, nil
	case <-p.done:
		return nil, errors.New("browser exited before DevTools was ready (is another instance using the profile?)")
	case <-ctx.Done():
		p.kill()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		p.kill()
		return nil, errors.New("timed out starting browser")
	}
}

// stop closes the browser gracefully so cookies are flushed to the profile.
func (p *devToolsProc) stop() {
	if p.browser != nil {
		p.browser.Close()
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.kill()
	}
}

func (p *devToolsProc) kill() {
	p.cmd.Process.Kill()
	<-p.done
}

func hostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func durationOr(d, fallback time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return fallback
}
