//go:build live

package mobile

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/player"
)

// liveHost plays in a fake player that "watches" a minute and stops, and has
// no browser, as an Android phone without its WebView would.
type liveHost struct {
	mu       sync.Mutex
	requests []player.Request
}

func (h *liveHost) OpenURL(string) error         { return nil }
func (h *liveHost) Sniff(string) (string, error) { return "", errors.New("no browser here") }
func (h *liveHost) Solve(string) (string, error) { return "", errors.New("no browser here") }
func (h *liveHost) Play(req string, pb *Playback) (Player, error) {
	var r player.Request
	if err := json.Unmarshal([]byte(req), &r); err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.requests = append(h.requests, r)
	h.mu.Unlock()
	go func() {
		pb.Duration(int64(24 * time.Minute))
		pb.Position(int64(time.Minute), int64(24*time.Minute))
		time.Sleep(200 * time.Millisecond)
		pb.Ended("quit", "")
	}()
	return livePlayer{}, nil
}

type livePlayer struct{}

func (livePlayer) SeekTo(int64) error                       { return nil }
func (livePlayer) ShowText(string, int64) error             { return nil }
func (livePlayer) AddSubtitle(string, string, string) error { return nil }
func (livePlayer) BindKey(string, string) error             { return nil }
func (livePlayer) Stop() error                              { return nil }

type statusLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *statusLog) Status(s string) {
	l.mu.Lock()
	l.lines = append(l.lines, s)
	l.mu.Unlock()
}

// Search finds a show on AniList, and Watch resolves a real stream for it
// from a provider that needs no browser, saving progress.
func TestLiveSearchAndWatch(t *testing.T) {
	h := &liveHost{}
	a, err := NewApp(t.TempDir(), t.TempDir(), h)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	found, err := a.Search("Frieren")
	if err != nil {
		t.Fatal(err)
	}
	var media []json.RawMessage
	if err := json.Unmarshal([]byte(found), &media); err != nil || len(media) == 0 {
		t.Fatalf("search: %s (%v)", found, err)
	}
	req := `{"Media":` + string(media[0]) + `,"Episode":1,"Mode":"sub"}`
	log := &statusLog{}
	w, err := a.Watch(req, log)
	if err != nil {
		t.Fatal(err)
	}
	err = w.Await()
	for _, l := range log.lines {
		var s struct{ Kind, Provider, Err string }
		json.Unmarshal([]byte(l), &s)
		t.Logf("%-16s %-12s %s", s.Kind, s.Provider, s.Err)
	}
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if len(h.requests) == 0 {
		t.Fatal("nothing was played")
	}
	r := h.requests[0]
	t.Logf("played %s (%s) %q, %d subtitle tracks", r.URL, r.Kind, r.Title, len(r.Subtitles)+len(r.MoreSubtitles))
	if !strings.HasPrefix(r.URL, "http") || r.Kind == "" {
		t.Errorf("request %+v", r)
	}
	progress, err := a.ShowProgress(154587)
	if err != nil || !strings.Contains(progress, `"Episode":1`) {
		t.Errorf("progress %s (%v)", progress, err)
	}
}
