//go:build !windows

package discord

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// ipcAddresses lists candidate sockets: the Discord client's temp directory and
// the sandbox paths used by the Flatpak and Snap packages and Vesktop.
func ipcAddresses() []string {
	var dirs []string
	seen := map[string]bool{}
	for _, env := range []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"} {
		if d := os.Getenv(env); d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	if !seen["/tmp"] {
		dirs = append(dirs, "/tmp")
	}
	subdirs := []string{"", "app/com.discordapp.Discord", "app/com.discordapp.DiscordCanary",
		"app/dev.vencord.Vesktop", ".flatpak/dev.vencord.Vesktop/xdg-run", "snap.discord", "snap.discord-canary"}

	var out []string
	for i := 0; i < 10; i++ {
		for _, d := range dirs {
			for _, sub := range subdirs {
				out = append(out, filepath.Join(d, sub, fmt.Sprintf("discord-ipc-%d", i)))
			}
		}
	}
	return out
}

func dialIPC(ctx context.Context, addr string) (net.Conn, error) {
	if _, err := os.Stat(addr); err != nil {
		return nil, err
	}
	var d net.Dialer
	return d.DialContext(ctx, "unix", addr)
}
