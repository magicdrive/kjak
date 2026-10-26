//go:build darwin

package fileacl

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// chmodACL runs chmod with an ACL operation (chmod +a "...", chmod -N).
func chmodACL(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("chmod", args...).CombinedOutput(); err != nil {
		t.Fatalf("chmod %v: %v %s", args, err, out)
	}
}

func newFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "secrets.toml")
	if err := os.WriteFile(p, []byte("a = \"b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func checkOpened(t *testing.T, path string, a Access) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	return CheckFile(f, a)
}

// Real ACLs, set with chmod +a: an allow entry for everyone (or for the
// user, which kjak cannot tell apart without the directory services) is
// refused, a deny entry is not; an entry inherited from the directory is
// refused as an explicit one.
func TestCritical_DarwinFileACL(t *testing.T) {
	dir := t.TempDir()
	p := newFile(t, dir)
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a file without an ACL: %v", err)
	}
	if err := CheckPath(dir, NoWrite); err != nil {
		t.Fatalf("a directory without an ACL: %v", err)
	}

	chmodACL(t, "+a", "everyone allow read", p)
	if err := checkOpened(t, p, Private); !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "group:everyone") {
		t.Fatalf("everyone allow read: %v", err)
	}
	if err := checkOpened(t, p, NoWrite); err != nil {
		t.Fatalf("reading is no change: %v", err)
	}
	chmodACL(t, "+a", "everyone allow write", p)
	if err := checkOpened(t, p, NoWrite); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("everyone allow write: %v", err)
	}
	chmodACL(t, "-N", p)
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("after chmod -N: %v", err)
	}

	chmodACL(t, "+a", "user:nobody deny read,write,delete", p)
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a deny entry: %v", err)
	}
	chmodACL(t, "-N", p)

	if u, err := user.Current(); err == nil {
		chmodACL(t, "+a", "user:"+u.Username+" allow read", p)
		if err := checkOpened(t, p, Private); !errors.Is(err, ErrNotPrivate) {
			t.Fatalf("an entry for the user is refused too (fail closed): %v", err)
		}
		chmodACL(t, "-N", p)
	}

	// inherited: a directory whose entries pass "everyone allow read" on
	inh := filepath.Join(dir, "inherit")
	if err := os.Mkdir(inh, 0o700); err != nil {
		t.Fatal(err)
	}
	chmodACL(t, "+a", "everyone allow read,file_inherit,only_inherit", inh)
	if err := CheckPath(inh, NoWrite); err != nil {
		t.Fatalf("an only-inherit entry does not apply to the directory: %v", err)
	}
	q := newFile(t, inh)
	if err := checkOpened(t, q, Private); !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "inherited") {
		t.Fatalf("an inherited entry: %v", err)
	}

	// a directory: adding a file is NoWrite's business, removing one is
	// NoReplace's
	chmodACL(t, "+a", "everyone allow add_file", dir)
	if err := CheckPath(dir, NoWrite); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("everyone allow add_file: %v", err)
	}
	if err := CheckPath(dir, NoReplace); err != nil {
		t.Fatalf("add_file above a plugin: %v", err)
	}
	chmodACL(t, "+a", "everyone allow delete_child", dir)
	if err := CheckPath(dir, NoReplace); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("everyone allow delete_child: %v", err)
	}
	chmodACL(t, "-N", dir)
}
