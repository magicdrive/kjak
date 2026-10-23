//go:build !windows

package commandline

import (
	"errors"
	"syscall"
)

// isBrokenPipe tells a write to a pipe whose reader went away (| head).
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE)
}
