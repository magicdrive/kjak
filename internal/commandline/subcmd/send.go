package subcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/delivery"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/builtin"
	"github.com/magicdrive/kjak/internal/feather/webhook"
	"github.com/magicdrive/kjak/internal/output"
	"github.com/magicdrive/kjak/internal/routing"
	"github.com/magicdrive/kjak/internal/template"
	"github.com/magicdrive/kjak/internal/textbank"
)

// maxInput bounds what is read of standard input or a JSON input file.
const maxInput = 1 << 20

// Send runs kjak send.
func Send(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseSend(args)
	var out *option.OutputOption
	if opt != nil {
		out = &opt.OutputOption
	}
	rt.jsonOutput(out, true)
	if code, ok := rt.parsed(ctx, err, textbank.SendHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	settings := l.Effective.Settings()
	msg, err := buildMessage(ctx, rt, opt, l.Effective, settings)
	if err != nil {
		var coded *codedError
		if !errors.As(err, &coded) {
			err = withCode(output.CodeInput, err)
		}
		return rt.fail(ctx, err)
	}
	plan, err := routing.Resolve(l.Effective, opt.Targets, msg.Level)
	if err != nil {
		return rt.fail(ctx, err)
	}

	// The deadline of the send starts once the message is read (its input
	// is paced by its producer: `make test | kjak send --stdin`): the
	// credentials and every delivery, retry and wait end by then.
	timeout := settings.Timeout
	if opt.Timeout > 0 {
		timeout = opt.Timeout
	}
	c := rt.Clock
	if c == nil {
		c = clock.Real{}
	}
	start := c.Now()
	preflight, cancel := clock.WithDeadline(ctx, c, timeout, feather.ErrDeadline)
	defer cancel()
	// pastDeadline is the error of a preflight that the deadline ended
	// (nothing was sent): exit 2, error code deadline.
	pastDeadline := func(err error) error {
		msg := fmt.Sprintf("the deadline of %s passed before anything was sent", timeout)
		if err != nil {
			msg += ": " + err.Error()
		}
		return withCode(output.CodeDeadline, errors.New(msg))
	}

	// preflight: every active feather is built (its credentials resolved)
	// before anything is sent
	redactor := &credential.Redactor{}
	httpClient := rt.HTTPClient
	if httpClient == nil {
		// one client for the send, its connections closed when it ends
		httpClient = webhook.NewClient()
		defer httpClient.CloseIdleConnections()
	}
	deps := builtin.Deps{
		Resolver:   credential.NewResolver(rt.LookupEnv, rt.Keychain),
		Redactor:   redactor,
		Clock:      rt.Clock,
		HTTPClient: httpClient,
	}
	targets := make([]delivery.Target, 0, len(plan.Entries))
	for _, e := range plan.Entries {
		t := delivery.Target{Name: e.Name, Type: e.Config.Type, Groups: e.Groups, Skip: e.Skip}
		if e.Skip == nil {
			f, err := builtin.Build(preflight, e.Name, e.Config, deps)
			if err != nil {
				if ctx.Err() == nil && preflight.Err() != nil {
					err = pastDeadline(err)
				}
				return rt.fail(ctx, &redactedError{msg: redactor.Redact(err.Error()), err: err})
			}
			t.Feather = f
		}
		targets = append(targets, t)
	}
	if ctx.Err() != nil {
		return rt.fail(ctx, ctx.Err())
	}
	// the deliveries have what the preflight left; none starts after it
	remaining := timeout - c.Now().Sub(start)
	if preflight.Err() != nil || remaining <= 0 {
		return rt.fail(ctx, pastDeadline(nil))
	}

	opts := delivery.Options{
		Timeout:     remaining,
		MaxRetries:  settings.MaxRetries,
		Concurrency: settings.Concurrency,
		Clock:       rt.Clock,
		Backoff:     rt.Backoff,
		Redactor:    redactor,
	}
	if opt.MaxRetries >= 0 {
		opts.MaxRetries = opt.MaxRetries
	}
	if opt.Concurrency > 0 {
		opts.Concurrency = opt.Concurrency
	}

	var report core.Report
	if opt.DryRun {
		report = dryRunReport(targets)
	} else {
		report = delivery.Run(ctx, msg, targets, opts)
	}

	switch {
	case opt.Format == output.FormatJSON:
		err = output.WriteJSON(rt.Stdout, report, msg.Level, opt.DryRun)
	case !opt.Quiet || report.Outcome != core.OutcomeSuccess:
		err = output.WriteText(rt.Stdout, report, opt.DryRun)
	}
	if err != nil {
		fmt.Fprintf(rt.Stderr, "kjak: cannot write the result: %v\n", err)
	}

	switch {
	case report.Canceled:
		return ExitInterrupted
	case report.Summary.Failed > 0:
		return ExitDelivery
	}
	return ExitOK
}

func dryRunReport(targets []delivery.Target) core.Report {
	results := make([]core.Result, len(targets))
	for i, t := range targets {
		r := core.Result{Feather: t.Name, Type: t.Type, Groups: t.Groups, Acceptance: core.AcceptanceNone, History: []core.Attempt{}}
		if t.Skip != nil {
			r.Status, r.Skipped = core.StatusSkipped, t.Skip
		} else {
			r.Status = core.StatusPlanned
		}
		results[i] = r
	}
	return core.Aggregate(results, false, 0)
}

// jsonMessage is the document of --json-input.
type jsonMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Level string `json:"level"`
	URL   string `json:"url"`
}

