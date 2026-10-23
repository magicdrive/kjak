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
	"time"

	"golang.org/x/sys/unix"
)

// readFileInput reads the file of --json-input (readInput). It is opened
// without waiting (O_NONBLOCK: a FIFO without a writer does not hold the
// open) and read through that description only: what becomes of the path
// afterwards does not matter.
func readFileInput(ctx context.Context, path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !fifoEndPolled && isFIFO(f) {
		return readFIFO(ctx, f, path)
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
	if !fifoEndPolled && isFIFO(f) {
		if path, ok := fifoPath(f); ok {
			// a named FIFO (kjak send --stdin < fifo): read through a
			// description of kjak's own, which it may make non-blocking
			g, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return nil, fmt.Errorf("cannot read %s: its FIFO is no longer at %s", what, path)
			}
			defer g.Close()
			if a, b := statOf(f), statOf(g); a == nil || b == nil || !os.SameFile(a, b) {
				return nil, fmt.Errorf("cannot read %s: its FIFO is no longer at %s", what, path)
			}
			return readFIFO(ctx, g, what)
		}
	}
	return readPolled(ctx, f, what)
}

// readPolled reads f, each read waiting in poll(2) for f or for the end of
// ctx. A FIFO without a writer is not readable until one writes or closes.
func readPolled(ctx context.Context, f *os.File, what string) ([]byte, error) {
	canceled, release, err := cancelPipe(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", what, err)
	}
	defer release()

	// Fd: blocking reads, made once poll tells
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}, {Fd: int32(canceled), Events: unix.POLLIN}}
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

// fifoRecheck bounds a wait of readFIFO: how late it may see that the
// writer of a FIFO closed.
const fifoRecheck = 100 * time.Millisecond

// readFIFO reads the FIFO f, a non-blocking description of kjak's own, on
// a system whose poll(2) does not tell that a FIFO's writer closed
// (fifoEndPolled false: macOS). It waits in poll for data or the end of ctx,
// fifoRecheck at most, and reads without waiting: no data with a writer
// there is EAGAIN, no writer is a read of 0, the end once a writer was
// seen. Nothing is written into the FIFO and its path is not used again. A
// writer that comes and goes without writing between two looks is not
// seen: the read waits for the next one.
func readFIFO(ctx context.Context, f *os.File, what string) ([]byte, error) {
	canceled, release, err := cancelPipe(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", what, err)
	}
	defer release()
	fd := int(f.Fd()) // Fd makes it blocking: it is ours, make it non-blocking again
	if err := unix.SetNonblock(fd, true); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", what, err)
	}

	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: int32(canceled), Events: unix.POLLIN}}
	var b []byte
	buf := make([]byte, 32<<10)
	writer := false
	for {
		n, err := unix.Read(fd, buf)
		switch {
		case n > 0:
			writer = true
			if b = append(b, buf[:n]...); len(b) > maxInput {
				return nil, fmt.Errorf("%s is larger than %d bytes", what, maxInput)
			}
			continue
		case err == unix.EINTR:
			continue
		case err == unix.EAGAIN:
			writer = true
		case err != nil:
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		case writer:
			return b, nil
		}
		fds[0].Revents, fds[1].Revents = 0, 0
		if _, err := unix.Poll(fds, int(fifoRecheck/time.Millisecond)); err != nil && err != unix.EINTR {
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		}
		if fds[1].Revents != 0 {
			return nil, context.Cause(ctx)
		}
		if fds[0].Revents&unix.POLLNVAL != 0 {
			return nil, fmt.Errorf("cannot read %s: the system cannot wait for it", what)
		}
	}
}

// cancelPipe is a descriptor that becomes readable when ctx is done (a
// byte written by context.AfterFunc), and the release that waits for that
// write, if it started, then closes the pipe.
func cancelPipe(ctx context.Context) (int, func(), error) {
	r, w, err := os.Pipe()
	if err != nil {
		return 0, nil, err
	}
	woke := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(woke)
		_, _ = w.Write([]byte{0})
	})
	return int(r.Fd()), func() {
		if !stop() {
			<-woke
		}
		w.Close()
		r.Close()
	}, nil
}

func statOf(f *os.File) os.FileInfo {
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	return st
}

func isFIFO(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode().Type() == fs.ModeNamedPipe
}
