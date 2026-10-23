//go:build !windows

package subcmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// openInput opens an input file without waiting: a FIFO without a writer
// yet does not hold the open (O_NONBLOCK; readInput waits for the writer).
func openInput(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

// readInput reads f to its end, maxInput bytes at most, until ctx is done
// at the latest. Each read waits in select(2) for f or for the byte that
// the end of ctx writes to a pipe of its own, so a FIFO or a pipe whose
// writer neither writes nor closes (cron, CI) gives way to SIGINT and
// SIGTERM, and nothing is left behind: the wake is joined, the pipe closed.
// A FIFO opened without a writer is not readable (Linux, macOS) until one
// writes or closes.
func readInput(ctx context.Context, f *os.File, what string) ([]byte, error) {
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

	fd, wake := int(f.Fd()), int(wr.Fd()) // Fd: blocking reads once select tells
	if fd >= unix.FD_SETSIZE || wake >= unix.FD_SETSIZE {
		return readLimited(f, what)
	}
	var b []byte
	buf := make([]byte, 32<<10)
	for {
		var set unix.FdSet
		set.Set(fd)
		set.Set(wake)
		if _, err := unix.Select(max(fd, wake)+1, &set, nil, nil, nil); err != nil {
			if err == unix.EINTR {
				continue
			}
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		}
		if set.IsSet(wake) {
			return nil, context.Cause(ctx)
		}
		n, err := f.Read(buf)
		b = append(b, buf[:n]...)
		if len(b) > maxInput {
			return nil, fmt.Errorf("%s is larger than %d bytes", what, maxInput)
		}
		if err == io.EOF {
			return b, nil
		}
		if err != nil && err != syscall.EAGAIN {
			return nil, fmt.Errorf("cannot read %s: %w", what, err)
		}
	}
}
