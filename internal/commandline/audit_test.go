package commandline_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/commandline"
)

// A JSON document with a key twice is ambiguous (encoding/json keeps the
// last one silently): it is refused.
func TestJSONInputDuplicateKeysRefused(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	for _, doc := range []string{
		`{"body":"first","body":"second"}`,
		`{"body":"x","level":"info","level":"critical"}`,
	} {
		e.stdin = doc
		if r := e.run("send", "--to", "a", "--json-input", "-"); r.code != 2 || !strings.Contains(r.stderr, "twice") {
			t.Errorf("%s: exit %d %s", doc, r.code, r.stderr)
		}
	}
	for _, doc := range []string{`{"body":"x"} {"body":"y"}`, `[{"body":"x"}]`, `{"body":"x"`} {
		e.stdin = doc
		if r := e.run("send", "--to", "a", "--json-input", "-"); r.code != 2 {
			t.Errorf("%s: exit %d", doc, r.code)
		}
	}
	if p.count() != 0 {
		t.Fatal("an ambiguous document was sent")
	}
}

// The command line accepts no delivery limit the configuration refuses.
func TestDeliveryFlagsBounded(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", "https://hooks.example.invalid/x"))
	for _, args := range [][]string{
		{"--timeout", "11m"},
		{"--max-retries", "11"},
		{"--concurrency", "65"},
	} {
		r := e.run(append([]string{"send", "--to", "a", "--dry-run", "x"}, args...)...)
		if r.code != 2 {
			t.Errorf("%v: exit %d", args, r.code)
		}
	}
}

// Running kjak many times in one process (as the tests and an embedding
// program do) leaves no connection open: each send closes what it opened.
func TestCritical_RepeatedSendsCloseConnections(t *testing.T) {
	var mu sync.Mutex
	open := map[net.Conn]bool{}
	closed := make(chan struct{}, 64)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	srv.Config.ConnState = func(c net.Conn, s http.ConnState) {
		mu.Lock()
		defer mu.Unlock()
		switch s {
		case http.StateNew:
			open[c] = true
		case http.StateClosed, http.StateHijacked:
			if open[c] {
				delete(open, c)
				closed <- struct{}{}
			}
		}
	}
	srv.Start()
	defer srv.Close()
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", srv.URL) + featherConfig("b", "discord", srv.URL))
	const runs = 5
	for i := 0; i < runs; i++ {
		e.must("send", "--to", "a,b", "x")
	}
	// every connection of every run is closed by kjak, not by the server
	timeout := time.After(10 * time.Second)
	for i := 0; i < runs*2; i++ {
		select {
		case <-closed:
		case <-timeout:
			mu.Lock()
			n := len(open)
			mu.Unlock()
			t.Fatalf("%d connection(s) left open after the sends returned", n)
		}
	}
}

// SIGINT that arrives after every delivery finished interrupts nothing: the
// outcome is the deliveries', not 130.
func TestInterruptAfterCompletionIsNotCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", srv.URL))
	// the context is canceled while the report is being written
	e.afterDelivery = cancel
	r := e.runCtx(ctx, "send", "--to", "a", "-o", "json", "x")
	if r.code != 0 || strings.Contains(r.stdout, `"canceled": true`) {
		t.Fatalf("exit %d: %s", r.code, r.stdout)
	}
}

// blockingKeychain answers only when released (the macOS dialog that waits
// for the user), or when its context is done.
type blockingKeychain struct{ entered chan struct{} }

func (b blockingKeychain) Lookup(ctx context.Context, service, account string) (string, error) {
	close(b.entered)
	<-ctx.Done()
	return "", ctx.Err()
}

// SIGINT while a credential is being resolved ends kjak (130), nothing sent.
func TestInterruptDuringCredentialResolution(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal("[feathers.k]\ntype = \"slack\"\nwebhook_url = { keychain = { service = \"kjak\", account = \"slow\" } }\n")
	kc := blockingKeychain{entered: make(chan struct{})}
	e.keychain = kc
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan result, 1)
	go func() { done <- e.runCtx(ctx, "send", "--to", "k", "x") }()
	<-kc.entered
	cancel()
	select {
	case r := <-done:
		if r.code != 130 || p.count() != 0 {
			t.Fatalf("exit %d, %d sent", r.code, p.count())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("kjak kept waiting for the keychain after SIGINT")
	}
}

// Standard input that never ends (a pipe nobody closes, in cron or CI) does
// not hold kjak after SIGINT or SIGTERM: it stops reading, exits 130, sends
// nothing.
func TestInterruptWhileReadingStdin(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	stdin, w, err := os.Pipe() // a file: the reader kjak gets from the OS
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close() }()
	defer func() { _ = w.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan result, 1)
	go func() {
		done <- e.runFull(ctx, func(rt *commandline.Runtime) { rt.Stdin = stdin }, "send", "--to", "a", "--stdin")
	}()
	select {
	case r := <-done:
		if r.code != 130 || p.count() != 0 {
			t.Fatalf("exit %d, %d sent", r.code, p.count())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("kjak kept reading standard input after SIGINT")
	}
}
