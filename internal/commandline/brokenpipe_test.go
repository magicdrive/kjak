package commandline

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"testing"
)

// The write of a pipe whose reader closed is a broken pipe on every system,
// wrapped or not; other write errors are not.
func TestIsBrokenPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
	_, werr := w.Write([]byte("x"))
	w.Close()
	if werr == nil || !isBrokenPipe(werr) || !isBrokenPipe(fmt.Errorf("writing the report: %w", werr)) {
		t.Fatalf("a closed pipe: %v (%#v)", werr, werr)
	}
	for _, err := range []error{
		nil,
		errors.New("broken pipe"), // the words, not the error
		os.ErrClosed,
		&fs.PathError{Op: "write", Path: "/dev/stdout", Err: syscall.ENOSPC},
		fmt.Errorf("x: %w", syscall.EIO),
	} {
		if isBrokenPipe(err) {
			t.Fatalf("%v is not a broken pipe", err)
		}
	}
}
