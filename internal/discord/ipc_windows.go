//go:build windows

package discord

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func ipcAddresses() []string {
	out := make([]string, 10)
	for i := range out {
		out[i] = fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
	}
	return out
}

func dialIPC(ctx context.Context, addr string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, addr)
}
