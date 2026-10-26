//go:build windows

package configure

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func icacls(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("icacls", args...).CombinedOutput(); err != nil {
		t.Fatalf("icacls %v: %v %s", args, err, out)
	}
}

// A secrets file other users can read is refused, before its content is
// parsed: an ACE for Everyone, Users or Authenticated Users, explicit or
// inherited, and a null DACL.
func TestCritical_WindowsSecretsFileACL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.toml")
	write(t, path, "[a]\nb = \""+secretValue+"\"\n")
	if sf, err := ReadSecretsFile(path, false); err != nil || !sf.Has("a.b") {
		t.Fatalf("a file of the temporary directory: %v", err)
	}
	for sid, name := range map[string]string{"S-1-1-0": "Everyone", "S-1-5-32-545": "Users", "S-1-5-11": "Authenticated Users"} {
		icacls(t, path, "/grant", "*"+sid+":(R)")
		_, err := ReadSecretsFile(path, false)
		if !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), name) {
			t.Errorf("%s can read: %v", name, err)
		} else if strings.Contains(err.Error(), secretValue) {
			t.Error("the error quotes the secret")
		}
		icacls(t, path, "/remove:g", "*"+sid)
	}
	if _, err := ReadSecretsFile(path, false); err != nil {
		t.Fatalf("the grants removed: %v", err)
	}
	// inherited from the directory
	icacls(t, dir, "/grant", "*S-1-1-0:(OI)(R)")
	inherited := filepath.Join(dir, "inherited.toml")
	write(t, inherited, "[a]\nb = \"c\"\n")
	if _, err := ReadSecretsFile(inherited, false); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "inherited") {
		t.Fatalf("an inherited ACE: %v", err)
	}
	// a null DACL
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSecretsFile(path, false); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "no DACL") {
		t.Fatalf("a null DACL: %v", err)
	}
}

// kjak's own secrets files (config migrate-secrets) get a protected DACL:
// private even in a directory that passes Everyone on.
func TestCritical_WindowsCreateSecretsFile(t *testing.T) {
	dir := t.TempDir()
	icacls(t, dir, "/grant", "*S-1-1-0:(OI)(CI)(F)")
	path := filepath.Join(dir, "sub", "secrets.toml")
	if err := CreateSecretsFile(path, []byte("a = \"b\"\n")); err != nil {
		t.Fatal(err)
	}
	if sf, err := ReadSecretsFile(path, true); err != nil || !sf.Has("a") {
		t.Fatalf("a file kjak made: %v", err)
	}
	if err := CreateSecretsFile(path, []byte("x")); err == nil {
		t.Fatal("an existing file was overwritten")
	}
}

// The project secrets file: a hard link (maybe the personal file) and a
// symbolic link are refused on the handle; the personal file may be a link.
func TestCritical_WindowsProjectSecretsLinks(t *testing.T) {
	fx := secretsFixture(t)
	write(t, fx.paths.Secrets, "[slack]\nbot_token = \"xoxb-1\"\n")
	if err := os.Link(fx.paths.Secrets, fx.paths.ProjectSecrets); err != nil {
		t.Fatal("hard links:", err)
	}
	if _, err := ReadSecretsFile(fx.paths.ProjectSecrets, true); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "hard links") {
		t.Fatalf("a hard-linked project file: %v", err)
	}
	if err := os.Remove(fx.paths.ProjectSecrets); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fx.paths.Secrets, fx.paths.ProjectSecrets); err != nil {
		t.Skip("symbolic links need a privilege:", err)
	}
	if _, err := ReadSecretsFile(fx.paths.ProjectSecrets, true); !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "reparse point") {
		t.Fatalf("a linked project file: %v", err)
	}
	if sf, err := ReadSecretsFile(fx.paths.ProjectSecrets, false); err != nil || !sf.Has("slack.bot_token") {
		t.Fatalf("a link to a private file, followed: %v", err)
	}
}

// While kjak reads a secrets file, it cannot be replaced or written to.
func TestWindowsSecretsFileLockedWhileRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.toml")
	write(t, path, "a = \"b\"\n")
	f, err := openSecrets(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	other := filepath.Join(dir, "other.toml")
	write(t, other, "a = \"evil\"\n")
	if err := os.Rename(other, path); err == nil {
		t.Fatal("the file was replaced while open")
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err == nil {
		t.Fatal("the file was written while open")
	}
}
