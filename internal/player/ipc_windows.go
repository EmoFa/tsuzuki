//go:build windows

package player

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"

	"github.com/Microsoft/go-winio"
)

func ipcAddress() (string, error) {
	b := make([]byte, 6)
	rand.Read(b)
	return fmt.Sprintf(`\\.\pipe\anitui-mpv-%d-%s`, os.Getpid(), hex.EncodeToString(b)), nil
}

func dialIPC(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, addr)
}

// Named pipes disappear with the server; nothing to clean up.
func cleanupIPC(string) {}
