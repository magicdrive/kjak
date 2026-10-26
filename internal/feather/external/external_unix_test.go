//go:build !windows

package external

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
)

// A file that is not executable, or that someone else could change, is
// refused; a symbolic link (Homebrew's) runs what it leads to.
func TestCritical_PluginFileChecks(t *testing.T) {
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
	for mode, want := range map[os.FileMode]string{0o644: "not executable", 0o775: "writable by group", 0o757: "writable by group or others"} {
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		check(want)
	}
	if err := os.Chmod(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	check("writable by others")
	if err := os.Chmod(dir, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	check("")
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// a directory named like a plugin is no plugin
	if err := os.Mkdir(filepath.Join(root, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "d", configure.PluginFile("dir", runtime.GOOS)), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve("dir", "", filepath.Join(root, "d"), runtime.GOOS); err == nil {
		t.Fatal("a directory was taken for a plugin")
	}
	// a link in a PATH directory runs its target
	links := filepath.Join(root, "links")
	if err := os.MkdirAll(links, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(p, filepath.Join(links, configure.PluginFile("test", runtime.GOOS))); err != nil {
		t.Fatal(err)
	}
	exe, err := Resolve("test", "", links, runtime.GOOS)
	if err != nil || exe.Real != p {
		t.Fatalf("link: %+v %v", exe, err)
	}
	// a dangling link is refused, not skipped for a later directory
	if err := os.Symlink(filepath.Join(root, "nothing"), filepath.Join(links, configure.PluginFile("gone", runtime.GOOS))); err != nil {
		t.Fatal(err)
	}
	install(t, filepath.Join(root, "later"), "gone")
	if _, err := Resolve("gone", "", links+string(os.PathListSeparator)+filepath.Join(root, "later"), runtime.GOOS); err == nil {
		t.Fatal("a refused candidate was skipped for a later one")
	}
}

// A timeout kills the plugin's whole process group: a child it started
// does not outlive it.
func TestCritical_PluginProcessGroupKilled(t *testing.T) {
	pidfile := filepath.Join(tempDir(t), "child.pid")
	f := plugin(t, map[string]string{"mode": "fork", "pidfile": pidfile}, func(fc *configure.FeatherConfig) {
		fc.Timeout = &configure.Duration{Duration: 2 * time.Second}
	})
	o := f.Deliver(context.Background(), msg())
	if o.Acceptance != core.AcceptanceUnknown {
		t.Fatalf("%+v", o)
	}
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("the child did not start: %v", err)
	}
	pid, _ := strconv.Atoi(string(b))
	for i := 0; i < 100; i++ {
		if err := syscall.Kill(pid, 0); err != nil {
			return // gone
		}
		var ws syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
		time.Sleep(50 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	t.Fatalf("the child %d outlived the plugin", pid)
}
