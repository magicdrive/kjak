package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/magicdrive/kjak/internal/commandline"
)

// Execute runs kjak with the process arguments and exits with the status of
// the exit code contract (internal/commandline/subcmd/runtime.go).
func Execute(version string) {
	// SIGINT (Ctrl-C) and SIGTERM (a CI job canceled, a service stopped)
	// cancel the run: kjak still reports and exits 130
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// a second one kills kjak at once
	context.AfterFunc(ctx, stop)
	// a reader that went away (| head) fails the write of the report, which
	// kjak reports; it does not kill kjak with the delivery's status unsaid.
	// Caught, not ignored: the keychain tools kjak runs keep the default.
	signal.Notify(make(chan os.Signal, 1), syscall.SIGPIPE)
	code := commandline.Execute(ctx, version, os.Args[1:], commandline.StdIO())
	stop()
	os.Exit(code)
}
