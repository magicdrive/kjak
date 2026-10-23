//go:build !windows

package subcmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// readFileInput reads the file of --json-input (readInput): a FIFO too,
// whose open waits for its writer within the context.
func readFileInput(ctx context.Context, path string) ([]byte, error) {
	if fifoNeedsWake {
		if st, err := os.Stat(path); err == nil && st.Mode().Type() == fs.ModeNamedPipe {
			return readFIFO(ctx, nil, path, path)
		}
	}
	// O_NONBLOCK: the open of a FIFO without a writer does not wait (poll
	// waits for one)
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fifoNeedsWake && isFIFO(f) {
		return nil, fmt.Errorf("%s became a FIFO while kjak opened it", path)
	}
	return readInput(ctx, f, path)
}

// readInput reads f to its end, maxInput bytes at most, until ctx is done
// at the latest: a FIFO or a pipe whose writer neither writes nor closes
// (cron, CI) gives way to SIGINT and SIGTERM, whatever the number of its
// descriptor, and nothing is left behind. A regular file never waits.
func readInput(ctx context.Context, f *os.File, what string) ([]byte, error) {
	if st, err := f.Stat(); err == nil && st.Mode().IsRegular() {
		return readLimited(f, what)
	}
	if isFIFO(f) {
		if path, ok := fifoPath(f); ok && !nonBlocking(f) {
			return readFIFO(ctx, f, path, what)
		}
	}
	return readPolled(ctx, f, what)
}

// readPolled reads f, each read waiting in poll(2) for f or for the byte
// that the end of ctx writes to a pipe of its own. A FIFO without a writer
// is not readable until one writes or closes.
func readPolled(ctx context.Context, f *os.File, what string) ([]byte, error) {
	wr, ww, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", what, err)
	}
	defer wr.Close()
	defer ww.Close()
	woke := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(woke)
		_, _ = ww.Write([]byte{0})
	})
	defer func() {
		if !stop() {
			<-woke
		}
	}()

	// Fd: blocking reads, made once poll tells
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}, {Fd: int32(wr.Fd()), Events: unix.POLLIN}}
	var b []byte
	buf := make([]byte, 32<<10)
	for {
		fds[0].Revents, fds[1].Revents = 0, 0
		if _, err := unix.Poll(fds, -1); err != nil {
			if err == unix.EINTR {
				continue
			}
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		}
		if fds[1].Revents != 0 {
			return nil, context.Cause(ctx)
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			// macOS polls no device (/dev/null)
			return nil, fmt.Errorf("cannot read %s: the system cannot wait for it", what)
		}
		if fds[0].Revents == 0 {
			continue
		}
		// readable, at its end (POLLHUP) or in error (POLLERR): the read tells
		n, err := f.Read(buf)
		if b = append(b, buf[:n]...); len(b) > maxInput {
			return nil, fmt.Errorf("%s is larger than %d bytes", what, maxInput)
		}
		switch {
		case err == io.EOF:
			return b, nil
		case errors.Is(err, syscall.EAGAIN): // inherited non-blocking: wait again
		case err != nil:
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		}
	}
}

// readFIFO reads the FIFO at path with blocking reads (opening it first
// when f is nil: the open waits for a writer), for a system whose poll(2)
// does not tell that a FIFO's writer closed (fifoNeedsWake). The end of ctx
// wakes the open or read: it opens the FIFO, a reader and a writer that do
// not wait, and writes a byte into it, after checking that the path is
// still that FIFO.
func readFIFO(ctx context.Context, f *os.File, path, what string) ([]byte, error) {
	var want unix.Stat_t
	if err := unix.Stat(path, &want); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", what, err)
	}
	finished, woke := make(chan struct{}), make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(woke)
		wakeFIFO(path, &want, finished)
	})
	defer func() {
		close(finished)
		if !stop() {
			<-woke
		}
	}()
	if f == nil {
		var err error
		if f, err = os.OpenFile(path, os.O_RDONLY, 0); err != nil { // a writer, or the wake
			return nil, err
		}
		defer f.Close()
	}
	var got unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &got); err != nil || got.Dev != want.Dev || got.Ino != want.Ino {
		return nil, fmt.Errorf("cannot read %s: the FIFO changed while kjak opened it", what)
	}
	var b []byte
	buf := make([]byte, 32<<10)
	for {
		n, err := f.Read(buf)
		if ctx.Err() != nil {
			return nil, context.Cause(ctx)
		}
		if b = append(b, buf[:n]...); len(b) > maxInput {
			return nil, fmt.Errorf("%s is larger than %d bytes", what, maxInput)
		}
		if err == io.EOF {
			return b, nil
		}
		if err != nil {
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		}
	}
}

// wakeFIFO ends a blocking open or read of the FIFO want at path: a reader
// lets a writer open without waiting, the writer lets the open return and
// its byte the read, both held until the read is finished.
func wakeFIFO(path string, want *unix.Stat_t, finished <-chan struct{}) {
	same := func(fd int) bool {
		var st unix.Stat_t
		return unix.Fstat(fd, &st) == nil && st.Dev == want.Dev && st.Ino == want.Ino
	}
	r, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(r)
	w, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(w)
	if same(r) && same(w) {
		_, _ = unix.Write(w, []byte{0}) // a full FIFO: the read returns anyway
		<-finished
	}
}

func isFIFO(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode().Type() == fs.ModeNamedPipe
}

func nonBlocking(f *os.File) bool {
	fl, err := unix.FcntlInt(f.Fd(), unix.F_GETFL, 0)
	return err == nil && fl&unix.O_NONBLOCK != 0
}
