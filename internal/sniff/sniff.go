// Package sniff describes capturing the network requests a page makes while it
// runs in a browser: what to load, which requests to keep, and what came back.
// Each platform's browser (internal/browser on desktop, a WebView on Android)
// implements it.
package sniff

import (
	"errors"
	"regexp"
	"time"
)

// ErrNoBrowser means there is no browser to run pages in.
var ErrNoBrowser = errors.New("no usable Chrome/Chromium: install one or set browser.path in the config")

// Match selects a request to capture.
type Match struct {
	Name string
	URL  *regexp.Regexp
	Body bool // also capture the response body
}

type Request struct {
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

// BlockedURLs keep the page light: artwork, fonts and ad trackers aren't needed
// to get a player to request its stream. Patterns use * as a wildcard.
var BlockedURLs = []string{"*.png*", "*.jpg*", "*.jpeg*", "*.gif*", "*.webp*", "*.woff*", "*.ttf*", "*tiktokcdn*", "*doubleclick*", "*googlesyndication*"}

// ChallengeJS reports whether the page is still showing a bot challenge (or
// hasn't finished loading the target yet).
const ChallengeJS = `() => {
	if (location.href === "about:blank" || document.readyState !== "complete") return true;
	const t = document.title || "";
	if (/just a moment|checking your browser|ddos-guard|attention required/i.test(t)) return true;
	return !!document.querySelector('#challenge-form, #challenge-error-text, #challenge-running, script[src*="ddos-guard/js-challenge"]');
}`
