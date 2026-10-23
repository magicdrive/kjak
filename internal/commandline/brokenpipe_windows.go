package commandline

import (
	"errors"
	"syscall"

	"golang.org/x/sys/windows"
)

// isBrokenPipe tells a write to a pipe whose reader went away (| head):
// Windows reports it as ERROR_NO_DATA ("the pipe is being closed") or
// ERROR_BROKEN_PIPE, not EPIPE.
func isBrokenPipe(err error) bool {
	return errors.Is(err, syscall.EPIPE) || errors.Is(err, windows.ERROR_NO_DATA) || errors.Is(err, windows.ERROR_BROKEN_PIPE)
}
