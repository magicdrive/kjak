//go:build !windows

package subcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// settled waits for the goroutines to be back to n (one ending may still be
// returning), failing after a while.
func settled(t *testing.T, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > n; {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines, %d before", runtime.NumGoroutine(), n)
		}
		runtime.Gosched()
	}
}

// Inputs that end are read whole, and the size limit holds.
func TestReadInputReadsToTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc")
	if err := os.WriteFile(path, []byte(`{"body":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := readFileLimited(context.Background(), path); err != nil || string(b) != `{"body":"x"}` {
		t.Fatalf("%q %v", b, err)
	}
	r, w, _ := os.Pipe()
	defer r.Close()
	go func() {
		_, _ = w.Write(make([]byte, maxInput+1))
		w.Close()
	}()
	if _, err := readInput(context.Background(), r, "pipe"); err == nil {
		t.Fatal("an input over the limit was read")
	}
}

// highFDs makes the descriptors the process opens next 1024 or more,
// beyond what select(2) can wait for, by holding every lower free one
// until the test ends; it skips where the open file limit does not allow.
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

// fdSet is the descriptors open in the process.
func fdSet(t *testing.T) map[string]bool {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	s := map[string]bool{}
	for _, e := range entries {
		s[e.Name()] = true
	}
	return s
}

// A read gives way to its context with the context's cause (not an end of
// input, not a read error), whatever the writer does, and leaves no
// descriptor and no goroutine behind: at low descriptors, and at 1024 and
// more, beyond what select(2) can wait for.
func TestReadInputCanceledLeavesNothing(t *testing.T) { testReadInputCanceled(t, false) }

func TestReadInputCanceledAtHighDescriptors(t *testing.T) { testReadInputCanceled(t, true) }

func testReadInputCanceled(t *testing.T, high bool) {
	cause := errors.New("interrupted")
	for _, name := range []string{"fifo, no writer", "fifo, silent writer", "fifo, partial document", "pipe"} {
		t.Run(name, func(t *testing.T) {
			fifo := filepath.Join(t.TempDir(), "in")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			if high {
				highFDs(t)
			}
			before, goroutines := fdSet(t), runtime.NumGoroutine()
			ctx, cancel := context.WithCancelCause(context.Background())
			done := make(chan error, 1)
			var r, w *os.File
			var err error
			if name == "pipe" {
				if r, w, err = os.Pipe(); err != nil {
					t.Fatal(err)
				}
				if high && r.Fd() < 1024 {
					t.Fatalf("the pipe is descriptor %d", r.Fd())
				}
				go func() { _, err := readStdin(ctx, r); done <- err }()
			} else {
				go func() { _, err := readFileLimited(ctx, fifo); done <- err }()
				if name != "fifo, no writer" {
					if w, err = os.OpenFile(fifo, os.O_WRONLY, 0); err != nil { // once kjak reads it
						t.Fatal(err)
					}
					if name == "fifo, partial document" {
						if _, err := w.WriteString(`{"body": "par`); err != nil {
							t.Fatal(err)
						}
					}
					for fd := range fdSet(t) {
						if high && !before[fd] && len(fd) < 4 {
							t.Fatalf("descriptor %s is below 1024", fd)
						}
					}
				}
			}
			cancel(cause)
			select {
			case err := <-done:
				if !errors.Is(err, cause) {
					t.Fatalf("err = %v, want the cause of the cancel", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the read did not give way to its context")
			}
			settled(t, goroutines) // before the closes, which would end a read left behind
			for _, f := range []*os.File{r, w} {
				if f != nil {
					f.Close()
				}
			}
			if after := fdSet(t); len(after) != len(before) {
				t.Fatalf("%d descriptors open, %d before", len(after), len(before))
			}
		})
	}
}

// The end of an input is told on every system and at any descriptor: a
// FIFO whose writer closes after its document (macOS: poll does not tell
// it), a pipe left non-blocking by whoever opened it (reads may say EAGAIN
// after poll), and /dev/null, which macOS cannot poll: an error, not a
// read that cannot be interrupted.
func TestReadInputEnds(t *testing.T) {
	highFDs(t)
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			return
		}
		_, _ = w.WriteString(`{"body":`)
		_, _ = w.WriteString(`"x"}`)
		w.Close()
	}()
	if b, err := readFileLimited(context.Background(), fifo); err != nil || string(b) != `{"body":"x"}` {
		t.Fatalf("fifo: %q %v", b, err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := syscall.SetNonblock(int(r.Fd()), true); err != nil {
		t.Fatal(err)
	}
	go func() {
		_, _ = w.WriteString("part one, ")
		_, _ = w.WriteString("part two")
		w.Close()
	}()
	if b, err := readStdin(context.Background(), r); err != nil || string(b) != "part one, part two" {
		t.Fatalf("non-blocking pipe: %q %v", b, err)
	}

	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	b, err := readStdin(context.Background(), null)
	if runtime.GOOS == "darwin" {
		if err == nil || !strings.Contains(err.Error(), "cannot wait") {
			t.Fatalf("/dev/null on macOS: %q %v", b, err)
		}
	} else if err != nil || len(b) != 0 {
		t.Fatalf("/dev/null: %q %v", b, err)
	}
}

// A FIFO over the size limit is refused, its writer not waited for.
func TestReadInputFIFOLimit(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	w := make(chan *os.File, 1)
	go func() {
		f, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			close(w)
			return
		}
		w <- f
		_, _ = f.Write(make([]byte, maxInput+1)) // the reader stops at the limit: EPIPE
	}()
	_, err := readFileLimited(context.Background(), fifo)
	if f, ok := <-w; ok {
		defer f.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v", err)
	}
}

// Standard input that is a named FIFO (kjak send --stdin < fifo; macOS
// reads it through its path): its end is told, and a silent writer gives
// way to the context, at high descriptors.
func TestReadInputStdinNamedFIFO(t *testing.T) {
	highFDs(t)
	open := func(t *testing.T) (r, w *os.File) {
		fifo := filepath.Join(t.TempDir(), "in")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		wc := make(chan *os.File, 1)
		go func() {
			f, _ := os.OpenFile(fifo, os.O_WRONLY, 0)
			wc <- f
		}()
		r, err := os.OpenFile(fifo, os.O_RDONLY, 0) // as a shell opens it for kjak
		if err != nil {
			t.Fatal(err)
		}
		return r, <-wc
	}
	r, w := open(t)
	go func(w *os.File) {
		_, _ = w.WriteString("the body")
		w.Close()
	}(w)
	if b, err := readStdin(context.Background(), r); err != nil || string(b) != "the body" {
		t.Fatalf("%q %v", b, err)
	}
	r.Close()

	r, w = open(t)
	defer r.Close()
	defer w.Close()
	goroutines := runtime.NumGoroutine()
	cause := errors.New("interrupted")
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	go func() { _, err := readStdin(ctx, r); done <- err }()
	cancel(cause)
	select {
	case err := <-done:
		if !errors.Is(err, cause) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read did not give way to its context")
	}
	settled(t, goroutines)
}
