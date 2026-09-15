// Package discord shows what's playing as Discord Rich Presence, talking to the
// local Discord client over its IPC socket (or named pipe on Windows).
package discord

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// Frame opcodes of the Discord IPC protocol.
const (
	opHandshake = 0
	opFrame     = 1
	opClose     = 2
	opPing      = 3
	opPong      = 4
)

// maxFrame bounds frames read from Discord.
const maxFrame = 1 << 20

// ErrNotRunning means no Discord client is listening.
var ErrNotRunning = errors.New("discord is not running")

// Error is an error reported by Discord, such as an invalid client ID.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("discord: %s (code %d)", e.Message, e.Code) }

// User is the Discord account the client is logged into.
type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Global   string `json:"global_name"`
}

// Client is a connection to the local Discord client. It is safe for
// concurrent use; calls are serialized.
type Client struct {
	conn net.Conn
	mu   sync.Mutex
	User User
}

// Dial connects to the first Discord IPC endpoint that accepts and completes
// the handshake for clientID.
func Dial(ctx context.Context, clientID string) (*Client, error) {
	var lastErr error = ErrNotRunning
	for _, addr := range ipcAddresses() {
		conn, err := dialIPC(ctx, addr)
		if err != nil {
			continue
		}
		c := &Client{conn: conn}
		if err := c.handshake(ctx, clientID); err != nil {
			conn.Close()
			var de *Error
			if errors.As(err, &de) {
				return nil, err // Discord answered; another socket won't say otherwise
			}
			lastErr = fmt.Errorf("%s: %w", addr, err)
			continue
		}
		return c, nil
	}
	return nil, lastErr
}

// newClient wraps an established connection (tests).
func newClient(ctx context.Context, conn net.Conn, clientID string) (*Client, error) {
	c := &Client{conn: conn}
	if err := c.handshake(ctx, clientID); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) handshake(ctx context.Context, clientID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.deadline(ctx)()
	if err := c.write(opHandshake, map[string]any{"v": 1, "client_id": clientID}); err != nil {
		return err
	}
	for {
		msg, err := c.read()
		if err != nil {
			return err
		}
		if msg.Evt == "READY" {
			var data struct {
				User User `json:"user"`
			}
			json.Unmarshal(msg.Data, &data)
			c.User = data.User
			return nil
		}
		if msg.Evt == "ERROR" {
			return msg.err()
		}
	}
}

// Activity is a Rich Presence activity. See Discord's SET_ACTIVITY docs for
// field limits; SetActivity trims text to fit.
type Activity struct {
	Type              ActivityType `json:"type"`
	StatusDisplayType int          `json:"status_display_type,omitempty"`
	Details           string       `json:"details,omitempty"`
	DetailsURL        string       `json:"details_url,omitempty"`
	State             string       `json:"state,omitempty"`
	Timestamps        *Timestamps  `json:"timestamps,omitempty"`
	Assets            *Assets      `json:"assets,omitempty"`
	Buttons           []Button     `json:"buttons,omitempty"`
}

type ActivityType int

const (
	Playing   ActivityType = 0
	Listening ActivityType = 2
	Watching  ActivityType = 3
)

// StatusDisplayDetails shows Details (rather than the application name) in
// the member list: "Watching <title>".
const StatusDisplayDetails = 2

// Timestamps are unix milliseconds. With both set Discord shows a progress bar.
type Timestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type Assets struct {
	LargeImage string `json:"large_image,omitempty"` // asset key or https URL
	LargeText  string `json:"large_text,omitempty"`
}

type Button struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// SetActivity shows a, or clears the presence when a is nil.
func (c *Client) SetActivity(ctx context.Context, a *Activity) error {
	args := map[string]any{"pid": os.Getpid()}
	if a != nil {
		args["activity"] = a.trimmed()
	}
	_, err := c.command(ctx, "SET_ACTIVITY", args)
	var de *Error
	if a != nil && errors.As(err, &de) && (a.StatusDisplayType != 0 || a.DetailsURL != "") {
		// Older Discord clients reject fields they don't know; retry without them.
		basic := *a
		basic.StatusDisplayType, basic.DetailsURL = 0, ""
		args["activity"] = basic.trimmed()
		_, err = c.command(ctx, "SET_ACTIVITY", args)
	}
	return err
}

