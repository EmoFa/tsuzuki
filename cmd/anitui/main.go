package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/EmoFa/anitui/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	root, app := cli.NewRootCmd()
	err := root.ExecuteContext(ctx)
	app.Close()
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
