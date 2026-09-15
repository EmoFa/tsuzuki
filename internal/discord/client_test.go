package discord

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
)

// fakeDiscord is the server side of an IPC connection.
type fakeDiscord struct {
	t    *testing.T
	conn net.Conn
}

func (f *fakeDiscord) read() (uint32, map[string]any) {
	var h [8]byte
	if _, err := io.ReadFull(f.conn, h[:]); err != nil {
		f.t.Errorf("server read: %v", err)
		return 0, nil
	}
	body := make([]byte, binary.LittleEndian.Uint32(h[4:]))
	io.ReadFull(f.conn, body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		f.t.Errorf("server got invalid JSON %q", body)
	}
	return binary.LittleEndian.Uint32(h[:4]), m
}

func (f *fakeDiscord) send(op uint32, payload string) {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf, op)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(payload)))
	f.conn.Write(append(buf, payload...))
}

func pipe(t *testing.T) (client net.Conn, server *fakeDiscord) {
	c, s := net.Pipe()
	t.Cleanup(func() { c.Close(); s.Close() })
	return c, &fakeDiscord{t: t, conn: s}
}

const ready = `{"cmd":"DISPATCH","evt":"READY","data":{"v":1,"user":{"id":"1","username":"testuser","global_name":"Test User"}},"nonce":null}`

func TestHandshakeAndSetActivity(t *testing.T) {
	conn, srv := pipe(t)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		op, hs := srv.read()
		if op != opHandshake || hs["client_id"] != "123" || hs["v"] != float64(1) {
			t.Errorf("handshake op=%d %v", op, hs)
		}
		srv.send(opPing, `{"n":1}`) // must be answered with a pong
		if op, _ := srv.read(); op != opPong {
			t.Errorf("expected pong, got op %d", op)
		}
		srv.send(opFrame, ready)

		op, cmd := srv.read()
		args := cmd["args"].(map[string]any)
		act := args["activity"].(map[string]any)
		if op != opFrame || cmd["cmd"] != "SET_ACTIVITY" || act["type"] != float64(3) || act["details"] != "Frieren" {
			t.Errorf("set activity op=%d %v", op, cmd)
		}
		if _, ok := args["pid"]; !ok {
			t.Error("pid missing")
		}
		// An unrelated event and a stale reply come before the real one.
		srv.send(opFrame, `{"cmd":"DISPATCH","evt":"ACTIVITY_JOIN","nonce":null}`)
		srv.send(opFrame, `{"cmd":"SET_ACTIVITY","nonce":"stale","data":{}}`)
		srv.send(opFrame, `{"cmd":"SET_ACTIVITY","nonce":"`+cmd["nonce"].(string)+`","data":{}}`)

		_, clear := srv.read()
		if _, has := clear["args"].(map[string]any)["activity"]; has {
			t.Errorf("clearing sent an activity: %v", clear)
		}
		srv.send(opFrame, `{"cmd":"SET_ACTIVITY","nonce":"`+clear["nonce"].(string)+`","data":null}`)
	}()

	ctx := context.Background()
	c, err := newClient(ctx, conn, "123")
	if err != nil {
		t.Fatal(err)
	}
	if c.User.Username != "testuser" {
		t.Errorf("user = %+v", c.User)
	}
	if err := c.SetActivity(ctx, &Activity{Type: Watching, Details: "Frieren"}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetActivity(ctx, nil); err != nil {
		t.Fatal(err)
	}
	<-serverDone
}

func TestInvalidClientID(t *testing.T) {
	conn, srv := pipe(t)
	go func() {
		srv.read()
		srv.send(opClose, `{"code":4000,"message":"Invalid Client ID"}`)
	}()
	_, err := newClient(context.Background(), conn, "bad")
	var de *Error
	if !errors.As(err, &de) || de.Code != 4000 {
		t.Fatalf("err = %v", err)
	}
}

func TestRejectedFieldsFallBack(t *testing.T) {
	conn, srv := pipe(t)
	go func() {
		srv.read()
		srv.send(opFrame, ready)
		_, cmd := srv.read()
		srv.send(opFrame, `{"cmd":"SET_ACTIVITY","evt":"ERROR","nonce":"`+cmd["nonce"].(string)+`","data":{"code":4000,"message":"child \"activity\" fails because [\"status_display_type\" is not allowed]"}}`)
		_, cmd = srv.read()
		act := cmd["args"].(map[string]any)["activity"].(map[string]any)
		if _, ok := act["status_display_type"]; ok {
			t.Errorf("retry kept status_display_type: %v", act)
		}
		if _, ok := act["details_url"]; ok {
			t.Errorf("retry kept details_url: %v", act)
		}
		srv.send(opFrame, `{"cmd":"SET_ACTIVITY","nonce":"`+cmd["nonce"].(string)+`","data":{}}`)
	}()
	ctx := context.Background()
	c, err := newClient(ctx, conn, "1")
	if err != nil {
		t.Fatal(err)
	}
	err = c.SetActivity(ctx, &Activity{Type: Watching, Details: "Frieren", StatusDisplayType: StatusDisplayDetails, DetailsURL: "https://anilist.co/anime/1"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTrimmed(t *testing.T) {
	long := strings.Repeat("フリーレン", 40)
	a := (&Activity{Details: long, State: "x", Buttons: []Button{
		{Label: strings.Repeat("b", 40), URL: "https://a"}, {Label: "two", URL: "https://b"}, {Label: "three", URL: "https://c"},
	}}).trimmed()
	if len(a.Details) > 128 || !strings.HasSuffix(a.Details, "…") || !strings.HasPrefix(a.Details, "フリーレン") {
		t.Errorf("details %d bytes: %q", len(a.Details), a.Details)
	}
	for _, r := range a.Details {
		if r == '�' {
			t.Fatal("cut inside a rune")
		}
	}
	if len([]rune(a.State)) < 2 {
		t.Errorf("state %q too short for Discord", a.State)
	}
	if len(a.Buttons) != 2 || len(a.Buttons[0].Label) > 32 {
		t.Errorf("buttons = %+v", a.Buttons)
	}
}