// Close closes the connection; Discord clears the presence it set.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.conn.SetWriteDeadline(time.Now().Add(time.Second))
	c.write(opClose, map[string]any{})
	return c.conn.Close()
}

type message struct {
	Cmd   string          `json:"cmd"`
	Evt   string          `json:"evt"`
	Nonce string          `json:"nonce"`
	Data  json.RawMessage `json:"data"`
	// Set on opClose frames.
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (m message) err() error {
	var e Error
	if json.Unmarshal(m.Data, &e) != nil || e.Message == "" {
		e = Error{Code: m.Code, Message: m.Message}
	}
	return &e
}

func (c *Client) command(ctx context.Context, cmd string, args any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.deadline(ctx)()
	nonce := newNonce()
	if err := c.write(opFrame, map[string]any{"cmd": cmd, "args": args, "nonce": nonce}); err != nil {
		return nil, err
	}
	for {
		msg, err := c.read()
		if err != nil {
			return nil, err
		}
		if msg.Nonce != nonce {
			continue // an event or a late reply to an earlier command
		}
		if msg.Evt == "ERROR" {
			return nil, msg.err()
		}
		return msg.Data, nil
	}
}

// deadline applies ctx's deadline (or a default) to the connection and returns
// a func that clears it.
func (c *Client) deadline(ctx context.Context) func() {
	d, ok := ctx.Deadline()
	if !ok {
		d = time.Now().Add(5 * time.Second)
	}
	c.conn.SetDeadline(d)
	stop := context.AfterFunc(ctx, func() { c.conn.SetDeadline(time.Now()) })
	return func() {
		stop()
		c.conn.SetDeadline(time.Time{})
	}
}

func (c *Client) write(op uint32, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	buf := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint32(buf[0:], op)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(body)))
	_, err = c.conn.Write(append(buf, body...))
	return err
}

// read returns the next frame, answering pings along the way.
func (c *Client) read() (message, error) {
	for {
		var header [8]byte
		if _, err := io.ReadFull(c.conn, header[:]); err != nil {
			return message{}, err
		}
		op := binary.LittleEndian.Uint32(header[0:])
		n := binary.LittleEndian.Uint32(header[4:])
		if n > maxFrame {
			return message{}, fmt.Errorf("discord frame too large (%d bytes)", n)
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(c.conn, body); err != nil {
			return message{}, err
		}
		switch op {
		case opPing:
			if err := c.writeRaw(opPong, body); err != nil {
				return message{}, err
			}
			continue
		case opPong:
			continue
		}
		var msg message
		if err := json.Unmarshal(body, &msg); err != nil {
			return message{}, fmt.Errorf("discord frame: %w", err)
		}
		if op == opClose {
			return message{}, msg.err()
		}
		return msg, nil
	}
}

func (c *Client) writeRaw(op uint32, body []byte) error {
	buf := make([]byte, 8, 8+len(body))
	binary.LittleEndian.PutUint32(buf[0:], op)
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(body)))
	_, err := c.conn.Write(append(buf, body...))
	return err
}

func newNonce() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// trimmed returns a copy with text cut to Discord's limits.
func (a *Activity) trimmed() *Activity {
	out := *a
	out.Details = clip(out.Details, 128)
	out.State = clip(out.State, 128)
	if out.Assets != nil {
		assets := *out.Assets
		assets.LargeText = clip(assets.LargeText, 128)
		if len(assets.LargeImage) > 300 {
			assets.LargeImage = ""
		}
		out.Assets = &assets
	}
	if len(out.DetailsURL) > 256 {
		out.DetailsURL = ""
	}
	out.Buttons = nil
	for _, b := range a.Buttons {
		if len(out.Buttons) < 2 && len(b.URL) <= 512 {
			out.Buttons = append(out.Buttons, Button{Label: clip(b.Label, 32), URL: b.URL})
		}
	}
	return &out
}

// clip shortens s to at most max bytes on a rune boundary, adding an ellipsis.
// Discord rejects text shorter than 2 characters, so such text is padded.
func clip(s string, max int) string {
	if s == "" {
		return s
	}
	if len(s) > max {
		cut := max - len("…")
		for cut > 0 && !utf8RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	if len([]rune(s)) < 2 {
		s += " "
	}
	return s
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
