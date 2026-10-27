package configure

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

// heldLock keeps the lock of TestHelperHoldLock reachable until the process
// is killed.
var heldLock func()

// TestHelperHoldLock is not a test: run by TestCritical_LockReleasedWhenHolderDies
// as another process, it takes the lock of KJAK_LOCK_PATH, says "locked" and
// waits to be killed.
func TestHelperHoldLock(t *testing.T) {
	path := os.Getenv("KJAK_LOCK_PATH")
	if path == "" {
		return // run as a test: nothing to do
	}
	unlock, err := lockFile(path)
	if err != nil {
		_, _ = os.Stdout.WriteString("error " + err.Error() + "\n")
		os.Exit(3)
	}
	// Never released: the process is killed. The lock must stay reachable,
	// or the finalizer of its *os.File closes the descriptor at the next
	// collection and the flock goes with it (macos-15-intel, run
	// 40469622700). Collect now so that a regression fails every run.
	heldLock = unlock
	runtime.GC()
	runtime.GC()
	time.Sleep(50 * time.Millisecond) // finalizers run on their own goroutine
	_, _ = os.Stdout.WriteString("locked\n")
	select {}
}

// A kjak killed while it holds the lock leaves nothing that blocks the next
// one: the OS releases the lock with the process (no stale lock, no PID
// guessing).
func TestCritical_LockReleasedWhenHolderDies(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperHoldLock$")
	cmd.Env = append(os.Environ(), "KJAK_LOCK_PATH="+fx.paths.Global)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		_ = cmd.Process.Kill()
		t.Fatalf("helper: %q", line)
	}
	// held by a live process: refused
	if err := Update(fx.paths, ScopeGlobal, t0, func(*File) error { return nil }); !errors.Is(err, ErrLocked) {
		_ = cmd.Process.Kill()
		t.Fatalf("while held: err = %v", err)
	}
	// the holder crashes
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	err = Update(fx.paths, ScopeGlobal, t0, func(f *File) error {
		f.Templates["after_crash"] = TemplateConfig{Body: "x"}
		return nil
	})
	if err != nil {
		t.Fatalf("after the holder died: %v", err)
	}
}

// Within one process too, a second writer is refused while the first holds
// the lock, and admitted after it released it.
func TestLockInProcess(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.paths.Global, globalTOML)
	unlock, err := lockFile(fx.paths.Global)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockFile(fx.paths.Global); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	unlock()
	unlock2, err := lockFile(fx.paths.Global)
	if err != nil {
		t.Fatalf("after release: %v", err)
	}
	unlock2()
}
