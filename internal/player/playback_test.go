package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeMpv answers commands on one end of a pipe and lets tests inject events.
type fakeMpv struct {
	t        *testing.T
	conn     net.Conn
	commands chan []any
	// reply decides the response for a command; nil means success with no data.
	reply func(cmd []any) (data any, errStr string)
}

func startFake(t *testing.T) (*fakeMpv, *Playback) {
	t.Helper()
	client, server := net.Pipe()
	f := &fakeMpv{t: t, conn: server, commands: make(chan []any, 32)}
	go f.serve()
	pb := newPlayback(client, nil)
	t.Cleanup(func() { pb.Close() })
	return f, pb
}

func (f *fakeMpv) serve() {
	sc := bufio.NewScanner(f.conn)
	for sc.Scan() {
		var req struct {
			Command   []any `json:"command"`
			RequestID int64 `json:"request_id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			f.t.Errorf("bad request %q: %v", sc.Text(), err)
			return
		}
		f.commands <- req.Command
		var data any
		errStr := "success"
		if f.reply != nil {
			if d, e := f.reply(req.Command); e != "" {
				errStr = e
			} else {
				data = d
			}
		}
		f.send(map[string]any{"request_id": req.RequestID, "error": errStr, "data": data})
	}
}

func (f *fakeMpv) send(v any) {
	b, _ := json.Marshal(v)
	f.conn.Write(append(b, '\n'))
}

func (f *fakeMpv) property(name string, value any) {
	f.send(map[string]any{"event": "property-change", "id": 1, "name": name, "data": value})
}

func next(t *testing.T, pb *Playback) Event {
	t.Helper()
	select {
	case e, ok := <-pb.Events():
		if !ok {
			t.Fatal("events channel closed")
		}
		return e
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	return Event{}
}

func TestCommandRepliesAndErrors(t *testing.T) {
	f, pb := startFake(t)
	f.reply = func(cmd []any) (any, string) {
		switch cmd[0] {
		case "get_property":
			return 42.5, ""
		case "keybind":
			return nil, "invalid parameter"
		}
		return nil, ""
	}
	ctx := context.Background()

	data, err := pb.Command(ctx, "get_property", "time-pos")
	if err != nil || string(data) != "42.5" {
		t.Fatalf("data=%s err=%v", data, err)
	}
	if err := pb.BindKey(ctx, "TAB", "skip"); err == nil || !strings.Contains(err.Error(), "invalid parameter") {
		t.Fatalf("err = %v", err)
	}
	if err := pb.Seek(ctx, 90*time.Second); err != nil {
		t.Fatal(err)
	}
	var seek []any
	for cmd := range f.commands {
		if cmd[0] == "seek" {
			seek = cmd
			break
		}
	}
	if seek[1] != 90.0 || seek[2] != "absolute" {
		t.Fatalf("seek = %v", seek)
	}
}

func TestBindKeyQuotesMessage(t *testing.T) {
	f, pb := startFake(t)
	if err := pb.BindKey(context.Background(), "Ctrl+s", `skip "op"`); err != nil {
		t.Fatal(err)
	}
	cmd := <-f.commands
	if cmd[0] != "keybind" || cmd[1] != "Ctrl+s" || cmd[2] != `script-message "skip \"op\""` {
		t.Fatalf("cmd = %v", cmd)
	}
}

func TestEventsAndState(t *testing.T) {
	f, pb := startFake(t)

	f.property("duration", 1440.0)
	if e := next(t, pb); e.Kind != EventDuration || e.Duration != 24*time.Minute {
		t.Fatalf("event = %+v", e)
	}
	f.property("time-pos", 10.5)
	if e := next(t, pb); e.Kind != EventPosition || e.Position != 10500*time.Millisecond || e.Duration != 24*time.Minute {
		t.Fatalf("event = %+v", e)
	}
	f.property("time-pos", nil) // unavailable: ignored
	f.property("pause", true)
	if e := next(t, pb); e.Kind != EventPause || !e.Paused {
		t.Fatalf("event = %+v", e)
	}
	f.send(map[string]any{"event": "client-message", "args": []string{"skip"}})
	if e := next(t, pb); e.Kind != EventMessage || len(e.Args) != 1 || e.Args[0] != "skip" {
		t.Fatalf("event = %+v", e)
	}
	f.send(map[string]any{"event": "end-file", "reason": "eof"})
	if e := next(t, pb); e.Kind != EventEndFile || e.Reason != "eof" {
		t.Fatalf("event = %+v", e)
	}

	st := pb.State()
	if st.Position != 10500*time.Millisecond || !st.Paused || st.EndReason != "eof" {
		t.Fatalf("state = %+v", st)
	}
}

func TestPositionThrottleAndSeekForcesUpdate(t *testing.T) {
	f, pb := startFake(t)
	f.property("time-pos", 1.0)
	if e := next(t, pb); e.Position != time.Second {
		t.Fatalf("event = %+v", e)
	}
	// Within the throttle window: dropped from events but kept in state.
	f.property("time-pos", 1.04)
	f.send(map[string]any{"event": "playback-restart"})
	if e := next(t, pb); e.Kind != EventSeek {
		t.Fatalf("event = %+v", e)
	}
	// The first position after a seek is delivered immediately.
	f.property("time-pos", 300.0)
	if e := next(t, pb); e.Kind != EventPosition || e.Position != 5*time.Minute {
		t.Fatalf("event = %+v", e)
	}
}

func TestSlowConsumerDoesNotBlockCommands(t *testing.T) {
	f, pb := startFake(t)
	// Flood events nobody reads, then issue a command.
	for i := range 500 {
		f.send(map[string]any{"event": "client-message", "args": []string{"m", string(rune('a' + i%26))}})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := pb.Command(ctx, "get_property", "pause"); err != nil {
		t.Fatalf("command blocked behind unread events: %v", err)
	}
}

func TestConnectionCloseEndsEventsAndCommands(t *testing.T) {
	f, pb := startFake(t)
	f.send(map[string]any{"event": "end-file", "reason": "quit"})
	f.conn.Close()

	var got []Event
	for e := range pb.Events() {
		got = append(got, e)
	}
	if len(got) != 1 || got[0].Reason != "quit" {
		t.Fatalf("events = %+v", got)
	}
	if _, err := pb.Command(context.Background(), "get_property", "pause"); !errors.Is(err, ErrClosed) {
		t.Fatalf("err = %v", err)
	}
	if err := pb.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildArgs(t *testing.T) {
	args := buildArgs(Request{
		URL:       "http://127.0.0.1/s/x/uwu.m3u8",
		Title:     "Frieren - Episode 1",
		Start:     83500 * time.Millisecond,
		Headers:   map[string]string{"Referer": "https://a/", "Origin": "https://a,b"},
		Subtitles: []string{"http://x/sub_en.ass"},
		SubLangs:  []string{"es", "en"},
		HideSubs:  true,
		AudioLang: "ja",
	}, "/run/tsuzuki.sock", Options{ExtraArgs: []string{"--fullscreen"}})

	want := []string{
		"--input-ipc-server=/run/tsuzuki.sock",
		"--keep-open=no",
		"--idle=no",
		"--no-terminal",
		"--force-media-title=Frieren - Episode 1",
		"--start=83.500",
		"--http-header-fields-append=Origin: https://a,b",
		"--http-header-fields-append=Referer: https://a/",
		"--sub-files-append=http://x/sub_en.ass",
		"--slang=es,en",
		"--sub-visibility=no",
		"--alang=ja",
		"--fullscreen",
		"--",
		"http://127.0.0.1/s/x/uwu.m3u8",
	}
	if strings.Join(args, "\n") != strings.Join(want, "\n") {
		t.Fatalf("args:\n%s\nwant:\n%s", strings.Join(args, "\n"), strings.Join(want, "\n"))
	}
}
