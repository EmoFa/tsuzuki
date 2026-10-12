package player

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Controller is a player outside tsuzuki that a Remote drives, such as the
// Android app's. Its methods may be called from any goroutine.
type Controller interface {
	Seek(pos time.Duration) error
	ShowText(text string, d time.Duration) error
	AddSubtitle(url, title, lang string) error
	// BindKey offers an action, such as an on-screen button, that reports
	// message back through Remote.Message when used. key is mpv's name for
	// the desktop key and only a hint.
	BindKey(key, message string) error
	// Stop ends playback. The controller still reports Ended afterwards.
	Stop() error
}

// remoteStopWait is how long Close waits for the controller to report the end
// after asking it to stop.
const remoteStopWait = 3 * time.Second

// Remote is a Playback whose player reports to it, instead of tsuzuki
// watching an mpv process. The controller calls Position, Duration, Paused,
// Seeked and Message as playback goes, and Ended exactly once at the end.
type Remote struct {
	ctrl Controller

	mu    sync.Mutex
	state State
	err   error
	ended bool

	queue     *eventQueue
	closing   chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func NewRemote(ctrl Controller) *Remote {
	closing := make(chan struct{})
	return &Remote{ctrl: ctrl, queue: newEventQueue(closing), closing: closing, done: make(chan struct{})}
}

// update changes the state and queues the event it makes, unless playback has
// already ended.
func (r *Remote) update(change func(*State) Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return
	}
	r.queue.push(change(&r.state))
}

// Position reports the playback position and the episode's length (0 when
// not yet known).
func (r *Remote) Position(pos, dur time.Duration) {
	r.update(func(s *State) Event {
		s.Position = pos
		if dur > 0 {
			s.Duration = dur
		}
		return Event{Kind: EventPosition, Position: pos, Duration: s.Duration}
	})
}

// Duration reports the episode's length once it's known.
func (r *Remote) Duration(dur time.Duration) {
	r.update(func(s *State) Event {
		s.Duration = dur
		return Event{Kind: EventDuration, Position: s.Position, Duration: dur}
	})
}

func (r *Remote) Paused(paused bool) {
	r.update(func(s *State) Event {
		s.Paused = paused
		return Event{Kind: EventPause, Paused: paused, Position: s.Position, Duration: s.Duration}
	})
}

// Seeked reports that playback resumed at pos after a seek.
func (r *Remote) Seeked(pos time.Duration) {
	r.update(func(s *State) Event {
		s.Position = pos
		return Event{Kind: EventSeek, Position: pos, Duration: s.Duration}
	})
}

// Message reports an action offered with BindKey being used.
func (r *Remote) Message(message string) {
	r.update(func(s *State) Event {
		return Event{Kind: EventMessage, Args: []string{message}}
	})
}

// Ended reports that playback is over, with mpv's reasons: "eof" when it
// played to the end, "quit" when the user left, "error" when it failed (err
// says why). Later reports are ignored.
func (r *Remote) Ended(reason string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ended {
		return
	}
	r.ended = true
	r.state.EndReason, r.err = reason, err
	r.queue.push(Event{Kind: EventEndFile, Reason: reason, Position: r.state.Position, Duration: r.state.Duration})
	r.queue.close()
	close(r.done)
}

func (r *Remote) Events() <-chan Event { return r.queue.out }

func (r *Remote) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

// Wait blocks until playback ends.
func (r *Remote) Wait() error {
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// Close stops playback, and stops delivering events.
func (r *Remote) Close() error {
	r.closeOnce.Do(func() {
		close(r.closing)
		select {
		case <-r.done:
			return
		default:
		}
		r.ctrl.Stop() //nolint:errcheck // ended below if it doesn't report back
		select {
		case <-r.done:
		case <-time.After(remoteStopWait):
			r.Ended("quit", nil)
		}
	})
	return nil
}

// errEnded is returned for commands sent once playback is over.
var errEnded = errors.New("player: playback has ended")

// command runs f unless playback has ended or ctx is done.
func (r *Remote) command(ctx context.Context, f func() error) error {
	select {
	case <-r.done:
		return errEnded
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return f()
}

func (r *Remote) Seek(ctx context.Context, pos time.Duration) error {
	return r.command(ctx, func() error { return r.ctrl.Seek(pos) })
}

func (r *Remote) ShowText(ctx context.Context, text string, d time.Duration) error {
	return r.command(ctx, func() error { return r.ctrl.ShowText(text, d) })
}

func (r *Remote) AddSubtitle(ctx context.Context, url, title, lang string) error {
	return r.command(ctx, func() error { return r.ctrl.AddSubtitle(url, title, lang) })
}

func (r *Remote) BindKey(ctx context.Context, key, message string) error {
	return r.command(ctx, func() error { return r.ctrl.BindKey(key, message) })
}
