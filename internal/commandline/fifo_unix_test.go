//go:build !windows

package commandline_test

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/magicdrive/kjak/internal/commandline"
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

// A FIFO whose writer closes it without writing (a producer that failed) is
// an empty input: exit 2, nothing sent, without waiting for another writer.
func TestFIFOEmptyWriterIsAnInputError(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan result, 1)
	go func() { done <- e.run("send", "--to", "a", "--json-input", fifo) }()
	openFIFOWriter(t, fifo).Close()
	if r := waitResult(t, done, "the end of an empty writer was not seen"); r.code != 2 || p.count() != 0 {
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

// highFDs makes the descriptors the process opens next 1024 or more,
// beyond what select(2) can wait for: it holds every lower free one (on
// /dev/null) until the test ends. It skips where the limit of open files
// does not allow it.
func highFDs(t *testing.T) {
	t.Helper()
	var rl syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil || rl.Cur < 1024+256 {
		if os.Getenv("CI") != "" { // CI must run it
			t.Fatalf("RLIMIT_NOFILE is %d (%v): 1024 descriptors cannot be held", rl.Cur, err)
		}
		t.Skipf("RLIMIT_NOFILE is %d (%v): 1024 descriptors cannot be held", rl.Cur, err)
	}
	var held []int
	t.Cleanup(func() {
		for _, fd := range held {
			syscall.Close(fd)
		}
	})
	for {
		fd, err := syscall.Open("/dev/null", syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		if fd >= 1024 {
			syscall.Close(fd)
			return
		}
		held = append(held, fd)
	}
}

// With every descriptor of kjak at 1024 or more, its input still gives way
// to SIGINT: a FIFO without a writer, with a silent one, with part of a
// document, and standard input.
func TestCritical_InputIsCancelableAtHighDescriptors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		writer func(t *testing.T, path string) *os.File
	}{
		{"fifo, no writer", func(*testing.T, string) *os.File { return nil }},
		{"fifo, silent writer", openFIFOWriter},
		{"fifo, partial document", func(t *testing.T, path string) *os.File {
			w := openFIFOWriter(t, path)
			if _, err := w.WriteString(`{"body": "par`); err != nil {
				t.Fatal(err)
			}
			return w
		}},
		{"stdin", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProvider(t, 200, "ok")
			e := newEnv(t)
			e.writeGlobal(featherConfig("a", "slack", p.URL))
			highFDs(t)
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan result, 1)
			if tc.writer == nil && tc.name == "stdin" {
				stdin, w, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				defer stdin.Close()
				defer w.Close()
				if stdin.Fd() < 1024 {
					t.Fatalf("standard input is descriptor %d", stdin.Fd())
				}
				go func() {
					done <- e.runFull(ctx, func(rt *commandline.Runtime) { rt.Stdin = stdin }, "send", "--to", "a", "--stdin")
				}()
			} else {
				fifo := filepath.Join(t.TempDir(), "in")
				if err := syscall.Mkfifo(fifo, 0o600); err != nil {
					t.Fatal(err)
				}
				go func() { done <- e.runCtx(ctx, "send", "--to", "a", "--json-input", fifo) }()
				if w := tc.writer(t, fifo); w != nil {
					defer w.Close()
					if w.Fd() < 1024 {
						t.Fatalf("the writer is descriptor %d", w.Fd())
					}
				}
			}
			cancel()
			r := waitResult(t, done, "the input kept kjak after SIGINT")
			if r.code != 130 || p.count() != 0 {
				t.Fatalf("exit %d, %d sent: %s", r.code, p.count(), r.stderr)
			}
		})
	}
}
