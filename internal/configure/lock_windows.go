//go:build windows

package configure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"golang.org/x/sys/windows"
)

// openLock opens the lock file; a symbolic link (a reparse point) is refused.
func openLock(path string) (*os.File, error) {
	if st, err := os.Lstat(path); err == nil && st.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%s is a symbolic link", path)
	}
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

// tryLock takes an exclusive lock (LockFileEx) without waiting; Windows
// releases it when the handle is closed or the process ends.
func tryLock(f *os.File) (held bool, err error) {
	var ol windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return true, nil
	}
	return false, err
}
