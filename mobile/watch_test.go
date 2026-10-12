package mobile

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/EmoFa/tsuzuki/internal/player"
	"github.com/EmoFa/tsuzuki/internal/session"
)

// playingHost plays everything in a fake player that reports straight back.
type playingHost struct {
	fakeHost
	request string
	player  *fakePlayer
}

type fakePlayer struct {
	pb     *Playback
	seeked int64
}

func (h *playingHost) Play(req string, pb *Playback) (Player, error) {
	h.request = req
	h.player = &fakePlayer{pb: pb}
	return h.player, nil
}

func (p *fakePlayer) SeekTo(pos int64) error {
	p.seeked = pos
	p.pb.Seeked(pos)
	return nil
}
func (p *fakePlayer) ShowText(string, int64) error             { return nil }
func (p *fakePlayer) AddSubtitle(string, string, string) error { return nil }
func (p *fakePlayer) BindKey(string, string) error             { return nil }
func (p *fakePlayer) Stop() error {
	p.pb.Ended("quit", "")
	return nil
}

// The session drives the app's player through play, and hears what it reports.
func TestPlayBridgesTheAppsPlayer(t *testing.T) {
	h := &playingHost{}
	a := &App{host: h}
	pb, err := a.play(t.Context(), player.Request{URL: "http://127.0.0.1/x.m3u8", Start: 90 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	var req player.Request
	if err := json.Unmarshal([]byte(h.request), &req); err != nil || req.Start != 90*time.Second {
		t.Fatalf("request %s (%v)", h.request, err)
	}

	if err := pb.Seek(t.Context(), 2*time.Minute); err != nil {
		t.Fatal(err)
	}
	if h.player.seeked != int64(2*time.Minute) {
		t.Errorf("player seeked to %d", h.player.seeked)
	}
	h.player.pb.Position(int64(3*time.Minute), int64(24*time.Minute))
	h.player.pb.Ended("error", "source error")

	var kinds []player.EventKind
	for e := range pb.Events() {
		kinds = append(kinds, e.Kind)
	}
	if len(kinds) == 0 || kinds[len(kinds)-1] != player.EventEndFile {
		t.Errorf("events %v", kinds)
	}
	if err := pb.Wait(); err == nil || !strings.Contains(err.Error(), "source error") {
		t.Errorf("Wait = %v", err)
	}
	if st := pb.State(); st.Position != 3*time.Minute || st.Duration != 24*time.Minute {
		t.Errorf("state %+v", st)
	}
}

func TestStatusJSONNamesTheKindAndError(t *testing.T) {
	got := statusJSON(session.Status{Kind: session.StatusProviderFailed, Provider: "senshi", Err: errors.New("no streams")})
	var s struct {
		Kind, Provider, Err string
	}
	if err := json.Unmarshal([]byte(got), &s); err != nil {
		t.Fatal(err)
	}
	if s.Kind != "provider_failed" || s.Provider != "senshi" || s.Err != "no streams" {
		t.Errorf("status %s", got)
	}
}

// Every status the session sends has a name the app knows.
func TestEveryStatusKindIsNamed(t *testing.T) {
	for k := session.StatusResolving; k <= session.StatusSkipPrompt; k++ {
		if statusKinds[k] == "" {
			t.Errorf("status kind %d has no name", k)
		}
	}
}
