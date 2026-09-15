package player

import (
	"context"
	"testing"
	"time"
)

// TestRealMpv drives an actual mpv on a generated 4-second clip. Skipped when
// mpv isn't installed or with -short.
func TestRealMpv(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	if _, err := FindMpv(""); err != nil {
		t.Skip(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p := New(Options{ExtraArgs: []string{"--no-config", "--vo=null", "--ao=null"}})
	pb, err := p.Play(ctx, Request{
		URL:   "av://lavfi:testsrc=duration=4:size=160x90:rate=25",
		Title: "tsuzuki test",
		Start: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pb.Close()

	if err := pb.BindKey(ctx, "Ctrl+x", "tsuzuki-test"); err != nil {
		t.Errorf("keybind: %v", err)
	}
	title, err := pb.Command(ctx, "get_property", "force-media-title")
	if err != nil || string(title) != `"tsuzuki test"` {
		t.Errorf("title=%s err=%v", title, err)
	}

	var sawPosition, sawDuration, sawSeek bool
	seeked := false
	early := 0 // positions before --start; mpv may report one before its initial seek lands
	for e := range pb.Events() {
		switch e.Kind {
		case EventDuration:
			sawDuration = e.Duration > 3*time.Second
		case EventPosition:
			if e.Position < 900*time.Millisecond && !seeked {
				if early++; early > 2 {
					t.Errorf("--start ignored: position %v", e.Position)
				}
			}
			sawPosition = true
			if !seeked && e.Position > 1500*time.Millisecond {
				seeked = true
				if err := pb.Seek(ctx, 3*time.Second); err != nil {
					t.Errorf("seek: %v", err)
				}
			}
		case EventSeek:
			if seeked {
				sawSeek = true
			}
		case EventEndFile:
			if e.Reason != "eof" {
				t.Errorf("end reason = %q", e.Reason)
			}
		}
	}
	if err := pb.Wait(); err != nil {
		t.Errorf("wait: %v", err)
	}
	if !sawPosition || !sawDuration || !sawSeek {
		t.Errorf("position=%v duration=%v seek=%v", sawPosition, sawDuration, sawSeek)
	}
	if st := pb.State(); st.EndReason != "eof" || st.Position < 3500*time.Millisecond {
		t.Errorf("final state = %+v", st)
	}
}
