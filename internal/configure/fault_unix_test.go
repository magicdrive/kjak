//go:build !windows

package configure

import (
	"os"
	"testing"
)

// An unreadable trust store fails closed: the project file is not used.
func TestUnreadableTrustStoreFailsClosed(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Fatal("run the tests as a regular user: root reads any file")
	}
	fx := newFixture(t)
	write(t, fx.paths.Project, projectTOML)
	write(t, fx.paths.Global, globalTOML)
	src, _ := ReadFile(fx.paths.Project)
	_ = Trust(fx.paths.TrustStore, fx.paths.Project, src.Hash, t0)
	if err := os.Chmod(fx.paths.TrustStore, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(fx.paths.TrustStore, 0o600) })
	if l, err := Load(fx.paths); err == nil {
		t.Fatalf("loaded with an unreadable trust store (project trusted: %t)", l.ProjectTrusted)
	}
}
