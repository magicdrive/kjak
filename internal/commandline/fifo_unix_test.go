//go:build !windows

package commandline_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// --json-input FIFO: whatever its writer does (not there yet, there but
// silent, gone quiet after part of the document), SIGINT or SIGTERM ends
// the read: exit 130, nothing sent.
func TestCritical_FIFOInputIsCancelable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writer func(t *testing.T, path string) *os.File
	}{
		{"no writer", func(*testing.T, string) *os.File { return nil }},
		{"silent writer", openFIFOWriter},
		{"partial document", func(t *testing.T, path string) *os.File {
			w := openFIFOWriter(t, path)
			if _, err := w.WriteString(`{"body": "par`); err != nil {
				t.Fatal(err)
			}
			return w
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, 200, "ok")
			e := newEnv(t)
			e.writeGlobal(featherConfig("a", "slack", p.URL))
			fifo := filepath.Join(t.TempDir(), "in")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan result, 1)
			go func() { done <- e.runCtx(ctx, "send", "--to", "a", "--json-input", fifo) }()
			if w := tc.writer(t, fifo); w != nil {
				defer w.Close()
			}
			cancel()
			r := waitResult(t, done, "the FIFO kept kjak after SIGINT")
			if r.code != 130 || p.count() != 0 {
				t.Fatalf("exit %d, %d sent: %s", r.code, p.count(), r.stderr)
			}
		})
	}
}

// A FIFO whose writer sends the document and closes is read like a file.
func TestFIFOInputDelivers(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan result, 1)
	go func() { done <- e.run("send", "--to", "a", "--json-input", fifo) }()
	w := openFIFOWriter(t, fifo)
	if _, err := w.WriteString(`{"body": "from a fifo"}`); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if r := waitResult(t, done, "the FIFO was not read to its end"); r.code != 0 || p.count() != 1 {
		t.Fatalf("exit %d, %d sent: %s", r.code, p.count(), r.stderr)
	}
}

// openFIFOWriter opens path for writing once kjak has it open for reading.
func openFIFOWriter(t *testing.T, path string) *os.File {
	t.Helper()
	w, err := os.OpenFile(path, os.O_WRONLY, 0) // waits for the reader
	if err != nil {
		t.Fatal(err)
	}
	return w
}
