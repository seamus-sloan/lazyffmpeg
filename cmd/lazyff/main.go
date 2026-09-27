// Command lazyff chains ffmpeg operations on one video, either headless
// from flags or interactively in a terminal UI.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/seamus-sloan/lazyffmpeg/internal/app"
	"github.com/seamus-sloan/lazyffmpeg/internal/tui"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	stderrIsTTY := false
	if fi, err := os.Stderr.Stat(); err == nil {
		stderrIsTTY = fi.Mode()&os.ModeCharDevice != 0
	}

	a := app.App{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		StderrIsTTY: stderrIsTTY,
		LaunchTUI:   tui.Run,
	}

	os.Exit(a.Main(ctx, os.Args[1:]))
}