// buildMessage reads the message from its one input, applies --title,
// --level and --url, and validates it.
func buildMessage(ctx context.Context, rt *Runtime, opt *option.SendOption, cfg *configure.File, s configure.Settings) (core.Message, error) {
	var msg core.Message
	switch opt.Mode {
	case option.InputArgs:
		msg.Body = opt.Body
	case option.InputStdin:
		b, err := readStdin(ctx, rt.Stdin)
		if err != nil {
			return msg, err
		}
		msg.Body = strings.TrimRight(string(b), "\r\n")
	case option.InputJSON:
		var b []byte
		var err error
		if opt.JSONInput == "-" {
			b, err = readStdin(ctx, rt.Stdin)
		} else {
			b, err = readFileLimited(ctx, opt.JSONInput)
		}
		if err != nil {
			return msg, err
		}
		if err := checkJSONObject(b); err != nil {
			return msg, fmt.Errorf("--json-input: %v", err)
		}
		var jm jsonMessage
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&jm); err != nil {
			return msg, fmt.Errorf("--json-input: %v (expected {\"title\", \"body\", \"level\", \"url\"})", err)
		}
		if dec.More() {
			return msg, errors.New("--json-input: expected one JSON object")
		}
		msg = core.Message{Title: jm.Title, Body: jm.Body, Level: core.Level(jm.Level), URL: jm.URL}
	case option.InputTemplate, option.InputTemplateFile:
		vars, err := template.ParseVars(opt.Vars)
		if err != nil {
			return msg, err
		}
		name := opt.Template
		var t configure.TemplateConfig
		if opt.Mode == option.InputTemplate {
			var ok bool
			if t, ok = cfg.Templates[name]; !ok {
				return msg, notFound("template", name)
			}
		} else {
			name = opt.TemplateFile
			if t, err = template.ParseFile(opt.TemplateFile); err != nil {
				return msg, err
			}
		}
		if msg, err = template.Render(name, t, vars); err != nil {
			return msg, err
		}
	}
	if opt.HasTitle {
		msg.Title = opt.Title
	}
	if opt.HasURL {
		msg.URL = opt.URL
	}
	if opt.HasLevel {
		msg.Level = core.Level(opt.Level)
	}
	if msg.Level == "" {
		msg.Level = core.Level(s.Level)
	}
	level, err := core.ParseLevel(string(msg.Level))
	if err != nil {
		return msg, err
	}
	msg.Level = level
	return msg, msg.Validate()
}

// checkJSONObject requires b to be exactly one JSON object whose keys are
// unique (encoding/json would keep the last of a repeated key silently).
func checkJSONObject(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errors.New("expected one JSON object")
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if seen[key] {
			return fmt.Errorf("key %q given twice", key)
		}
		seen[key] = true
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}

// readStdin reads standard input until ctx is done at the latest
// (readInput); a reader that is not a file (a test's) is read as it is.
func readStdin(ctx context.Context, r io.Reader) ([]byte, error) {
	if f, ok := r.(*os.File); ok {
		return readInput(ctx, f, "standard input")
	}
	return readLimited(r, "standard input")
}

func readLimited(r io.Reader, what string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxInput+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", what, err)
	}
	if len(b) > maxInput {
		return nil, fmt.Errorf("%s is larger than %d bytes", what, maxInput)
	}
	return b, nil
}

// readFileLimited reads the file of --json-input, a FIFO too (openInput,
// readInput).
func readFileLimited(ctx context.Context, path string) ([]byte, error) {
	f, err := openInput(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readInput(ctx, f, path)
}
