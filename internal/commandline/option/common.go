// Package option parses the command line of each kjak subcommand into an
// option struct. It knows nothing of configuration files or delivery: the
// subcommands (../subcmd) turn options into calls of the domain packages.
package option

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
)

// ErrHelp: --help was given; the caller prints the help of the command.
var ErrHelp = flag.ErrHelp

// UsageError is an error of the command line (exit status 2).
type UsageError struct {
	Command string
	Err     error
}

func (e *UsageError) Error() string {
	return fmt.Sprintf("%v\nRun 'kjak %s --help' for usage", e.Err, e.Command)
}

func (e *UsageError) Unwrap() error { return e.Err }

func usageErr(command, format string, a ...any) error {
	return &UsageError{Command: command, Err: fmt.Errorf(format, a...)}
}

// list is a repeatable flag; each value may be comma-separated.
type list []string

func (l *list) String() string { return strings.Join(*l, ",") }

func (l *list) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// repeat is a repeatable flag whose values are kept whole (key=value).
type repeat []string

func (r *repeat) String() string { return strings.Join(*r, " ") }

func (r *repeat) Set(v string) error {
	*r = append(*r, v)
	return nil
}

func newFlagSet(command string) *flag.FlagSet {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// ConfigOption are the flags of every command that reads the configuration.
type ConfigOption struct {
	ConfigPath string // --config: the global configuration file
	NoProject  bool   // --no-project: ignore ./.kjak.toml
}

func (c *ConfigOption) register(fs *flag.FlagSet) {
	fs.StringVar(&c.ConfigPath, "config", "", "")
	fs.StringVar(&c.ConfigPath, "c", "", "")
	fs.BoolVar(&c.NoProject, "no-project", false, "")
}

// OutputOption is --output.
type OutputOption struct {
	Format string
}

func (o *OutputOption) register(fs *flag.FlagSet) {
	fs.StringVar(&o.Format, "output", "text", "")
	fs.StringVar(&o.Format, "o", "text", "")
}

// JSON reports whether --output json was given.
func (o *OutputOption) JSON() bool { return o.Format == "json" }

func (o *OutputOption) check(command string) error {
	if o.Format != "text" && o.Format != "json" {
		return usageErr(command, "unknown --output %q (use text or json)", o.Format)
	}
	return nil
}

// parse parses flags and positional arguments in any order ("kjak send hello
// --to x"); everything after "--" is positional.
func parse(command string, fs *flag.FlagSet, args []string) ([]string, error) {
	var tail []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, tail = args[:i], args[i+1:]
	}
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, ErrHelp
			}
			return nil, &UsageError{Command: command, Err: err}
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		pos = append(pos, rest[0])
		args = rest[1:]
	}
	return append(pos, tail...), nil
}

// set reports the flags given on the command line.
func set(fs *flag.FlagSet) map[string]bool {
	m := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { m[f.Name] = true })
	return m
}

// exactArgs checks the number of positional arguments.
func exactArgs(command string, pos []string, n int, names string) error {
	if len(pos) != n {
		return usageErr(command, "expected %s, got %d argument(s)", names, len(pos))
	}
	return nil
}
