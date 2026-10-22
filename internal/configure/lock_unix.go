//go:build !windows

package configure

import (
	"errors"
	"os"
	"syscall"
)

// openLock opens the lock file without following a symbolic link (a link a
// repository planted next to .kjak.toml).
func openLock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
}

// tryLock takes an exclusive advisory lock (flock) without waiting; the OS
// releases it when the file is closed or the process ends.
func tryLock(f *os.File) (held bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}
