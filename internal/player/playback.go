package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// positionInterval throttles position events; mpv reports time-pos every frame.
const positionInterval = 250 * time.Millisecond

var ErrClosed = errors.New("mpv connection closed")

type EventKind int

const (
	EventPosition EventKind = iota + 1 // Position changed (throttled)
	EventDuration                      // Duration became known or changed
	EventPause                         // Paused toggled
	EventSeek                          // playback resumed after a seek
	EventEndFile                       // the file stopped; Reason says why
	EventMessage                       // a key bound with BindKey was pressed; Args holds its message
)

type Event struct {
	Kind     EventKind
	Position time.Duration
	Duration time.Duration
	Paused   bool
	Reason   string   // EventEndFile: "eof", "stop", "quit", "error", "redirect"
	Args     []string // EventMessage
}

// State is the latest playback state reported by mpv.
type State struct {
	Position time.Duration
	Duration time.Duration
	Paused   bool
	// EndReason is set once the file has ended ("eof" means it played to the end).
	EndReason string
}

type Playback struct {
	conn net.Conn
	proc *process // nil in tests

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan ipcMessage
	state    State
	lastPos  time.Time
	forcePos bool

	queue    *eventQueue
	readDone chan struct{}
	closing  chan struct{}
	closeOne sync.Once
}

type ipcMessage struct {
	RequestID int64           `json:"request_id"`
	Error     string          `json:"error"`
	Data      json.RawMessage `json:"data"`
	Event     string          `json:"event"`
	Name      string          `json:"name"`
	Reason    string          `json:"reason"`
	Args      []string        `json:"args"`
}

func newPlayback(conn net.Conn, proc *process) *Playback {
	closing := make(chan struct{})
	pb := &Playback{
		conn:     conn,
		proc:     proc,
		pending:  map[int64]chan ipcMessage{},
		queue:    newEventQueue(closing),
		readDone: make(chan struct{}),
		closing:  closing,
	}
	go pb.readLoop()
	return pb
}

// Events delivers playback events. It is closed after mpv's connection ends
// and every queued event has been delivered (or Close was called).
func (pb *Playback) Events() <-chan Event { return pb.queue.out }

// State returns the most recent state, also valid after mpv has exited.
func (pb *Playback) State() State {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	return pb.state
}

// Command sends a raw mpv command and returns its data.
func (pb *Playback) Command(ctx context.Context, args ...any) (json.RawMessage, error) {
	id := pb.nextID.Add(1)
	reply := make(chan ipcMessage, 1)
	pb.mu.Lock()
	pb.pending[id] = reply
	pb.mu.Unlock()
	defer func() {
		pb.mu.Lock()
		delete(pb.pending, id)
		pb.mu.Unlock()
	}()

	line, err := json.Marshal(map[string]any{"command": args, "request_id": id})
	if err != nil {
		return nil, err
	}
	pb.writeMu.Lock()
	_, err = pb.conn.Write(append(line, '\n'))
	pb.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("mpv %v: %w", args, ErrClosed)
	}

	select {
	case msg := <-reply:
		if msg.Error != "success" {
			return nil, fmt.Errorf("mpv %v: %s", args, msg.Error)
		}
		return msg.Data, nil
	case <-pb.readDone:
		return nil, fmt.Errorf("mpv %v: %w", args, ErrClosed)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Seek jumps to an absolute position.
func (pb *Playback) Seek(ctx context.Context, pos time.Duration) error {
	_, err := pb.Command(ctx, "seek", pos.Seconds(), "absolute")
	return err
}

func (pb *Playback) SetPause(ctx context.Context, paused bool) error {
	_, err := pb.Command(ctx, "set_property", "pause", paused)
	return err
}

// ShowText displays an OSD message.
func (pb *Playback) ShowText(ctx context.Context, text string, d time.Duration) error {
	_, err := pb.Command(ctx, "show-text", text, d.Milliseconds())
	return err
}

// BindKey makes key (mpv key name, e.g. "TAB" or "Ctrl+s") emit an
// EventMessage with message as its only argument. Needs mpv 0.38+.
func (pb *Playback) BindKey(ctx context.Context, key, message string) error {
	msg, _ := json.Marshal(message)
	_, err := pb.Command(ctx, "keybind", key, "script-message "+string(msg))
	return err
}

// Quit asks mpv to exit.
func (pb *Playback) Quit(ctx context.Context) error {
	_, err := pb.Command(ctx, "quit")
	if errors.Is(err, ErrClosed) {
		return nil // mpv may close the connection before replying
	}
	return err
}

// Wait blocks until mpv exits and returns its exit error, if any.
func (pb *Playback) Wait() error {
	<-pb.readDone
	if pb.proc == nil {
		return nil
	}
	<-pb.proc.exited
	var exitErr interface{ ExitCode() int }
	if errors.As(pb.proc.err, &exitErr) {
		return fmt.Errorf("mpv exited with code %d", exitErr.ExitCode())
	}
	return pb.proc.err
}

// Close quits mpv (killing it if it doesn't exit promptly) and releases
// everything. Safe to call more than once and after mpv has exited.
func (pb *Playback) Close() error {
	pb.closeOne.Do(func() {
		close(pb.closing)
		if pb.proc != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			pb.Quit(ctx) //nolint:errcheck // killed below if it didn't quit
			cancel()
			select {
			case <-pb.proc.exited:
			case <-time.After(3 * time.Second):
				pb.proc.kill()
			}
		}
		pb.conn.Close()
		<-pb.readDone
	})
	return nil
}

