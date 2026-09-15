package discord

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"sync"
	"time"
)

// Setter is the part of Client a Presence uses.
type Setter interface {
	SetActivity(ctx context.Context, a *Activity) error
	Close() error
}

// DialFunc connects to Discord.
type DialFunc func(ctx context.Context) (Setter, error)

// Presence keeps Discord showing the latest activity. It connects lazily,
// reconnects with backoff when Discord isn't running or restarts, coalesces
// rapid updates to respect Discord's rate limit, and briefly delays clearing so
// moving to the next episode doesn't flicker. Methods never block on Discord.
type Presence struct {
	dial DialFunc

	// Tunables; tests shorten them.
	clearDelay time.Duration
	minBackoff time.Duration
	maxBackoff time.Duration
	burst      int           // updates allowed at once
	refill     time.Duration // time to regain one update

	mu      sync.Mutex
	want    *Activity
	changed chan struct{}
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func NewPresence(dial DialFunc) *Presence {
	p := &Presence{
		dial:       dial,
		clearDelay: 5 * time.Second,
		minBackoff: 5 * time.Second,
		maxBackoff: time.Minute,
		burst:      5,
		refill:     4 * time.Second, // Discord allows 5 updates per 20s
		changed:    make(chan struct{}, 1),
		done:       make(chan struct{}),
		stopped:    make(chan struct{}),
	}
	return p
}

// Start runs the update loop until Close.
func (p *Presence) Start() { go p.loop() }

// Set shows a; nil clears the presence (after a short delay).
func (p *Presence) Set(a *Activity) {
	p.mu.Lock()
	p.want = a
	p.mu.Unlock()
	select {
	case p.changed <- struct{}{}:
	default:
	}
}

// Close clears the presence and disconnects, waiting at most a couple of seconds.
func (p *Presence) Close() {
	p.once.Do(func() { close(p.done) })
	select {
	case <-p.stopped:
	case <-time.After(3 * time.Second):
	}
}

func (p *Presence) loop() {
	defer close(p.stopped)
	var (
		conn       Setter
		sent       *Activity
		retryAt    time.Time
		backoff    = p.minBackoff
		tokens     = p.burst
		lastRefill = time.Now()
		clearAfter time.Time
		timer      = time.NewTimer(time.Hour)
	)
	defer timer.Stop()
	disconnect := func() {
		if conn != nil {
			conn.Close()
			conn = nil
		}
	}
	defer disconnect()

	for {
		p.mu.Lock()
		want := p.want
		p.mu.Unlock()

		now := time.Now()
		for tokens < p.burst && now.Sub(lastRefill) >= p.refill {
			tokens++
			lastRefill = lastRefill.Add(p.refill)
		}
		if tokens == p.burst {
			lastRefill = now
		}

		wait := time.Hour
		switch {
		case reflect.DeepEqual(want, sent):
			clearAfter = time.Time{}
		case want == nil && clearAfter.IsZero():
			clearAfter = now.Add(p.clearDelay)
			wait = p.clearDelay
		case want == nil && now.Before(clearAfter):
			wait = clearAfter.Sub(now)
		case conn == nil && want == nil:
			sent = nil // nothing is shown without a connection
		case conn == nil && now.Before(retryAt):
			wait = retryAt.Sub(now)
		case tokens == 0:
			wait = p.refill - now.Sub(lastRefill)
		default:
			clearAfter = time.Time{}
			if conn == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				c, err := p.dial(ctx)
				cancel()
				if err != nil {
					logDial(err)
					retryAt, backoff = now.Add(backoff), min(backoff*2, p.maxBackoff)
					continue
				}
				conn, backoff = c, p.minBackoff
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := conn.SetActivity(ctx, want)
			cancel()
			tokens--
			var de *Error
			switch {
			case errors.As(err, &de):
				// Discord rejected the activity itself; retrying the same one won't help.
				slog.Info("discord rejected presence", "err", err)
			case err != nil:
				slog.Info("discord presence update failed", "err", err)
				disconnect()
				sent = nil
				retryAt, backoff = now.Add(backoff), min(backoff*2, p.maxBackoff)
				continue
			}
			sent = want
			continue
		}

		timer.Reset(wait)
		select {
		case <-p.changed:
		case <-timer.C:
		case <-p.done:
			if conn != nil && sent != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				conn.SetActivity(ctx, nil) //nolint:errcheck // best effort on exit
				cancel()
			}
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}
}

var lastDialErr struct {
	sync.Mutex
	msg string
}

// logDial logs connection failures once per distinct error: Discord not
// running is normal and would otherwise fill the log.
func logDial(err error) {
	lastDialErr.Lock()
	defer lastDialErr.Unlock()
	if err.Error() != lastDialErr.msg {
		lastDialErr.msg = err.Error()
		slog.Info("discord presence unavailable", "err", err)
	}
}
