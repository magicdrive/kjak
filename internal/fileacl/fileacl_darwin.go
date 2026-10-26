//go:build darwin

package fileacl

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// Model is what is checked on this OS (kjak config path).
const Model = "owner, mode bits, ACL (extended security)"

// The ACL is read with getattrlist(2) / fgetattrlist(2) for
// ATTR_CMN_EXTENDED_SECURITY: the kernel returns the kauth_filesec that
// acl_get_fd(3) would decode. x/sys/unix wraps neither call and cgo is not
// used (CGO_ENABLED=0 release builds): getattrlist_darwin.go calls them in
// libSystem the way x/sys does. A failure of the call fails the check
// (fail closed).

// maxFilesec bounds the buffer: a header and ACL_MAX_ENTRIES entries.
const maxFilesec = attrlistHeaderAndAttr + kauthFilesecHeader + maxDarwinACLEntries*kauthACESize

// CheckFile checks the ACL of the opened file f.
func CheckFile(f *os.File, a Access) error {
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	buf := make([]byte, maxFilesec)
	if err := fgetattrlist(f.Fd(), &attrs, buf); err != nil {
		return fmt.Errorf("%w: its ACL cannot be read: %v", ErrNotPrivate, err)
	}
	return checkAttrBuf(buf, a)
}

// CheckPath checks the ACL of path (a directory; links followed).
func CheckPath(path string, a Access) error {
	attrs := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_EXTENDED_SECURITY}
	buf := make([]byte, maxFilesec)
	if err := getattrlist(path, &attrs, buf); err != nil {
		return fmt.Errorf("%w: the ACL of %s cannot be read: %v", ErrNotPrivate, path, err)
	}
	return checkAttrBuf(buf, a)
}

func checkAttrBuf(buf []byte, a Access) error {
	fs, err := ParseAttrlistFilesec(buf)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotPrivate, err)
	}
	aces, err := ParseFilesec(fs)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotPrivate, err)
	}
	return CheckDarwinACL(aces, a)
}
