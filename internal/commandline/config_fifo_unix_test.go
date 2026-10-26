//go:build !windows

package commandline_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// A configuration file that is a FIFO (or a link to one, or to a device)
// is refused at once: kjak never waits for a writer that a repository
// planted as .kjak.toml.
func TestCritical_ConfigFileThatIsNotARegularFileIsRefused(t *testing.T) {
	for _, target := range []string{"fifo", "link to fifo", "link to device", "directory"} {
		t.Run(target, func(t *testing.T) {
			e := newEnv(t)
			project := filepath.Join(e.project, ".kjak.toml")
			fifo := filepath.Join(t.TempDir(), "fifo")
			if err := syscall.Mkfifo(fifo, 0o600); err != nil {
				t.Fatal(err)
			}
			var err error
			switch target {
			case "fifo":
				err = syscall.Mkfifo(project, 0o600)
			case "link to fifo":
				err = os.Symlink(fifo, project)
			case "link to device":
				err = os.Symlink("/dev/zero", project)
			case "directory":
				err = os.Mkdir(project, 0o700)
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, args := range [][]string{{"config", "validate"}, {"config", "trust", "--yes"}, {"send", "--to", "x", "hi"}} {
				done := make(chan result, 1)
				go func() { done <- e.run(args...) }()
				r := waitResult(t, done, "kjak waited on a configuration file that is not a regular file")
				if r.code != 2 || !strings.Contains(r.stderr, "not a regular file") {
					t.Fatalf("%v: %+v", args, r)
				}
			}
		})
	}
}
