//go:build !windows

package subcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func openFDs(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

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

// A canceled read of a FIFO without a writer, of a silent writer's, and of
// a pipe returns the cause, and leaves no descriptor and no goroutine.
func TestReadInputCanceledLeavesNothing(t *testing.T) {
	cause := errors.New("interrupted")
	for _, name := range []string{"fifo, no writer", "fifo, silent writer", "pipe"} {
		t.Run(name, func(t *testing.T) {
			fifo := filepath.Join(t.TempDir(), "in")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			fds, goroutines := openFDs(t), runtime.NumGoroutine()
			var f, w *os.File
			var err error
			switch name {
			case "pipe":
				f, w, err = os.Pipe()
			default:
				f, err = openInput(fifo)
				if err == nil && name == "fifo, silent writer" {
					w, err = os.OpenFile(fifo, os.O_WRONLY, 0)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			started := make(chan struct{})
			go func() { close(started); cancel(cause) }()
			<-started
			if _, err := readInput(ctx, f, "in"); !errors.Is(err, cause) {
				t.Fatalf("err = %v", err)
			}
			settled(t, goroutines) // before the close, which would end a read left behind
			f.Close()
			if w != nil {
				w.Close()
			}
			if n := openFDs(t); n != fds {
				t.Fatalf("%d descriptors open, %d before", n, fds)
			}
		})
	}
}

// Inputs that end are read whole, and the size limit holds.
func TestReadInputReadsToTheEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc")
	if err := os.WriteFile(path, []byte(`{"body":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openInput(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if b, err := readInput(context.Background(), f, "doc"); err != nil || string(b) != `{"body":"x"}` {
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
