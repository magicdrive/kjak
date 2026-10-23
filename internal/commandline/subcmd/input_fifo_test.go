//go:build !windows

package subcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// opened waits until the process holds n descriptors of the file st (kjak
// opened it), failing after a while. (A stat of /dev/fd/N on macOS has the
// device of /dev/fd: fstat tells.)
func opened(t *testing.T, st os.FileInfo, n int) {
	t.Helper()
	want := st.Sys().(*syscall.Stat_t)
	for deadline := time.Now().Add(10 * time.Second); ; {
		entries, _ := os.ReadDir("/dev/fd")
		count := 0
		for _, e := range entries {
			var got syscall.Stat_t
			if fd, err := strconv.Atoi(e.Name()); err == nil && syscall.Fstat(fd, &got) == nil && got.Dev == want.Dev && got.Ino == want.Ino {
				count++
			}
		}
		if count >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d descriptors of the FIFO, want %d", count, n)
		}
		runtime.Gosched()
	}
}

// waitCanceled waits for the read of done to end with cause.
func waitCanceled(t *testing.T, done <-chan error, cause error) {
	t.Helper()
	select {
	case err := <-done:
		if !errors.Is(err, cause) {
			t.Fatalf("err = %v, want the cause of the cancel", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read did not give way to its context")
	}
}

// The cancel of a FIFO's read holds whatever happens to its path once kjak
// opened it (removed, replaced by another FIFO, a file, a symbolic link)
// and writes into nothing: not the FIFO, not what replaced it.
func TestFIFOCancelIgnoresItsPath(t *testing.T) {
	for _, high := range []bool{false, true} {
		for _, tc := range []string{"removed, no writer", "removed", "another FIFO", "a file", "a symbolic link"} {
			name := tc
			if high {
				name += ", high descriptors"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				fifo := filepath.Join(dir, "in")
				if err := syscall.Mkfifo(fifo, 0o600); err != nil {
					t.Fatal(err)
				}
				st, err := os.Stat(fifo)
				if err != nil {
					t.Fatal(err)
				}
				if high {
					highFDs(t)
				}
				fds, goroutines := len(fdSet(t)), runtime.NumGoroutine()
				cause := errors.New("interrupted")
				ctx, cancel := context.WithCancelCause(context.Background())
				done := make(chan error, 1)
				go func() { _, err := readFileLimited(ctx, fifo); done <- err }()
				var w *os.File
				if tc == "removed, no writer" {
					opened(t, st, 1)
				} else if w, err = os.OpenFile(fifo, os.O_WRONLY, 0); err != nil { // once kjak reads
					t.Fatal(err)
				}
				if err := os.Remove(fifo); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(dir, "target")
				var other *os.File
				switch tc {
				case "another FIFO":
					if err := syscall.Mkfifo(fifo, 0o600); err != nil {
						t.Fatal(err)
					}
					if other, err = os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0); err != nil {
						t.Fatal(err)
					}
					defer other.Close()
				case "a file":
					target = fifo
					fallthrough
				case "a symbolic link":
					if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
						t.Fatal(err)
					}
					if target != fifo {
						if err := os.Symlink(target, fifo); err != nil {
							t.Fatal(err)
						}
					}
				}
				cancel(cause)
				waitCanceled(t, done, cause)
				settled(t, goroutines)
				if w != nil {
					w.Close()
				}
				if other != nil {
					if n, _ := other.Read(make([]byte, 8)); n != 0 {
						t.Fatalf("%d bytes written into the FIFO that replaced it", n)
					}
				}
				if tc == "a file" || tc == "a symbolic link" {
					if b, _ := os.ReadFile(target); string(b) != "keep" {
						t.Fatalf("the file that replaced it holds %q", b)
					}
				}
				if n := len(fdSet(t)); n != fds+boolInt(other != nil) {
					t.Fatalf("%d descriptors open, %d before", n, fds)
				}
			})
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Other readers of the FIFO (it is a stream they share), waiting in their
// reads before or after kjak: the cancel holds, and puts nothing into the
// stream that they or kjak could read.
func TestFIFOCancelWithOtherReaders(t *testing.T) {
	for _, readers := range []int{1, 3} {
		for _, othersFirst := range []bool{false, true} {
			t.Run("", func(t *testing.T) { testFIFOCancelWithOtherReaders(t, readers, othersFirst) })
		}
	}
}

func testFIFOCancelWithOtherReaders(t *testing.T, readers int, othersFirst bool) {
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(fifo)
	if err != nil {
		t.Fatal(err)
	}
	goroutines := runtime.NumGoroutine()
	cause := errors.New("interrupted")
	ctx, cancel := context.WithCancelCause(context.Background())
	done := make(chan error, 1)
	kjak := func() { go func() { _, err := readFileLimited(ctx, fifo); done <- err }() }
	got := make(chan []byte, readers)
	others := func() {
		for range readers {
			go func() {
				r, err := os.OpenFile(fifo, os.O_RDONLY, 0) // waits for the writer
				if err != nil {
					got <- nil
					return
				}
				defer r.Close()
				b := make([]byte, 1)
				n, _ := r.Read(b)
				got <- b[:n]
			}()
		}
	}
	var w *os.File
	if othersFirst {
		others()
		if w, err = os.OpenFile(fifo, os.O_WRONLY, 0); err != nil {
			t.Fatal(err)
		}
		opened(t, st, readers+1) // the others and the writer
		kjak()
		opened(t, st, readers+2)
	} else {
		kjak()
		if w, err = os.OpenFile(fifo, os.O_WRONLY, 0); err != nil { // once kjak reads
			t.Fatal(err)
		}
		others()
		opened(t, st, readers+2)
	}
	defer w.Close()
	cancel(cause)
	waitCanceled(t, done, cause)
	settled(t, goroutines+readers)
	// a marker each ends the others' reads (macOS does not wake every
	// blocked reader at the end of a FIFO): any other byte was not sent
	for range readers {
		if _, err := w.Write([]byte("M")); err != nil {
			t.Fatal(err)
		}
		if b := <-got; string(b) != "M" {
			t.Fatalf("another reader read %q, not the marker", b)
		}
	}
}

// A cancel and the input's end in a race: the read gives either the whole
// document or the cancel, never a part of it, never a byte not written.
func TestFIFOCancelRacesTheEnd(t *testing.T) {
	const doc = `{"body":"the whole document"}`
	for range 50 {
		fifo := filepath.Join(t.TempDir(), "in")
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatal(err)
		}
		cause := errors.New("interrupted")
		ctx, cancel := context.WithCancelCause(context.Background())
		type read struct {
			b   []byte
			err error
		}
		done := make(chan read, 1)
		go func() { b, err := readFileLimited(ctx, fifo); done <- read{b, err} }()
		w, err := os.OpenFile(fifo, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			_, _ = w.WriteString(doc[:10])
			_, _ = w.WriteString(doc[10:])
			w.Close()
		}()
		cancel(cause)
		select {
		case r := <-done:
			if !(r.err == nil && string(r.b) == doc) && !errors.Is(r.err, cause) {
				t.Fatalf("%q %v", r.b, r.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the read did not end")
		}
	}
}

// A writer that opens the FIFO and closes it without writing (a producer
// that failed: `false > fifo`) ends the input, empty, as on Linux: kjak
// does not wait for another one. Whether kjak or the writer came first.
func TestFIFOEmptyWriterEndsTheInput(t *testing.T) {
	for _, writerFirst := range []bool{false, true} {
		for range 20 {
			fifo := filepath.Join(t.TempDir(), "in")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			type read struct {
				b   []byte
				err error
			}
			done := make(chan read, 1)
			kjak := func() { go func() { b, err := readFileLimited(context.Background(), fifo); done <- read{b, err} }() }
			wrote := make(chan error, 1)
			writer := func() {
				w, err := os.OpenFile(fifo, os.O_WRONLY, 0) // waits for kjak
				if err == nil {
					err = w.Close()
				}
				wrote <- err
			}
			if writerFirst {
				go writer()
				kjak()
			} else {
				kjak()
				writer()
			}
			if err := <-wrote; err != nil {
				t.Fatal(err)
			}
			select {
			case r := <-done:
				if r.err != nil || len(r.b) != 0 {
					t.Fatalf("writer first %v: %q %v", writerFirst, r.b, r.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatalf("writer first %v: the end of a writer that wrote nothing was not seen", writerFirst)
			}
		}
	}
}

// Standard input that is a named FIFO whose path no longer leads to it
// (removed, or another file there) before kjak reads it: macOS, which reads
// such a FIFO through its path, says so; Linux polls the descriptor it
// has. Nothing is written anywhere, and nothing waits forever.
func TestStdinFIFOWithoutItsPath(t *testing.T) {
	for _, tc := range []string{"removed", "a file"} {
		t.Run(tc, func(t *testing.T) {
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
			defer r.Close()
			w := <-wc
			if err := os.Remove(fifo); err != nil {
				t.Fatal(err)
			}
			if tc == "a file" {
				if err := os.WriteFile(fifo, []byte("keep"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			go func() {
				_, _ = w.WriteString("the body")
				w.Close()
			}()
			b, err := readStdin(context.Background(), r)
			if runtime.GOOS == "darwin" {
				if err == nil || !strings.Contains(err.Error(), "no longer at") {
					t.Fatalf("%q %v", b, err)
				}
			} else if err != nil || string(b) != "the body" {
				t.Fatalf("%q %v", b, err)
			}
			if tc == "a file" {
				if c, _ := os.ReadFile(fifo); string(c) != "keep" {
					t.Fatalf("the file holds %q", c)
				}
			}
		})
	}
}

// A context done before the read: it ends at once with the cause, and the
// pipe of the cancel and the wake are released (the wake ran before the
// release waits for it).
func TestFIFOCanceledBeforeTheRead(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	fds, goroutines := len(fdSet(t)), runtime.NumGoroutine()
	cause := errors.New("interrupted")
	for range 20 {
		ctx, cancel := context.WithCancelCause(context.Background())
		cancel(cause)
		if _, err := readFileLimited(ctx, fifo); !errors.Is(err, cause) {
			t.Fatalf("err = %v", err)
		}
	}
	settled(t, goroutines)
	if n := len(fdSet(t)); n != fds {
		t.Fatalf("%d descriptors open, %d before", n, fds)
	}
}
