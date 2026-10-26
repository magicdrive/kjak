//go:build windows

package external

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func icacls(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("icacls", args...).CombinedOutput(); err != nil {
		t.Fatalf("icacls %v: %v %s", args, err, out)
	}
}

// A plugin someone else could change, or that sits in a directory where
// someone else could put another file, is refused (its DACL, and those of
// its directories).
func TestCritical_WindowsPluginACL(t *testing.T) {
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
	// read and execute for everyone is how programs are installed
	icacls(t, p, "/grant", "*S-1-1-0:(RX)")
	check("")
	icacls(t, p, "/grant", "*S-1-1-0:(M)")
	check("Everyone")
	icacls(t, p, "/remove:g", "*S-1-1-0")
	check("")
	icacls(t, dir, "/grant", "*S-1-5-32-545:(AD,WD)")
	check("its directory")
	icacls(t, dir, "/remove:g", "*S-1-5-32-545")
	check("")
	// above it: adding entries is fine, deleting them is not
	icacls(t, root, "/grant", "*S-1-5-11:(AD,WD)")
	check("")
	icacls(t, root, "/grant", "*S-1-5-11:(DC)")
	check("above it")
	icacls(t, root, "/remove:g", "*S-1-5-11")
	check("")
}

// A plugin cannot start a child outside its job.
func TestCritical_WindowsJobNoBreakaway(t *testing.T) {
	d := tempDir(t)
	pidfile, result := filepath.Join(d, "child.pid"), filepath.Join(d, "result")
	f := plugin(t, map[string]string{"mode": "breakaway", "pidfile": pidfile, "result": result}, nil)
	f.Deliver(context.Background(), msg())
	b, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "refused") {
		if pid, err := waitPID(pidfile); err == nil {
			killProcess(pid)
		}
		t.Fatalf("a child broke away from the job: %s", b)
	}
	t.Logf("CREATE_BREAKAWAY_FROM_JOB: %s", b)
}

func waitPID(pidfile string) (int, error) {
	var err error
	for i := 0; i < 200; i++ {
		var b []byte
		if b, err = os.ReadFile(pidfile); err == nil {
			return strconv.Atoi(string(b))
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0, err
}

var procGetProcessHandleCount = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")

func handleCount(t *testing.T) int {
	t.Helper()
	var n uint32
	if r, _, err := procGetProcessHandleCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&n))); r == 0 {
		t.Fatal(err)
	}
	return int(n)
}

// Every run releases its job, process and thread handles.
func TestWindowsPluginRunsDoNotLeakHandles(t *testing.T) {
	f := sharedPlugin(t, map[string]string{"mode": "ok"}, nil)
	tree, pidfile := treeFeather(t, "exit", 10*time.Second)
	f.Deliver(context.Background(), msg()) // warm up
	before := handleCount(t)
	const runs = 20
	for i := 0; i < runs; i++ {
		f.Deliver(context.Background(), msg())
	}
	tree.Deliver(context.Background(), msg())
	waitGone(t, "tree", treePIDs(t, pidfile))
	after := handleCount(t)
	t.Logf("handles: %d before, %d after %d runs", before, after, runs+1)
	if after-before >= runs/2 {
		t.Fatalf("handles leak: %d before, %d after %d runs", before, after, runs+1)
	}
}

// A plugin that could not be put in a job never runs: newContainment's
// job is closed before the start.
func TestWindowsContainmentClosedJob(t *testing.T) {
	cmd := exec.Command(built)
	c, err := newContainment(cmd)
	if err != nil {
		t.Fatal(err)
	}
	c.close() // as if the job had gone
	cmd.Stdin = strings.NewReader("{}")
	if err := c.start(); err == nil || !strings.Contains(err.Error(), "job") {
		_ = cmd.Process.Kill()
		t.Fatalf("a plugin ran outside a job: %v", err)
	}
	if cmd.ProcessState == nil {
		t.Fatal("the suspended process was not waited for")
	}
}
