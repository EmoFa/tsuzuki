package discord

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeSetter struct {
	mu     sync.Mutex
	sets   []*Activity
	closed int
	fail   error
	onSet  chan struct{}
}

func (f *fakeSetter) SetActivity(_ context.Context, a *Activity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sets = append(f.sets, a)
	select {
	case f.onSet <- struct{}{}:
	default:
	}
	return nil
}

func (f *fakeSetter) Close() error {
	f.mu.Lock()
	f.closed++
	f.mu.Unlock()
	return nil
}

func (f *fakeSetter) snapshot() []*Activity {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*Activity(nil), f.sets...)
}

func testPresence(dial DialFunc) *Presence {
	p := NewPresence(dial)
	p.clearDelay = 80 * time.Millisecond
	p.minBackoff, p.maxBackoff = 20*time.Millisecond, 50*time.Millisecond
	p.refill = 30 * time.Millisecond
	return p
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func act(details string) *Activity { return &Activity{Type: Watching, Details: details} }

func TestPresenceSendsLatestAndDelaysClear(t *testing.T) {
	fs := &fakeSetter{}
	dials := 0
	p := testPresence(func(context.Context) (Setter, error) { dials++; return fs, nil })
	p.Start()

	p.Set(act("one"))
	eventually(t, "first activity", func() bool { return len(fs.snapshot()) == 1 })

	// Stopping then quickly starting the next episode never clears.
	p.Set(nil)
	time.Sleep(20 * time.Millisecond)
	p.Set(act("two"))
	eventually(t, "second activity", func() bool { return len(fs.snapshot()) == 2 })
	time.Sleep(150 * time.Millisecond)
	if sets := fs.snapshot(); len(sets) != 2 || sets[1].Details != "two" {
		t.Fatalf("sets = %v", sets)
	}

	// A clear that stands is sent after the delay.
	p.Set(nil)
	time.Sleep(30 * time.Millisecond)
	if len(fs.snapshot()) != 2 {
		t.Fatal("cleared before the delay")
	}
	eventually(t, "clear", func() bool { s := fs.snapshot(); return len(s) == 3 && s[2] == nil })

	p.Close()
	if dials != 1 {
		t.Errorf("dials = %d", dials)
	}
	if fs.closed != 1 {
		t.Errorf("closed = %d", fs.closed)
	}
}

func TestPresenceRateLimitCoalesces(t *testing.T) {
	fs := &fakeSetter{}
	p := testPresence(func(context.Context) (Setter, error) { return fs, nil })
	p.burst, p.refill = 2, 100*time.Millisecond
	p.Start()
	defer p.Close()

	for _, d := range []string{"a", "b", "c", "d", "e", "f"} {
		p.Set(act(d))
		time.Sleep(2 * time.Millisecond)
	}
	eventually(t, "latest activity", func() bool {
		s := fs.snapshot()
		return len(s) > 0 && s[len(s)-1].Details == "f"
	})
	if n := len(fs.snapshot()); n > 3 {
		t.Errorf("%d updates sent for a burst of 2", n)
	}
}

func TestPresenceReconnects(t *testing.T) {
	fs := &fakeSetter{}
	var mu sync.Mutex
	attempts := 0
	p := testPresence(func(context.Context) (Setter, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts < 3 {
			return nil, ErrNotRunning
		}
		return fs, nil
	})
	p.Start()
	defer p.Close()

	p.Set(act("one"))
	eventually(t, "connect after Discord starts", func() bool { return len(fs.snapshot()) == 1 })

	// The connection breaks (Discord restarted): the next update reconnects.
	fs.mu.Lock()
	fs.fail = errors.New("broken pipe")
	fs.mu.Unlock()
	p.Set(act("two"))
	eventually(t, "disconnect", func() bool { fs.mu.Lock(); defer fs.mu.Unlock(); return fs.closed == 1 })
	fs.mu.Lock()
	fs.fail = nil
	fs.mu.Unlock()
	eventually(t, "resend after reconnect", func() bool {
		s := fs.snapshot()
		return len(s) == 2 && s[1].Details == "two"
	})
}

func TestPresenceRejectedActivityNotRetried(t *testing.T) {
	fs := &fakeSetter{fail: &Error{Code: 4000, Message: "bad"}}
	calls := 0
	p := testPresence(func(context.Context) (Setter, error) { calls++; return fs, nil })
	p.Start()
	p.Set(act("x"))
	time.Sleep(150 * time.Millisecond)
	p.Close()
	if calls != 1 || fs.closed != 1 {
		t.Errorf("dials = %d, closed = %d (rejections should keep the connection)", calls, fs.closed)
	}
}

func TestPresenceNoDiscordStaysQuiet(t *testing.T) {
	p := testPresence(func(context.Context) (Setter, error) { return nil, ErrNotRunning })
	p.Start()
	p.Set(act("x"))
	p.Set(nil)
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	p.Close()
	if time.Since(start) > time.Second {
		t.Error("Close blocked")
	}
}
