//go:build linux

package fileacl

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Model is what is checked on this OS (kjak config path).
const Model = "owner, mode bits, POSIX ACL"

const posixACLAccessXattr = "system.posix_acl_access"

// CheckFile checks the POSIX ACL of the opened file f.
func CheckFile(f *os.File, a Access) error {
	return checkXattr(func(buf []byte) (int, error) {
		return unix.Fgetxattr(int(f.Fd()), posixACLAccessXattr, buf)
	}, a, false)
}

// CheckPath checks the POSIX ACL of path (a directory; links followed).
func CheckPath(path string, a Access) error {
	sticky := false
	if st, err := os.Stat(path); err == nil {
		sticky = st.Mode()&os.ModeSticky != 0
	}
	return checkXattr(func(buf []byte) (int, error) {
		return unix.Getxattr(path, posixACLAccessXattr, buf)
	}, a, sticky)
}

func checkXattr(get func([]byte) (int, error), a Access, sticky bool) error {
	buf := make([]byte, 1024)
	n, err := get(buf)
	if errors.Is(err, unix.ERANGE) {
		if n, err = get(nil); err == nil {
			buf = make([]byte, n)
			n, err = get(buf)
		}
	}
	switch {
	case errors.Is(err, unix.ENODATA), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP):
		return nil // no ACL, or a file system without POSIX ACLs
	case err != nil:
		return fmt.Errorf("%w: its POSIX ACL cannot be read: %v", ErrNotPrivate, err)
	}
	aces, err := ParsePOSIXACL(buf[:n])
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotPrivate, err)
	}
	return CheckPOSIXACL(aces, POSIXPolicy{Access: a, UID: uint32(os.Geteuid()), RootTrusted: a != Private, Sticky: sticky})
}
