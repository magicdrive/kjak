// Package commandline dispatches the kjak command line to its subcommands.
// There is no default subcommand: `kjak` alone prints the short help.
package commandline

import (
	"context"
	"fmt"

	"github.com/magicdrive/kjak/internal/commandline/subcmd"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/textbank"
)

const (
	Send     = "send"
	Feather  = "feather"
	Group    = "group"
	Config   = "config"
	Template = "template"
	Version  = "version"
)

// Runtime is what the subcommands read from and write to.
type Runtime = subcmd.Runtime

// Keychain is the OS keychain of a Runtime.
type Keychain = credential.Keychain

// StdIO is the runtime of the process.
func StdIO() Runtime { return subcmd.OSRuntime() }

// Execute runs the command line args and returns the exit status.
func Execute(ctx context.Context, version string, args []string, rt Runtime) int {
	if len(args) == 0 {
		fmt.Fprint(rt.Stderr, textbank.ShortHelpMessage)
		return subcmd.ExitUsage
	}
	command, rest := args[0], args[1:]
	switch command {
	case Send:
		return subcmd.Send(ctx, &rt, rest)
	case Feather:
		return subcmd.Feather(ctx, &rt, rest)
	case Group:
		return subcmd.Group(ctx, &rt, rest)
	case Config:
		return subcmd.Config(ctx, &rt, rest)
	case Template:
		return subcmd.Template(ctx, &rt, rest)
	case Version, "-v", "--version":
		fmt.Fprintf(rt.Stdout, "kjak version %s\n", version)
		return subcmd.ExitOK
	case "-h", "--help", "help":
		fmt.Fprint(rt.Stdout, textbank.HelpMessage)
		return subcmd.ExitOK
	}
	fmt.Fprintf(rt.Stderr, "kjak: unknown command %q\n\n%s", command, textbank.ShortHelpMessage)
	return subcmd.ExitUsage
}
