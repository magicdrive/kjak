package configure

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A full disk (the sync of the new content fails): the old file stays, no
// temporary file or lock is left.
func TestWriteFailureKeepsOldFile(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	syncFile = func(*os.File) error { return syscall.ENOSPC }
	t.Cleanup(func() { syncFile = func(f *os.File) error { return f.Sync() } })
	err := Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Templates["x"] = TemplateConfig{Body: "x"}
		return nil
	})
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("err = %v", err)
	}
	b, _ := os.ReadFile(fx.paths.Global)
	if string(b) != globalTOML {
		t.Fatal("the old file changed")
	}
	entries, _ := os.ReadDir(filepath.Dir(fx.paths.Global))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("left behind: %s", e.Name())
		}
	}
	assertUnlocked(t, fx.paths.Global)
}

// What a kjak killed in the middle of a write leaves (its lock file and a
// temporary file): reads work, and so does the next write — the lock died
// with its process.
func TestCrashLeftovers(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	dir := filepath.Dir(fx.paths.Global)
	write(t, fx.paths.Global+".lock", "4242\n")
	write(t, filepath.Join(dir, ".kjak-123.tmp"), "[half")
	l, err := Load(fx.paths)
	if err != nil || len(l.Effective.Feathers) != 2 {
		t.Fatalf("load after a crash: %v", err)
	}
	if err := Update(fx.paths, ScopeGlobal, t0, func(*File) error { return nil }); err != nil {
		t.Fatalf("a lock file left by a dead process blocks: %v", err)
	}
}

// The project file is replaced while kjak edits it (after its trust was
// checked): the edit stops and the trust does not move to either content.
func TestCritical_ProjectSwappedDuringEdit(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	write(t, fx.paths.Project, projectTOML)
	src, _ := ReadFile(fx.paths.Project)
	if err := Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0); err != nil {
		t.Fatal(err)
	}
	evil := projectTOML + "\n[feathers.evil]\ntype = \"slack\"\nwebhook_url = { env = \"AWS_SECRET_ACCESS_KEY\" }\n"
	beforeCommit = func(path string) { write(t, path, evil) }
	t.Cleanup(func() { beforeCommit = nil })
	err := Update(fx.paths, ScopeProject, t0, func(f *File) error {
		f.Templates = map[string]TemplateConfig{"t": {Body: "x"}}
		return nil
	})
	if !errors.Is(err, ErrConcurrentModification) {
		t.Fatalf("err = %v", err)
	}
	if _, err := Load(fx.paths); !errors.Is(err, ErrUntrustedProject) {
		t.Fatalf("the swapped file is used: %v", err)
	}
}
