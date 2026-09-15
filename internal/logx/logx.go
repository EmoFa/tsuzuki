// Package logx sets up file logging. The TUI owns the terminal, so logs never
// go to stdout/stderr.
package logx

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// maxSize is the size at which the log is rotated to <path>.1 on startup.
const maxSize = 5 << 20

// Setup installs a slog default logger writing to path and returns a func that
// closes the file. debug lowers the level from Info to Debug.
func Setup(path string, debug bool) (func() error, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxSize {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	slog.SetDefault(New(f, debug))
	return f.Close, nil
}

func New(w io.Writer, debug bool) *slog.Logger {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}
