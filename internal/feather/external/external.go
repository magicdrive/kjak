// Package external is the external feather: a plugin, the executable
// kjak-feather-<name>, run once per attempt with protocol v1
// (docs/plugins.md):
//
//   - kjak writes one JSON request on its standard input and closes it: the
//     message, the feather's settings, the secrets the configuration gives
//     it (and no other), the attempt, the deadline;
//   - the plugin writes one JSON response on its standard output and exits
//     0; standard error is diagnostics (kept short, redacted);
//   - no argument, a minimal environment (an allow list), the temporary
//     directory as its working directory, no secret in either;
//   - its own process group (Unix) or Job Object (Windows), killed with it
//     at a timeout, the deadline or SIGINT, and when it ends.
//
// A plugin runs with the user's rights: it can do anything the user can.
// kjak decides which one runs and what it gets, not what it does.
package external

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather"
	"github.com/magicdrive/kjak/internal/feather/webhook"
)

// ProtocolVersion is the version of the request and the response.
const ProtocolVersion = 1

// Limits of a run.
const (
	MaxStdout      = 1 << 20
	MaxStderr      = 64 << 10
	DefaultTimeout = 10 * time.Second
	// stopGrace bounds the wait for the plugin's pipes once it was killed
	// or exited (a child that kept them open).
	stopGrace = 2 * time.Second
)

// BaseEnv are the environment variables every plugin gets (when set).
var BaseEnv = []string{"PATH", "HOME", "TMPDIR", "TEMP", "TMP", "LANG", "LC_ALL", "LC_CTYPE", "TZ", "SYSTEMROOT", "USERPROFILE"}

// Request is the document a plugin reads on its standard input.
type Request struct {
	ProtocolVersion int               `json:"protocol_version"`
	Feather         string            `json:"feather"`
	Plugin          string            `json:"plugin"`
	Attempt         int               `json:"attempt"`
	Deadline        time.Time         `json:"deadline"`
	TimeoutMS       int64             `json:"timeout_ms"`
	IdempotencyKey  string            `json:"idempotency_key"`
	Message         RequestMessage    `json:"message"`
	Settings        map[string]string `json:"settings"`
	Secrets         map[string]string `json:"secrets"`
}

// RequestMessage is the message of a request.
type RequestMessage struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Level string `json:"level"`
	URL   string `json:"url"`
}

// Response is the document a plugin writes on its standard output. Unknown
// fields are ignored.
type Response struct {
	ProtocolVersion *int   `json:"protocol_version"`
	Status          string `json:"status"`     // delivered, failed
	Acceptance      string `json:"acceptance"` // confirmed, rejected, unknown
	Retryable       bool   `json:"retryable"`
	RetryAfterMS    int64  `json:"retry_after_ms"`
	StatusCode      int    `json:"status_code"`
	Error           *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Message string `json:"message"`
}

// Feather runs one plugin.
type Feather struct {
	Name     string
	Exe      *Executable
	Rehash   bool // the digest is pinned: hash again before each run
	Settings map[string]string
	Secrets  map[string]credential.Secret
	Env      []string // KEY=VALUE
	Timeout  time.Duration
	// Retry: a refusal the plugin marks retryable may be retried
	// (retry.policy = "plugin"); MaxAttempts caps the attempts.
	Retry          bool
	MaxAttempts    int
	IdempotencyKey string
	Clock          clock.Clock
	// Redactor: the secrets redacted from the plugin's text before it is
	// shortened (nil: none).
	Redactor *credential.Redactor

	attempts atomic.Int32
}

// Options of New.
type Options struct {
	// LookupEnv reads kjak's environment (PATH, the allowed variables).
	LookupEnv func(string) (string, bool)
	Clock     clock.Clock
	// Redactor redacts the secrets of the feather from the plugin's text
	// (its message, its stderr) before it is shortened.
	Redactor *credential.Redactor
}

// New builds the feather of fc around a checked executable, with the
// secrets resolved for it.
func New(name string, fc configure.FeatherConfig, exe *Executable, secrets map[string]credential.Secret, opt Options) (*Feather, error) {
	f := &Feather{
		Name: name, Exe: exe, Rehash: fc.SHA256 != "",
		Settings: map[string]string{}, Secrets: secrets,
		Timeout: DefaultTimeout, Clock: opt.Clock, Redactor: opt.Redactor,
	}
	for k, v := range fc.Settings {
		f.Settings[k] = v
	}
	if fc.Timeout != nil {
		f.Timeout = fc.Timeout.Duration
	}
	if fc.Retry != nil {
		f.Retry = fc.Retry.Policy == configure.RetryPlugin
		f.MaxAttempts = fc.Retry.MaxAttempts
	}
	// a variable that is the source of one of its secrets is not passed
	// as a variable too: the plugin gets its secrets through the request
	for _, e := range fc.Env {
		for k, r := range fc.PluginSecrets {
			if r != nil && r.Env != nil && *r.Env == e {
				return nil, fmt.Errorf("env: %s is the source of secrets.%s: it is passed in the request only", e, k)
			}
		}
	}
	names := append(append([]string(nil), BaseEnv...), fc.Env...)
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] || opt.LookupEnv == nil {
			continue
		}
		seen[n] = true
		v, ok := opt.LookupEnv(n)
		if !ok {
			continue
		}
		// Windows names variables without case: Path is PATH there
		if strings.EqualFold(n, "PATH") {
			// the plugin's commands are found as the plugin is: in absolute
			// directories only, never in its working directory
			dirs, _ := searchDirs(v)
			if len(dirs) == 0 {
				continue
			}
			v = strings.Join(dirs, string(os.PathListSeparator))
		}
		f.Env = append(f.Env, n+"="+v)
	}
	f.Env = append(f.Env, "KJAK_PLUGIN_PROTOCOL=1")
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	f.IdempotencyKey = hex.EncodeToString(b[:])
	return f, nil
}

