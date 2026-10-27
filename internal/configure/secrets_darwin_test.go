//go:build darwin

package configure

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A secrets file of mode 0600 that an ACL entry lets everyone read is
// refused: the mode bits are not the whole story on macOS.
func TestCritical_DarwinSecretsFileACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.toml")
	write(t, path, "[a]\nb = \""+secretValue+"\"\n")
	if out, err := exec.Command("/bin/chmod", "+a", "everyone allow read", path).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a: %v %s", err, out)
	}
	_, err := ReadSecretsFile(path, false)
	if !errors.Is(err, ErrInsecureSecrets) || !strings.Contains(err.Error(), "group:everyone") {
		t.Fatalf("an ACL entry for everyone: %v", err)
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatal("the error quotes the secret")
	}
	if out, err := exec.Command("/bin/chmod", "-N", path).CombinedOutput(); err != nil {
		t.Fatalf("chmod -N: %v %s", err, out)
	}
	if sf, err := ReadSecretsFile(path, false); err != nil || !sf.Has("a.b") {
		t.Fatalf("the ACL removed: %v", err)
	}
}
