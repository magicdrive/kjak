package commandline_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/commandline"
)

// Several feathers when the whole deadline passes: the delivered one stays
// delivered, the one the provider holds after reading it is unknown (not
// retried), the one still queued fails without an attempt. kjak ends near the
// deadline, not later.
func TestCritical_DeadlineAcrossFeathers(t *testing.T) {
	fast := newProvider(t, 200, "ok")
	release := make(chan struct{})
	defer close(release)
	var holdHits atomic.Int32
	hold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		holdHits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer hold.Close()
	e := newEnv(t)
	e.writeGlobal(featherConfig("a_fast", "slack", fast.URL) + featherConfig("b_hold", "slack", hold.URL) + featherConfig("c_queued", "slack", fast.URL))
	start := time.Now()
	r := e.run("send", "--to", "a_fast,b_hold,c_queued", "--concurrency", "1", "--timeout", "1s", "-o", "json", "x")
	elapsed := time.Since(start)
	rep := parseReport(t, r.stdout)
	if r.code != 1 || rep.Outcome != "partial" {
		t.Fatalf("exit %d: %s", r.code, r.stdout)
	}
	a, b, c := rep.Results[0], rep.Results[1], rep.Results[2]
	if a.Status != "delivered" || b.Acceptance != "unknown" || b.Attempts != 1 || c.Attempts != 0 || c.Acceptance != "none" || c.Error.Code != "deadline" {
		t.Fatalf("results = %+v", rep.Results)
	}
	if holdHits.Load() != 1 {
		t.Fatalf("the held request was sent %d times", holdHits.Load())
	}
	if elapsed > 10*time.Second {
		t.Fatalf("kjak ran %s with a 1s deadline", elapsed)
	}
	if fast.count() != 1 {
		t.Fatalf("fast: %d requests", fast.count())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// The report cannot be written (a closed pipe): kjak says so on stderr and
// its exit status still tells the delivery's outcome.
func TestOutputWriteFailure(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	var errOut bytes.Buffer
	r := e.runWith(func(rt *commandline.Runtime) { rt.Stdout = failingWriter{}; rt.Stderr = &errOut }, "send", "--to", "a", "x")
	if r.code != 0 || !strings.Contains(errOut.String(), "cannot write the result") || p.count() != 1 {
		t.Fatalf("exit %d, stderr %q", r.code, errOut.String())
	}
}

// Inputs over the limits are refused before anything is sent.
func TestOversizedInputs(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	e.stdin = strings.Repeat("x", 1<<20+1)
	if r := e.run("send", "--to", "a", "--stdin"); r.code != 2 || !strings.Contains(r.stderr, "larger than") {
		t.Fatalf("stdin: exit %d %s", r.code, r.stderr)
	}
	if r := e.run("send", "--to", "a", strings.Repeat("あ", 16385)); r.code != 2 {
		t.Fatalf("body: exit %d", r.code)
	}
	e.stdin = `{"body":"` + strings.Repeat("x", 1<<20) + `"}`
	if r := e.run("send", "--to", "a", "--json-input", "-"); r.code != 2 {
		t.Fatalf("json: exit %d", r.code)
	}
	if r := e.run("send", "--to", "a", "--title", "a\x1b]8;;evil\x07", "x"); r.code != 2 {
		t.Fatalf("title escape sequence: exit %d", r.code)
	}
	if p.count() != 0 {
		t.Fatal("something was sent")
	}
}

type epipeWriter struct{}

func (epipeWriter) Write([]byte) (int, error) { return 0, syscall.EPIPE }

// The output of the other commands is their result: when it cannot be
// written (a full disk), the command fails (exit 2) and says why; a reader
// that went away (| head) needs no diagnostic.
func TestCommandOutputWriteFailure(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", "https://hooks.example/x"))
	for _, args := range [][]string{{"feather", "list"}, {"feather", "list", "-o", "json"}, {"config", "validate"}, {"version"}} {
		var errOut bytes.Buffer
		r := e.runWith(func(rt *commandline.Runtime) { rt.Stdout = failingWriter{}; rt.Stderr = &errOut }, args...)
		if r.code != 2 || !strings.Contains(errOut.String(), "cannot write the output") {
			t.Fatalf("%v: exit %d, stderr %q", args, r.code, errOut.String())
		}
		errOut.Reset()
		r = e.runWith(func(rt *commandline.Runtime) { rt.Stdout = epipeWriter{}; rt.Stderr = &errOut }, args...)
		if r.code != 2 || errOut.Len() != 0 {
			t.Fatalf("%v, closed pipe: exit %d, stderr %q", args, r.code, errOut.String())
		}
	}
}

// A feather in several groups is refused naming the first group, every
// time (not the one a map happened to give).
func TestFeatherRemoveNamesGroupDeterministically(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", "https://hooks.example/x") +
		"[groups.zz]\nfeathers = [\"a\"]\n[groups.bb]\nfeathers = [\"a\"]\n[groups.mm]\nfeathers = [\"a\"]\n")
	for i := 0; i < 30; i++ {
		if r := e.run("feather", "remove", "a"); r.code != 2 || !strings.Contains(r.stderr, `group "bb"`) {
			t.Fatalf("exit %d: %s", r.code, r.stderr)
		}
	}
}

// partialWriter takes the first n bytes, then fails every write with err.
type partialWriter struct {
	n   int
	err error
	got bytes.Buffer
}

func (w *partialWriter) Write(p []byte) (int, error) {
	if left := w.n - w.got.Len(); left < len(p) {
		w.got.Write(p[:max(left, 0)])
		return max(left, 0), w.err
	}
	return w.got.Write(p)
}

// The output contract when the output breaks part way, through a JSON
// document: a send keeps the exit status of its deliveries and says on
// stderr that its report is incomplete (closed pipe or not: the deliveries
// were made); another command fails (exit 2), quietly for a closed pipe. A
// stderr that cannot be written changes no exit status.
func TestOutputContractOnPartialWrites(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	for _, err := range []error{syscall.EPIPE, syscall.ENOSPC} {
		var errOut bytes.Buffer
		w := &partialWriter{n: 20, err: err}
		r := e.runWith(func(rt *commandline.Runtime) { rt.Stdout = w; rt.Stderr = &errOut }, "send", "--to", "a", "-o", "json", "x")
		if r.code != 0 || !strings.Contains(errOut.String(), "cannot write the result") || w.got.Len() != 20 {
			t.Fatalf("send, %v: exit %d, stderr %q, %d bytes", err, r.code, errOut.String(), w.got.Len())
		}
		errOut.Reset()
		w = &partialWriter{n: 20, err: err}
		r = e.runWith(func(rt *commandline.Runtime) { rt.Stdout = w; rt.Stderr = &errOut }, "feather", "list", "-o", "json")
		quiet := err == syscall.EPIPE
		if r.code != 2 || (errOut.Len() == 0) != quiet {
			t.Fatalf("feather list, %v: exit %d, stderr %q", err, r.code, errOut.String())
		}
	}
	if n := p.count(); n != 2 {
		t.Fatalf("%d deliveries", n)
	}
	r := e.runWith(func(rt *commandline.Runtime) { rt.Stderr = failingWriter{} }, "send", "--to", "nobody", "x")
	if r.code != 2 {
		t.Fatalf("an unwritable stderr: exit %d", r.code)
	}
	r = e.runWith(func(rt *commandline.Runtime) { rt.Stderr = failingWriter{} }, "send", "--to", "a", "x")
	if r.code != 0 {
		t.Fatalf("an unwritable stderr: exit %d", r.code)
	}
}
