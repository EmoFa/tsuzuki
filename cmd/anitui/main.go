package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/EmoFa/anitui/internal/cli"
)

func main() {
	// Interrupts, `kill` and a closed terminal all stop anitui the same way, so
	// mpv, the browser and temporary files are cleaned up.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		// After the first signal, restore the default so a second one exits at
		// once if cleanup gets stuck.
		<-ctx.Done()
		stop()
	}()
	root, app := cli.NewRootCmd()
	err := root.ExecuteContext(ctx)
	app.Close()
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
