//go:build !windows

package configure

import (
	"os"
	"syscall"
)

// openConfig opens a configuration file (a configuration, the trust store)
// for reading without blocking: a FIFO planted as .kjak.toml would block
// open(2) until a writer appears (O_NONBLOCK; the type of the file is
// checked after the open, on the descriptor).
func openConfig(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
