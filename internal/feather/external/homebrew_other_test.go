//go:build !darwin && !windows

package external

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Outside macOS there is no Homebrew exception: a directory its group can
// write to is refused whatever accept says.
func TestCritical_NoHomebrewExceptionOutsideMacOS(t *testing.T) {
	root := tempDir(t)
	keg := filepath.Join(root, "Cellar", "kjak-test", "1.0", "bin")
	install(t, keg, "test")
	if err := os.Chmod(filepath.Join(root, "Cellar"), 0o775); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveAccept("test", "", keg, runtime.GOOS, func(string, string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("want the strict refusal, got %v", err)
	}
}
