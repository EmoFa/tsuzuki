package player

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeController struct {
	mu      sync.Mutex
	calls   []string
	onStop  func()
	stopped bool
}

func (c *fakeController) record(call string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
	return nil
}

func (c *fakeController) Seek(pos time.Duration) error { return c.record("seek " + pos.String()) }
func (c *fakeController) ShowText(text string, _ time.Duration) error {
	return c.record("text " + text)
}
func (c *fakeController) AddSubtitle(url, _, _ string) error { return c.record("sub " + url) }
func (c *fakeController) BindKey(_, message string) error    { return c.record("bind " + message) }
func (c *fakeController) Stop() error {
	c.mu.Lock()
	c.stopped = true
	c.mu.Unlock()
	if c.onStop != nil {
		c.onStop()
	}
	return nil
}

// drain collects events until the stream closes, as the session does.
func drain(t *testing.T, r *Remote) []Event {
	t.Helper()
	var got []Event
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-r.Events():
			if !ok {
				return got
			}
			got = append(got, e)
		case <-timeout:
			t.Fatalf("events never closed; got %+v", got)
		}
	}
}

// What the player reports reaches the session in order, and the end of the
// episode ends the stream of events.
func TestRemoteDeliversReportsUntilTheEnd(t *testing.T) {
	r := NewRemote(&fakeController{})
	go func() {
		r.Duration(24 * time.Minute)
		r.Paused(true)
		r.Seeked(90 * time.Second)
		r.Message("tsuzuki-skip")
		r.Position(23*time.Minute, 0)
		r.Ended("eof", nil)
		r.Position(time.Hour, 0) // after the end: ignored
	}()
	got := drain(t, r)
	var kinds []EventKind
	for _, e := range got {
		kinds = append(kinds, e.Kind)
	}
	want := []EventKind{EventDuration, EventPause, EventSeek, EventMessage, EventPosition, EventEndFile}
	if len(kinds) != len(want) {
		t.Fatalf("events %v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("events %v, want %v", kinds, want)
		}
	}
	if err := r.Wait(); err != nil {
		t.Fatal(err)
	}
	st := r.State()
	if st.EndReason != "eof" || st.Position != 23*time.Minute || st.Duration != 24*time.Minute || !st.Paused {
		t.Errorf("state = %+v", st)
	}
	if got[4].Duration != 24*time.Minute {
		t.Errorf("a position without a length lost the known one: %+v", got[4])
	}
}

func TestRemoteReportsAFailure(t *testing.T) {
	r := NewRemote(&fakeController{})
	boom := errors.New("source error")
	r.Ended("error", boom)
	r.Ended("eof", nil) // only the first end counts
	if err := r.Wait(); !errors.Is(err, boom) {
		t.Errorf("Wait = %v", err)
	}
	if st := r.State(); st.EndReason != "error" {
		t.Errorf("reason = %q", st.EndReason)
	}
}

// Closing asks the player to stop and waits for it to say it has.
func TestRemoteCloseStopsThePlayer(t *testing.T) {
	c := &fakeController{}
	r := NewRemote(c)
	c.onStop = func() { go r.Ended("quit", nil) }
	r.Close()
	if !c.stopped {
		t.Error("the player wasn't told to stop")
	}
	if err := r.Wait(); err != nil || r.State().EndReason != "quit" {
		t.Errorf("Wait = %v, state %+v", err, r.State())
	}
	if err := r.Seek(t.Context(), time.Second); !errors.Is(err, errEnded) {
		t.Errorf("seek after the end = %v", err)
	}
}

// A player that never answers can't hang the session.
func TestRemoteCloseGivesUpOnASilentPlayer(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the stop timeout")
	}
	r := NewRemote(&fakeController{})
	start := time.Now()
	r.Close()
	if err := r.Wait(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < remoteStopWait || elapsed > remoteStopWait+time.Second {
		t.Errorf("gave up after %s", elapsed)
	}
}

func TestRemoteCommandsReachThePlayer(t *testing.T) {
	c := &fakeController{}
	r := NewRemote(c)
	ctx := t.Context()
	r.Seek(ctx, 5*time.Second)
	r.ShowText(ctx, "Skipped opening", time.Second)
	r.AddSubtitle(ctx, "http://x/en.vtt", "English", "en")
	r.BindKey(ctx, "TAB", "tsuzuki-skip")
	want := []string{"seek 5s", "text Skipped opening", "sub http://x/en.vtt", "bind tsuzuki-skip"}
	if len(c.calls) != len(want) {
		t.Fatalf("calls %v", c.calls)
	}
	for i := range want {
		if c.calls[i] != want[i] {
			t.Fatalf("calls %v, want %v", c.calls, want)
		}
	}
}
