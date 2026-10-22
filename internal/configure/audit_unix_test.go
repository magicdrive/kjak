//go:build !windows

// Unix only: symbolic links (a privilege on Windows) and permission bits.

package configure

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A configuration file that is a symbolic link (a dotfiles repository) stays
// one: kjak writes the file it points to.
func TestUpdateWritesThroughSymlink(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(fx.dir, "dotfiles", "kjak.toml")
	write(t, target, globalTOML)
	if err := os.MkdirAll(filepath.Dir(fx.paths.Global), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, fx.paths.Global); err != nil {
		t.Fatal(err)
	}
	err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Templates["added"] = TemplateConfig{Body: "x"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(fx.paths.Global)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symbolic link was replaced: %v %v", st.Mode(), err)
	}
	src, err := ReadFile(target)
	if err != nil || src.File.Templates["added"].Body != "x" {
		t.Fatalf("the target was not written: %v", err)
	}
}

// kjak keeps the permissions of a file it rewrites, except that it never
// leaves it writable by others; a new file is 0600.
func TestUpdateKeepsPermissions(t *testing.T) {
	for _, tc := range []struct{ before, after os.FileMode }{{0o640, 0o640}, {0o600, 0o600}, {0o666, 0o644}, {0o644, 0o644}} {
		fx := newFixture(t)
		write(t, fx.paths.Global, globalTOML)
		if err := os.Chmod(fx.paths.Global, tc.before); err != nil {
			t.Fatal(err)
		}
		if err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error { return nil }); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(fx.paths.Global)
		if st.Mode().Perm() != tc.after {
			t.Errorf("%o became %o, want %o", tc.before, st.Mode().Perm(), tc.after)
		}
	}
}

// A write that fails leaves the old file and no temporary file behind.
func TestFailedWriteLeavesOldFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("run the tests as a regular user: root ignores directory permissions")
	}
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	dir := filepath.Dir(fx.paths.Global)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Templates["x"] = TemplateConfig{Body: "x"}
		return nil
	})
	if err == nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err = %v", err)
	}
	b, _ := os.ReadFile(fx.paths.Global)
	if string(b) != globalTOML {
		t.Fatal("the old file changed")
	}
}

// A project file that is a link to nothing is not followed: kjak does not
// create a file where a link of a repository points.
func TestDanglingSymlinkNotWritten(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(fx.dir, "elsewhere", "planted.toml")
	if err := os.Symlink(target, fx.paths.Project); err != nil {
		t.Fatal(err)
	}
	err := Update(fx.paths, ScopeProject, t0, func(f *File) error {
		f.Templates = map[string]TemplateConfig{"t": {Body: "x"}}
		return nil
	})
	if err == nil {
		t.Fatal("wrote through a dangling link")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("created %s", target)
	}
}

// A lock file that is a symbolic link (planted next to .kjak.toml) is not
// followed.
func TestSymlinkedLockRefused(t *testing.T) {
	fx := newFixture(t)
	target := filepath.Join(fx.dir, "victim")
	if err := os.Symlink(target, fx.paths.Project+".lock"); err != nil {
		t.Fatal(err)
	}
	err := Update(fx.paths, ScopeProject, t0, func(f *File) error {
		f.Templates = map[string]TemplateConfig{"t": {Body: "x"}}
		return nil
	})
	if err == nil {
		t.Fatal("locked through a symbolic link")
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the link target was created")
	}
}
