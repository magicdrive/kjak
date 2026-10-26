package external

import (
	"path/filepath"
	"testing"
)

// The PATH split of the plugin search, for any PATH value: every element is
// either searched (absolute, cleaned) or reported as skipped (empty, or
// relative: the current directory is never searched); none is lost.
func FuzzSearchDirs(f *testing.F) {
	sep := string(filepath.ListSeparator)
	for _, s := range []string{"", "/usr/bin", "/usr/bin" + sep + sep + "bin" + sep + "/opt//x/../y", ".", "./bin" + sep + "/a", `C:\Windows`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, pathList string) {
		dirs, skipped := searchDirs(pathList)
		if got, want := len(dirs)+len(skipped), len(filepath.SplitList(pathList)); got != want {
			t.Fatalf("%q: %d searched + skipped, %d elements", pathList, got, want)
		}
		for _, d := range dirs {
			if !filepath.IsAbs(d) || d != filepath.Clean(d) {
				t.Fatalf("%q: searched %q", pathList, d)
			}
		}
		for _, s := range skipped {
			if s.Element != "" && filepath.IsAbs(s.Element) {
				t.Fatalf("%q: absolute %q skipped", pathList, s.Element)
			}
			if s.Why == "" {
				t.Fatalf("%q: %q skipped without a reason", pathList, s.Element)
			}
		}
	})
}
