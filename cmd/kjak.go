package cmd

import (
	"context"
	"os"
	"os/signal"

	"github.com/magicdrive/kjak/internal/commandline"
)

// Execute runs kjak with the process arguments and exits with the status of
// the exit code contract (internal/commandline/subcmd/exitcode.go).
func Execute(version string) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	// the first SIGINT cancels the run; a second one kills kjak at once
	context.AfterFunc(ctx, stop)
	code := commandline.Execute(ctx, version, os.Args[1:], commandline.StdIO())
	stop()
	os.Exit(code)
}