var errAttemptTimeout = errors.New("the plugin timeout passed")

// Deliver runs the plugin once.
func (f *Feather) Deliver(ctx context.Context, msg core.Message) feather.Outcome {
	n := int(f.attempts.Add(1))
	o := f.run(ctx, msg, n)
	if o.Retryable {
		switch {
		case !f.Retry:
			o.Retryable = false
			o.Err = fmt.Errorf("%w (not retried: retry.policy is never)", o.Err)
		case f.MaxAttempts > 0 && n >= f.MaxAttempts:
			o.Retryable = false
			o.Err = fmt.Errorf("%w (retry.max_attempts %d reached)", o.Err, f.MaxAttempts)
		}
	}
	return o
}

// cappedBuffer keeps at most max bytes. With fail, a write past max fails
// (the plugin's next write gets a broken pipe); otherwise the rest is
// dropped quietly.
type cappedBuffer struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	max      int
	fail     bool
	overflow bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if left := c.max - c.buf.Len(); len(p) > left {
		c.buf.Write(p[:max(left, 0)])
		c.overflow = true
		if c.fail {
			return max(left, 0), errors.New("output limit")
		}
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *cappedBuffer) bytes() ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...), c.overflow
}

func (f *Feather) run(ctx context.Context, msg core.Message, attempt int) feather.Outcome {
	if err := f.Exe.Verify(f.Rehash); err != nil {
		return feather.Rejected(core.ErrCodeBlocked, 0, false, fmt.Errorf("the plugin was not run: %v", err))
	}
	c := f.Clock
	if c == nil {
		c = clock.Real{}
	}
	deadline := c.Now().Add(f.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	req := Request{
		ProtocolVersion: ProtocolVersion, Feather: f.Name, Plugin: f.Exe.Name, Attempt: attempt,
		Deadline: deadline.UTC(), TimeoutMS: f.Timeout.Milliseconds(), IdempotencyKey: f.IdempotencyKey,
		Message:  RequestMessage{Title: msg.Title, Body: msg.Body, Level: string(msg.Level), URL: msg.URL},
		Settings: f.Settings, Secrets: map[string]string{},
	}
	for k, s := range f.Secrets {
		req.Secrets[k] = s.Reveal()
	}
	in, err := json.Marshal(req)
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, err)
	}

	actx, cancel := context.WithTimeoutCause(ctx, f.Timeout, errAttemptTimeout)
	defer cancel()
	cmd := exec.CommandContext(actx, f.Exe.Real)
	cmd.Args = []string{f.Exe.Real}
	cmd.Env = f.Env
	// a new private directory, removed afterwards: not a directory others can
	// write to (os.TempDir may be /tmp), where a relative path of the plugin
	// could find their files
	dir, err := os.MkdirTemp("", "kjak-plugin-")
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, fmt.Errorf("the plugin was not run: %v", err))
	}
	defer func() { _ = os.RemoveAll(dir) }()
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(in)
	stdout := &cappedBuffer{max: MaxStdout, fail: true}
	stderr := &cappedBuffer{max: MaxStderr}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = stopGrace
	box, err := newContainment(cmd)
	if err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, fmt.Errorf("the plugin was not run: %v", err))
	}
	if err := box.start(); err != nil {
		return feather.Rejected(core.ErrCodeInternal, 0, false, fmt.Errorf("the plugin could not be started: %s", describeStart(err)))
	}
	werr := cmd.Wait()
	box.close() // nothing the plugin started stays behind

	out, overflow := stdout.bytes()
	diag, _ := stderr.bytes()
	withDiag := func(err error) error {
		if d := tail(f.Redactor, diag); d != "" {
			return fmt.Errorf("%w; stderr: %s", err, d)
		}
		return err
	}
	switch {
	case ctx.Err() != nil:
		return feather.Unknown(feather.ContextCode(ctx), withDiag(fmt.Errorf("%v while the plugin ran; it was stopped and may have delivered", feather.ContextErr(ctx))))
	case errors.Is(context.Cause(actx), errAttemptTimeout):
		return feather.Unknown(core.ErrCodeAmbiguous, withDiag(fmt.Errorf("the plugin did not finish within its timeout of %s; it was stopped and may have delivered", f.Timeout)))
	case overflow:
		return feather.Unknown(core.ErrCodeProtocol, withDiag(fmt.Errorf("the plugin wrote more than %d bytes on stdout; it may have delivered", MaxStdout)))
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) {
		return feather.Unknown(core.ErrCodeProtocol, withDiag(fmt.Errorf("the plugin %s; it may have delivered", describeExit(ee))))
	}
	if werr != nil {
		return feather.Unknown(core.ErrCodeProtocol, withDiag(fmt.Errorf("the plugin's output could not be read (%v); it may have delivered", werr)))
	}
	o, err := parseResponse(f.Redactor, out, c.Now())
	if err != nil {
		return feather.Unknown(core.ErrCodeProtocol, withDiag(fmt.Errorf("protocol error: %v; the plugin may have delivered", err)))
	}
	if o.Err != nil {
		o.Err = withDiag(o.Err)
	}
	return o
}

