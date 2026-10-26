// Package subcmd runs the kjak subcommands: it turns parsed options
// (../option) into calls of the domain packages and their results into
// output and an exit status.
package subcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/output"
	"github.com/magicdrive/kjak/internal/routing"
	"github.com/magicdrive/kjak/internal/template"
)

// Exit statuses (the exit code contract, docs/cli.md).
const (
	ExitOK          = 0   // success (every attempted target delivered)
	ExitDelivery    = 1   // a delivery failed (including an unknown acceptance)
	ExitUsage       = 2   // input, configuration or preflight error: nothing was sent
	ExitInterrupted = 130 // SIGINT
)

// Runtime is everything a subcommand reads from or writes to the outside.
// Tests replace each part.
type Runtime struct {
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	StdinIsTerminal bool

	ConfigEnv  configure.Env
	LookupEnv  func(string) (string, bool)
	Keychain   credential.Keychain
	Clock      clock.Clock
	HTTPClient *http.Client // nil: the webhook feathers' own
	Backoff    delivery.Backoff

	// jsonErrors: the command asked for --output json, so its errors are
	// JSON documents on stdout too; jsonReport: in the shape of a send report.
	jsonErrors bool
	jsonReport bool
}

// OSRuntime is the runtime of the process.
func OSRuntime() Runtime {
	return Runtime{
		Stdin:           os.Stdin,
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		StdinIsTerminal: isTerminal(os.Stdin),
		ConfigEnv:       configure.OSEnv(),
		LookupEnv:       os.LookupEnv,
		Keychain:        credential.DefaultKeychain(),
		Clock:           clock.Real{},
		Backoff:         delivery.DefaultBackoff(),
	}
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// fail reports err and returns the exit status of a usage, configuration or
// preflight error (2), or of an interruption (130). The diagnostic goes to
// stderr; with --output json, the error document goes to stdout as well.
func (rt *Runtime) fail(ctx context.Context, err error) int {
	code, status := errorCode(err), ExitUsage
	msg := err.Error()
	if ctx.Err() != nil {
		code, status, msg = output.CodeInterrupted, ExitInterrupted, "interrupted"
	}
	fmt.Fprintf(rt.Stderr, "kjak: %s\n", msg)
	if rt.jsonErrors {
		if werr := output.WriteErrorJSON(rt.Stdout, rt.jsonReport, code, msg); werr != nil {
			fmt.Fprintf(rt.Stderr, "kjak: cannot write the result: %v\n", werr)
		}
	}
	return status
}

// jsonOutput records whether a command asked for --output json (o is nil
// when its flags could not be parsed: the error is then text only).
func (rt *Runtime) jsonOutput(o *option.OutputOption, report bool) {
	rt.jsonErrors = o != nil && o.JSON()
	rt.jsonReport = report
}

// codedError gives an error its JSON error code.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string { return e.err.Error() }
func (e *codedError) Unwrap() error { return e.err }

func withCode(code string, err error) error { return &codedError{code: code, err: err} }

// notFound is the error of a name that the configuration does not define.
func notFound(kind, name string) error {
	return withCode(output.CodeNotFound, fmt.Errorf("no %s named %q", kind, name))
}

// redactedError keeps err's identity (its code) with a message cleaned of
// secrets.
type redactedError struct {
	msg string
	err error
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

func errorCode(err error) string {
	var coded *codedError
	var usage *option.UsageError
	switch {
	case errors.As(err, &coded):
		return coded.code
	case errors.As(err, &usage):
		return output.CodeUsage
	case errors.Is(err, configure.ErrUntrustedProject):
		return output.CodeUntrustedProject
	case errors.Is(err, credential.ErrCredential):
		return output.CodeCredential
	case errors.Is(err, configure.ErrInvalidConfig), errors.Is(err, configure.ErrLocked), errors.Is(err, configure.ErrConcurrentModification):
		return output.CodeConfig
	case errors.Is(err, routing.ErrRouting):
		return output.CodeRouting
	case errors.Is(err, core.ErrInvalidMessage), errors.Is(err, template.ErrTemplate):
		return output.CodeInput
	}
	return output.CodeError
}

// help prints a help text and returns ExitOK.
func (rt *Runtime) help(text string) int {
	fmt.Fprint(rt.Stdout, text)
	return ExitOK
}

// parsed handles the error of an option parser: --help prints help, any
// other error is a usage error. ok is true when the command should run.
func (rt *Runtime) parsed(ctx context.Context, err error, helpText string) (code int, ok bool) {
	if err == nil {
		return 0, true
	}
	if errors.Is(err, option.ErrHelp) {
		return rt.help(helpText), false
	}
	return rt.fail(ctx, err), false
}

func (rt *Runtime) paths(c option.ConfigOption) (configure.Paths, error) {
	p, err := configure.DefaultPaths(rt.ConfigEnv, c.ConfigPath, c.NoProject)
	if err != nil {
		return p, err
	}
	p.SetExplicitSecrets(c.SecretsPath)
	return p, nil
}

// load reads the configuration of a run.
func (rt *Runtime) load(c option.ConfigOption) (*configure.Loaded, error) {
	p, err := rt.paths(c)
	if err != nil {
		return nil, err
	}
	return configure.Load(p)
}

// warnLoadedLiterals reports the literal secrets of the files a send uses.
func (rt *Runtime) warnLoadedLiterals(l *configure.Loaded) {
	rt.warnLiterals(l.Global)
	if l.ProjectTrusted {
		rt.warnLiterals(l.Project)
	}
}

// warnLiterals reports the literal secrets of a configuration file on
// stderr: they are deprecated (kjak config migrate-secrets moves them to a
// secrets file).
func (rt *Runtime) warnLiterals(src *configure.Source) {
	if src == nil || src.File == nil {
		return
	}
	lits := configure.Literals(src.File)
	if len(lits) == 0 {
		return
	}
	fmt.Fprintf(rt.Stderr, "kjak: warning: %s keeps %d secret(s) in the file as literal (deprecated): %s\n  move them to a secrets file: kjak config migrate-secrets (see docs/migration.md)\n",
		src.Path, len(lits), literalFields(lits))
}

func literalFields(lits []configure.CredentialField) string {
	fields := make([]string, len(lits))
	for i, c := range lits {
		fields[i] = "feathers." + c.Feather + "." + c.Field
	}
	return strings.Join(fields, ", ")
}

// resolveScope is the file a change of name goes to: the scope asked for,
// else the file that defines name, else the global file.
func resolveScope(l *configure.Loaded, scope, kind, name string) (configure.Scope, error) {
	if scope != "" {
		return configure.ParseScope(scope)
	}
	if l != nil && l.Origin(kind, name) == "project" {
		return configure.ScopeProject, nil
	}
	return configure.ScopeGlobal, nil
}
