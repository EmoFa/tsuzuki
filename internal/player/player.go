// Package player launches mpv and controls it over its JSON IPC protocol.
package player

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/EmoFa/anitui/internal/procfs"
)

var ErrMpvNotFound = errors.New("mpv not found: install it (https://mpv.io/installation/) or set player.mpv_path in the config")

// connectTimeout bounds how long mpv may take to open its IPC server.
const connectTimeout = 15 * time.Second

type Options struct {
	MpvPath   string   // empty: search PATH and common install locations
	ExtraArgs []string // appended after anitui's own arguments, so they win
	// Output receives mpv's terminal output. Nil disables mpv's terminal
	// entirely, which the TUI needs.
	Output io.Writer
}

// Request describes one thing to play.
type Request struct {
	URL       string
	Title     string
	Start     time.Duration
	Headers   map[string]string // sent with every HTTP request mpv makes
	Subtitles []string          // subtitle file URLs, first is selected
	AudioLang string            // preferred audio track language
}

type Player struct {
	opts Options
}

func New(opts Options) *Player { return &Player{opts: opts} }

// FindMpv resolves the mpv binary from a configured path or well-known places.
func FindMpv(configured string) (string, error) {
	if configured != "" {
		p, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("player.mpv_path %q: %w", configured, err)
		}
		return p, nil
	}
	if p, err := exec.LookPath("mpv"); err == nil {
		return p, nil
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/opt/homebrew/bin/mpv", "/usr/local/bin/mpv", "/Applications/mpv.app/Contents/MacOS/mpv"}
	case "windows":
		if dir := os.Getenv("ProgramFiles"); dir != "" {
			candidates = append(candidates, dir+`\mpv\mpv.exe`)
		}
		if dir := os.Getenv("LocalAppData"); dir != "" {
			candidates = append(candidates, dir+`\Programs\mpv\mpv.exe`)
		}
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", ErrMpvNotFound
}

// Play starts mpv and returns once its IPC connection is ready. ctx only
// bounds startup; stop playback with Playback.Close.
func (p *Player) Play(ctx context.Context, req Request) (*Playback, error) {
	bin, err := FindMpv(p.opts.MpvPath)
	if err != nil {
		return nil, err
	}
	addr, err := ipcAddress()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(bin, buildArgs(req, addr, p.opts)...)
	if p.opts.Output != nil {
		cmd.Stdout, cmd.Stderr = p.opts.Output, p.opts.Output
	}
	procfs.DieWithParent(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting mpv: %w", err)
	}
	slog.Debug("mpv started", "pid", cmd.Process.Pid, "ipc", addr)

	proc := &process{cmd: cmd, exited: make(chan struct{}), addr: addr}
	go func() {
		proc.err = cmd.Wait()
		cleanupIPC(addr)
		close(proc.exited)
	}()

	conn, err := connect(ctx, addr, proc.exited)
	if err != nil {
		proc.kill()
		return nil, err
	}
	pb := newPlayback(conn, proc)
	if err := pb.observe(ctx); err != nil {
		pb.Close()
		return nil, err
	}
	return pb, nil
}

// connect retries until mpv's IPC server accepts, mpv exits, or the timeout.
func connect(ctx context.Context, addr string, exited <-chan struct{}) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	for {
		conn, err := dialIPC(ctx, addr)
		if err == nil {
			return conn, nil
		}
		select {
		case <-exited:
			return nil, errors.New("mpv exited before its IPC server started (check player.extra_args)")
		case <-ctx.Done():
			return nil, fmt.Errorf("connecting to mpv IPC at %s: %w", addr, err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func buildArgs(req Request, ipcAddr string, opts Options) []string {
	args := []string{"--input-ipc-server=" + ipcAddr}
	if opts.Output == nil {
		args = append(args, "--no-terminal")
	}
	if req.Title != "" {
		args = append(args, "--force-media-title="+req.Title)
	}
	if req.Start > 0 {
		args = append(args, "--start="+strconv.FormatFloat(req.Start.Seconds(), 'f', 3, 64))
	}
	keys := make([]string, 0, len(req.Headers))
	for k := range req.Headers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		// The -append form takes one value verbatim, so commas are safe.
		args = append(args, "--http-header-fields-append="+k+": "+req.Headers[k])
	}
	for _, s := range req.Subtitles {
		args = append(args, "--sub-files-append="+s)
	}
	if req.AudioLang != "" {
		args = append(args, "--alang="+req.AudioLang)
	}
	args = append(args, opts.ExtraArgs...)
	return append(args, "--", req.URL)
}

// process tracks the mpv child.
type process struct {
	cmd    *exec.Cmd
	addr   string
	exited chan struct{}
	err    error // valid after exited is closed
}

func (p *process) kill() {
	if p == nil {
		return
	}
	p.cmd.Process.Kill()
	<-p.exited
}