// parseResponse turns a response into an outcome.
func parseResponse(rd *credential.Redactor, out []byte, now time.Time) (feather.Outcome, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var r Response
	if err := dec.Decode(&r); err != nil {
		if len(bytes.TrimSpace(out)) == 0 {
			return feather.Outcome{}, errors.New("no response on stdout")
		}
		return feather.Outcome{}, errors.New("the response is not a JSON object")
	}
	if dec.More() {
		return feather.Outcome{}, errors.New("more than one JSON value on stdout")
	}
	if _, err := dec.Token(); err != io.EOF {
		return feather.Outcome{}, errors.New("more than one JSON value on stdout")
	}
	switch {
	case r.ProtocolVersion == nil:
		return feather.Outcome{}, errors.New("protocol_version is missing")
	case *r.ProtocolVersion != ProtocolVersion:
		return feather.Outcome{}, fmt.Errorf("unsupported protocol_version %d (kjak speaks %d)", *r.ProtocolVersion, ProtocolVersion)
	}
	text := ""
	code := ""
	if r.Error != nil {
		text, code = clean(rd, r.Error.Message), r.Error.Code
	}
	if text == "" {
		text = clean(rd, r.Message)
	}
	failure := func(def string) error {
		if text == "" {
			text = def
		}
		if r.StatusCode != 0 {
			return fmt.Errorf("plugin: status %d: %s", r.StatusCode, text)
		}
		return fmt.Errorf("plugin: %s", text)
	}
	switch r.Status {
	case "delivered":
		if r.Acceptance != "" && r.Acceptance != string(core.AcceptanceConfirmed) {
			return feather.Outcome{}, fmt.Errorf("status delivered with acceptance %q", r.Acceptance)
		}
		return feather.Confirmed(r.StatusCode), nil
	case "failed":
		switch r.Acceptance {
		case string(core.AcceptanceRejected):
			o := feather.Rejected(rejectedCode(code), r.StatusCode, r.Retryable, failure("refused"))
			if r.RetryAfterMS > 0 {
				// capped before the conversion: a huge value must not overflow
				o.RetryAfter = time.Duration(min(r.RetryAfterMS, webhook.MaxRetryAfter.Milliseconds())) * time.Millisecond
			}
			return o, nil
		case "", string(core.AcceptanceUnknown):
			return feather.Unknown(core.ErrCodeAmbiguous, failure("acceptance unknown")), nil
		}
		return feather.Outcome{}, fmt.Errorf("status failed with acceptance %q", r.Acceptance)
	}
	return feather.Outcome{}, fmt.Errorf("status must be delivered or failed, not %q", r.Status)
}

// rejectedCode keeps the codes kjak knows; any other is "rejected".
func rejectedCode(c string) core.ErrorCode {
	switch core.ErrorCode(c) {
	case core.ErrCodeRateLimited, core.ErrCodeServer, core.ErrCodeNetwork, core.ErrCodeProtocol, core.ErrCodeInternal:
		return core.ErrorCode(c)
	}
	return core.ErrCodeRejected
}

// printable is a plugin's text on one line, without control characters.
func printable(s string) string { return strings.TrimSpace(credential.Printable(s)) }

// clean keeps a plugin's text short and printable, the secrets of rd
// redacted before it is changed (credential.Redactor.Clip).
func clean(rd *credential.Redactor, s string) string {
	return rd.Clip(s, printable, 300, false)
}

// tail is the end of a plugin's stderr, printable and short, the secrets
// of rd redacted before it is changed.
func tail(rd *credential.Redactor, b []byte) string {
	return rd.Clip(strings.ToValidUTF8(string(b), "?"), printable, 300, true)
}

func describeStart(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func describeExit(ee *exec.ExitError) string {
	if code := ee.ExitCode(); code >= 0 {
		return fmt.Sprintf("exited with status %d", code)
	}
	return "was killed (" + ee.String() + ")"
}

// SortedSecretNames are the names of the secrets a feather passes.
func (f *Feather) SortedSecretNames() []string {
	names := make([]string, 0, len(f.Secrets))
	for k := range f.Secrets {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
