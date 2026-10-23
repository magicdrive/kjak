package subcmd

import (
	"context"
	"os"
)

// openInput opens an input file (Windows has no FIFO at a path).
func openInput(path string) (*os.File, error) {
	return os.Open(path)
}

// readInput reads f to its end, maxInput bytes at most. A read of a
// console or a pipe cannot be interrupted on Windows: when ctx is done,
// kjak stops waiting for it and the read ends with the process.
func readInput(ctx context.Context, f *os.File, what string) ([]byte, error) {
	type read struct {
		b   []byte
		err error
	}
	done := make(chan read, 1)
	go func() {
		b, err := readLimited(f, what)
		done <- read{b, err}
	}()
	select {
	case res := <-done:
		return res.b, res.err
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	}
}
