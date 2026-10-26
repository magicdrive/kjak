//go:build !windows

package configure

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A secrets file another user can read or change is refused, before its
// content is parsed: group and other bits, whatever they are.
func TestCritical_SecretsFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.toml")
	write(t, path, "[a]\nb = \""+secretValue+"\"\n")
	for _, mode := range []os.FileMode{0o644, 0o640, 0o604, 0o620, 0o602, 0o660, 0o666, 0o700 | 0o004} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		_, err := ReadSecretsFile(path, false)
		if !errors.Is(err, ErrInsecureSecrets) {
			t.Errorf("mode %04o: %v", mode, err)
		} else if strings.Contains(err.Error(), secretValue) {
			t.Errorf("mode %04o: the error quotes the secret", mode)
		}
	}
	for _, mode := range []os.FileMode{0o600, 0o400} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if sf, err := ReadSecretsFile(path, false); err != nil || !sf.Has("a.b") {
			t.Errorf("mode %04o: %v", mode, err)
		}
	}
}

// fakeInfo is a FileInfo of another owner.
type fakeInfo struct {
	mode os.FileMode
	uid  uint32
}

func (f fakeInfo) Name() string       { return "x" }
func (f fakeInfo) Size() int64        { return 1 }
func (f fakeInfo) Mode() os.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }

func TestSecretsFileOwner(t *testing.T) {
	me := uint32(os.Geteuid())
	path := filepath.Join(t.TempDir(), "secrets.toml")
	write(t, path, "")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := checkPrivateFile(f, "x", fakeInfo{mode: 0o600, uid: me}, true); err != nil {
		t.Fatal(err)
	}
	if err := checkPrivateFile(f, "x", fakeInfo{mode: 0o600, uid: me + 1}, false); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "owned by uid") {
		t.Fatalf("another owner: %v", err)
	}
}

// The project secrets file must not be a symbolic link (a repository could
// link it to the user's own file); the personal file may be one (dotfiles),
// and what it leads to is checked.
func TestCritical_SecretsFileSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.toml")
	write(t, target, "[a]\nb = \"x\"\n")
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretsFile(link, true); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("a link with noFollow: %v", err)
	}
	if sf, err := ReadSecretsFile(link, false); err != nil || !sf.Has("a.b") {
		t.Fatalf("a link to a private file: %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretsFile(link, false); !errors.Is(err, ErrInsecureSecrets) {
		t.Fatalf("a link to a readable file: %v", err)
	}
	dangling := filepath.Join(dir, "dangling.toml")
	if err := os.Symlink(filepath.Join(dir, "nothing"), dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretsFile(dangling, true); !errors.Is(err, ErrInsecureSecrets) {
		t.Fatalf("a dangling link with noFollow: %v", err)
	}
}

// A FIFO (a process substitution) is refused at once: the open does not
// wait for a writer.
func TestSecretsFileFIFODoesNotBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadSecretsFile(path, false); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrInsecureSecrets) {
			t.Fatalf("a FIFO: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}

// migrate's files: new only, mode 0600, never through a link.
func TestCreateSecretsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "secrets.toml")
	if err := CreateSecretsFile(path, []byte("a = \"b\"\n")); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v %v", st.Mode(), err)
	}
	if err := CreateSecretsFile(path, []byte("x")); err == nil {
		t.Fatal("an existing file was overwritten")
	}
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(filepath.Join(dir, "elsewhere.toml"), link); err != nil {
		t.Fatal(err)
	}
	if err := CreateSecretsFile(link, []byte("x")); err == nil {
		t.Fatal("a file was created through a link")
	}
	if _, err := os.Lstat(filepath.Join(dir, "elsewhere.toml")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the link target was created")
	}
}

// A project secrets file with another name (a hard link: maybe the user's
// own file) is refused; the personal file may have several.
func TestCritical_ProjectSecretsHardLink(t *testing.T) {
	fx := secretsFixture(t)
	write(t, fx.paths.Secrets, "[slack]\nbot_token = \"xoxb-1\"\n")
	if err := os.Link(fx.paths.Secrets, fx.paths.ProjectSecrets); err != nil {
		t.Skip("hard links:", err)
	}
	if _, err := ReadSecretsFile(fx.paths.ProjectSecrets, true); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "hard links") {
		t.Fatalf("a linked project file: %v", err)
	}
	if _, err := ReadSecretsFile(fx.paths.Secrets, false); err != nil {
		t.Fatalf("the personal file: %v", err)
	}
	// the same file seen twice (a bind mount, a path through a link) is
	// refused as the project's
	personal, _ := ReadSecretsFile(fx.paths.Secrets, false)
	if !personal.SameFile(personal) {
		t.Fatal("SameFile")
	}
	if err := NotPersonal(fx.paths, personal); !errors.Is(err, ErrInsecureSecrets) {
		t.Fatalf("NotPersonal: %v", err)
	}
}
