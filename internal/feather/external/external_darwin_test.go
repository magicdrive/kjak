//go:build darwin

package external

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func chmodACL(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("/bin/chmod", args...).CombinedOutput(); err != nil {
		t.Fatalf("chmod %v: %v %s", args, err, out)
	}
}

// The mode bits of a plugin and its directories can be right while an ACL
// entry lets everyone change them: refused.
func TestCritical_DarwinPluginACL(t *testing.T) {
	root := tempDir(t)
	dir := filepath.Join(root, "bin")
	p := install(t, dir, "test")
	check := func(want string) {
		t.Helper()
		_, err := Resolve("test", "", dir, runtime.GOOS)
		if want == "" && err != nil {
			t.Fatalf("refused: %v", err)
		}
		if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Fatalf("want %q, got %v", want, err)
		}
	}
	check("")
	chmodACL(t, "+a", "everyone allow read,execute", p)
	check("")
	chmodACL(t, "+a", "everyone allow write", p)
	check("group:everyone")
	chmodACL(t, "-N", p)
	chmodACL(t, "+a", "everyone allow add_file", dir)
	check("its directory")
	chmodACL(t, "-N", dir)
	chmodACL(t, "+a", "everyone allow add_file,add_subdirectory", root)
	check("")
	chmodACL(t, "+a", "everyone allow delete_child", root)
	check("above it")
	chmodACL(t, "-N", root)
	check("")
}
