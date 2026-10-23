package commandline_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/clock"
	"github.com/magicdrive/kjak/internal/commandline"
)

// stallKeychain answers after advance has passed on clock (a lookup that
// takes that long), or, with block, waits for its context.
type stallKeychain struct {
	clock   *clock.Fake
	advance time.Duration
	block   bool
	value   string
	entered chan struct{}
	calls   *atomic.Int32
}

func (k stallKeychain) Lookup(ctx context.Context, service, account string) (string, error) {
	k.calls.Add(1)
	k.entered <- struct{}{}
	if k.block {
		<-ctx.Done()
		return "", context.Cause(ctx)
	}
	k.clock.Advance(k.advance)
	return k.value, nil
}

func keychainFeathers(names ...string) string {
	var b strings.Builder
	for _, n := range names {
		b.WriteString("[feathers." + n + "]\ntype = \"slack\"\nwebhook_url = { keychain = { service = \"kjak\", account = \"" + n + "\" } }\n")
	}
	return b.String()
}

func waitResult(t *testing.T, done <-chan result, what string) result {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(20 * time.Second):
		t.Fatalf("kjak did not end: %s", what)
	}
	return result{}
}

// --timeout covers the credentials: a lookup that outlives it ends the
// send (exit 2, error code deadline, nothing sent), and the lookups of the
// other feathers do not wait a deadline of their own.
func TestCritical_DeadlineCoversCredentialResolution(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(keychainFeathers("k1", "k2", "k3"))
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var calls atomic.Int32
	e.keychain = stallKeychain{block: true, entered: make(chan struct{}, 3), calls: &calls}
	done := make(chan result, 1)
	go func() {
		done <- e.runFull(context.Background(), func(rt *commandline.Runtime) { rt.Clock = fc }, "send", "--to", "k1,k2,k3", "--timeout", "5s", "-o", "json", "x")
	}()
	<-e.keychain.(stallKeychain).entered
	fc.Advance(5 * time.Second) // once: the deadline is the send's, not each lookup's
	r := waitResult(t, done, "a keychain lookup outlived --timeout")
	d := parseErrorDoc(t, r)
	if r.code != 2 || d.Error == nil || d.Error.Code != "deadline" || p.count() != 0 || calls.Load() != 1 {
		t.Fatalf("exit %d, %d lookups, %d sent: %s %s", r.code, calls.Load(), p.count(), r.stdout, r.stderr)
	}
}

// The time the credentials took is taken from the deliveries: a send of
// --timeout 5s whose lookup took 4s gives its delivery the 1s left. The
// request was written when that second passed: unknown, not retried.
func TestCritical_DeadlineSpansPreflightAndDelivery(t *testing.T) {
	received := make(chan struct{}, 4)
	release := make(chan struct{})
	var hits atomic.Int32
	hold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		hits.Add(1)
		received <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer hold.Close()
	defer close(release) // first: Close waits for the handler
	e := newEnv(t)
	e.writeGlobal(keychainFeathers("k"))
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var calls atomic.Int32
	e.keychain = stallKeychain{clock: fc, advance: 4 * time.Second, value: hold.URL, entered: make(chan struct{}, 1), calls: &calls}
	done := make(chan result, 1)
	go func() {
		done <- e.runFull(context.Background(), func(rt *commandline.Runtime) { rt.Clock = fc }, "send", "--to", "k", "--timeout", "5s", "--max-retries", "3", "-o", "json", "x")
	}()
	<-received
	fc.Advance(time.Second)
	r := waitResult(t, done, "the delivery outlived the deadline of the send")
	if r.code != 1 || !strings.Contains(r.stdout, `"acceptance": "unknown"`) || !strings.Contains(r.stdout, "deadline exceeded after the request was sent") || hits.Load() != 1 {
		t.Fatalf("exit %d, %d requests: %s", r.code, hits.Load(), r.stdout)
	}
}

// A lookup that used up the whole deadline leaves no time to send: nothing
// is attempted (exit 2, deadline), rather than a delivery started late.
func TestCritical_NoDeliveryStartsAfterTheDeadline(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(keychainFeathers("k"))
	fc := clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var calls atomic.Int32
	e.keychain = stallKeychain{clock: fc, advance: 5 * time.Second, value: p.URL, entered: make(chan struct{}, 1), calls: &calls}
	r := e.runWith(func(rt *commandline.Runtime) { rt.Clock = fc }, "send", "--to", "k", "--timeout", "5s", "-o", "json", "x")
	d := parseErrorDoc(t, r)
	if r.code != 2 || d.Error == nil || d.Error.Code != "deadline" || p.count() != 0 {
		t.Fatalf("exit %d, %d sent: %s %s", r.code, p.count(), r.stdout, r.stderr)
	}
}
