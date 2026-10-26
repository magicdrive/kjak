//go:build linux

package fileacl

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// setACL sets the POSIX ACL of path as setfacl would (the kernel keeps the
// group bits of the mode as the mask); the entries go in the kernel's order
// (user_obj, users, group_obj, groups, mask, other).
func setACL(t *testing.T, path string, entries ...POSIXACE) {
	t.Helper()
	if err := unix.Setxattr(path, posixACLAccessXattr, posixACL(entries...), 0); err != nil {
		if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
			t.Skip("no POSIX ACLs on this file system:", err)
		}
		t.Fatal(err)
	}
}

func checkOpened(t *testing.T, path string, a Access) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	return CheckFile(f, a)
}

// Real POSIX ACLs (setfacl -m u:OTHER:r): a named entry that the mask lets
// through is refused; once chmod 600 masks it, it grants nothing.
func TestCritical_LinuxFileACL(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "secrets.toml")
	if err := os.WriteFile(p, []byte("a = \"b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a file without an ACL: %v", err)
	}
	other := uint32(os.Geteuid()) + 1
	setACL(t, p, POSIXACE{posixACLUserObj, 6, 0}, POSIXACE{posixACLUser, 4, other},
		POSIXACE{posixACLGroupObj, 0, 0}, POSIXACE{posixACLMask, 4, 0}, POSIXACE{posixACLOther, 0, 0})
	err := checkOpened(t, p, Private)
	if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "user:") {
		t.Fatalf("a named user: %v", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a masked entry: %v", err)
	}
	// a directory: a named group that can write
	setACL(t, dir, POSIXACE{posixACLUserObj, 7, 0}, POSIXACE{posixACLGroupObj, 5, 0},
		POSIXACE{posixACLGroup, 7, other}, POSIXACE{posixACLMask, 7, 0}, POSIXACE{posixACLOther, 5, 0})
	if err := CheckPath(dir, NoWrite); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("a writable directory: %v", err)
	}
	if err := os.Chmod(dir, 0o775|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if err := CheckPath(dir, NoReplace); err != nil {
		t.Fatalf("a sticky directory above: %v", err)
	}
}
