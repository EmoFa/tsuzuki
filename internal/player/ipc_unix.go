//go:build !windows

package player

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// maxSocketPath stays under the smallest sun_path limit (104 bytes on macOS).
const maxSocketPath = 100

func ipcAddress() (string, error) {
	b := make([]byte, 6)
	rand.Read(b)
	name := fmt.Sprintf("anitui-mpv-%d-%s.sock", os.Getpid(), hex.EncodeToString(b))
	for _, dir := range []string{os.Getenv("XDG_RUNTIME_DIR"), os.TempDir(), "/tmp"} {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, name); len(p) <= maxSocketPath {
			return p, nil
		}
	}
	return "", fmt.Errorf("no directory with a short enough path for the mpv IPC socket")
}

func dialIPC(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}

func cleanupIPC(addr string) { os.Remove(addr) }