// observe subscribes to the properties behind the event stream.
func (pb *Playback) observe(ctx context.Context) error {
	for i, name := range []string{"time-pos", "duration", "pause"} {
		if _, err := pb.Command(ctx, "observe_property", i+1, name); err != nil {
			return err
		}
	}
	return nil
}

func (pb *Playback) readLoop() {
	defer func() {
		close(pb.readDone)
		pb.queue.close()
	}()
	sc := bufio.NewScanner(pb.conn)
	sc.Buffer(make([]byte, 64*1024), 4<<20)
	for sc.Scan() {
		var msg ipcMessage
		if err := json.Unmarshal(sc.Bytes(), &msg); err != nil {
			slog.Warn("mpv ipc: bad message", "err", err, "line", sc.Text())
			continue
		}
		if msg.Event == "" {
			pb.mu.Lock()
			reply := pb.pending[msg.RequestID]
			pb.mu.Unlock()
			if reply != nil {
				reply <- msg
			}
			continue
		}
		pb.handleEvent(msg)
	}
}

func (pb *Playback) handleEvent(msg ipcMessage) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	switch msg.Event {
	case "property-change":
		switch msg.Name {
		case "time-pos":
			pos, ok := seconds(msg.Data)
			if !ok {
				return
			}
			pb.state.Position = pos
			now := time.Now()
			if pb.forcePos || now.Sub(pb.lastPos) >= positionInterval {
				pb.lastPos, pb.forcePos = now, false
				pb.queue.push(Event{Kind: EventPosition, Position: pos, Duration: pb.state.Duration})
			}
		case "duration":
			if d, ok := seconds(msg.Data); ok {
				pb.state.Duration = d
				pb.queue.push(Event{Kind: EventDuration, Position: pb.state.Position, Duration: d})
			}
		case "pause":
			var paused bool
			if json.Unmarshal(msg.Data, &paused) == nil {
				pb.state.Paused = paused
				pb.queue.push(Event{Kind: EventPause, Paused: paused, Position: pb.state.Position, Duration: pb.state.Duration})
			}
		}
	case "playback-restart":
		pb.forcePos = true
		pb.queue.push(Event{Kind: EventSeek, Position: pb.state.Position, Duration: pb.state.Duration})
	case "end-file":
		pb.state.EndReason = msg.Reason
		pb.queue.push(Event{Kind: EventEndFile, Reason: msg.Reason, Position: pb.state.Position, Duration: pb.state.Duration})
	case "client-message":
		pb.queue.push(Event{Kind: EventMessage, Args: msg.Args})
	}
}

// seconds decodes an mpv time property; null (unavailable) reports false.
func seconds(data json.RawMessage) (time.Duration, bool) {
	var f *float64
	if json.Unmarshal(data, &f) != nil || f == nil {
		return 0, false
	}
	return time.Duration(*f * float64(time.Second)), true
}

// eventQueue decouples the IPC reader from the consumer so a slow consumer
// never stalls command replies. Consecutive position events coalesce.
type eventQueue struct {
	out chan Event

	mu     sync.Mutex
	items  []Event
	closed bool
	wake   chan struct{}
}

// newEventQueue starts delivering to out. abandon stops delivery early once the
// consumer has gone away.
func newEventQueue(abandon <-chan struct{}) *eventQueue {
	q := &eventQueue{out: make(chan Event), wake: make(chan struct{}, 1)}
	go q.pump(abandon)
	return q
}

func (q *eventQueue) push(e Event) {
	q.mu.Lock()
	if n := len(q.items); n > 0 && e.Kind == EventPosition && q.items[n-1].Kind == EventPosition {
		q.items[n-1] = e
	} else {
		q.items = append(q.items, e)
	}
	q.mu.Unlock()
	q.signal()
}

// close lets the pump finish once queued events are delivered.
func (q *eventQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}

func (q *eventQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *eventQueue) pump(abandon <-chan struct{}) {
	defer close(q.out)
	for {
		q.mu.Lock()
		if len(q.items) == 0 {
			closed := q.closed
			q.mu.Unlock()
			if closed {
				return
			}
			select {
			case <-q.wake:
				continue
			case <-abandon:
				return
			}
		}
		e := q.items[0]
		q.items = q.items[1:]
		q.mu.Unlock()
		select {
		case q.out <- e:
		case <-abandon:
			return
		}
	}
}
