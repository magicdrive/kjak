//go:build !windows

package configure

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// openSecrets opens a secrets file for reading: without blocking on a FIFO
// (O_NONBLOCK; the type is checked after the open) and, with noFollow,
// without following a symbolic link at path (O_NOFOLLOW).
func openSecrets(path string, noFollow bool) (*os.File, error) {
	flags := os.O_RDONLY | syscall.O_NONBLOCK
	if noFollow {
		flags |= syscall.O_NOFOLLOW
	}
	f, err := os.OpenFile(path, flags, 0)
	if err != nil && noFollow && !errors.Is(err, fs.ErrNotExist) {
		// ELOOP (Linux, macOS) or EMLINK (BSD) for a link: say so
		if st, lerr := os.Lstat(path); lerr == nil && st.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: it is a symbolic link, which is refused here (a repository could link it to your own secrets)", ErrInsecureSecrets)
		}
	}
	return f, err
}

// checkSecretsInfo checks the opened file: a regular file of the effective
// user, with no permission for group or others. With single (the project
// file), a file with another name (a hard link) is refused too: it could be
// the user's personal file under the project's name.
func checkSecretsInfo(path string, st os.FileInfo, single bool) error {
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%w: it is not a regular file", ErrInsecureSecrets)
	}
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		if single && sys.Nlink > 1 {
			return fmt.Errorf("%w: it has %d names (hard links), which is refused here (it could be your own secrets file under the project's name)", ErrInsecureSecrets, sys.Nlink)
		}
		if uid := os.Geteuid(); int(sys.Uid) != uid {
			return fmt.Errorf("%w: it is owned by uid %d, not by the user running kjak (uid %d)", ErrInsecureSecrets, sys.Uid, uid)
		}
	}
	if perm := st.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Errorf("%w: group or others have access (mode %04o); run: chmod 600 %s", ErrInsecureSecrets, perm, path)
	}
	return nil
}

// SecretsChecksPermissions: on Unix the owner and the mode of a secrets file
// are checked.
const SecretsChecksPermissions = true
