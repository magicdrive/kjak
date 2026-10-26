package external

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
)

// treePIDs are the pids of the child and the grandchild of a plugin run in
// mode "tree".
func treePIDs(t *testing.T, pidfile string) []int {
	t.Helper()
	var pids []int
	for _, n := range []string{"2", "1"} {
		b, err := os.ReadFile(pidfile + "." + n)
		if err != nil {
			t.Fatalf("the descendant %s did not start: %v", n, err)
		}
		pid, err := strconv.Atoi(string(b))
		if err != nil {
			t.Fatal(err)
		}
		pids = append(pids, pid)
	}
	return pids
}

// waitGone waits until none of pids is alive (a few seconds at most).
func waitGone(t *testing.T, what string, pids []int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var alive []int
		for _, p := range pids {
			if processAlive(p) {
				alive = append(alive, p)
			}
		}
		if len(alive) == 0 {
			return
		}
		if time.Now().After(deadline) {
			for _, p := range alive {
				killProcess(p)
			}
			t.Fatalf("%s: the descendants %v outlived the plugin", what, alive)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func treeFeather(t *testing.T, then string, timeout time.Duration) (*Feather, string) {
	t.Helper()
	pidfile := filepath.Join(tempDir(t), "tree.pid")
	f := sharedPlugin(t, map[string]string{"mode": "tree", "pidfile": pidfile, "then": then}, func(fc *configure.FeatherConfig) {
		fc.Timeout = &configure.Duration{Duration: timeout}
	})
	return f, pidfile
}

// The plugin's whole tree (child and grandchild) is killed whichever way
// the run ends: the plugin's timeout, a cancel, a crash, or a plugin that
// answered and exited, leaving its children behind.
func TestCritical_PluginTreeKilled(t *testing.T) {
	for _, then := range []string{"sleep", "crash", "exit"} {
		t.Run(then, func(t *testing.T) {
			f, pidfile := treeFeather(t, then, 15*time.Second)
			o := f.Deliver(context.Background(), msg())
			pids := treePIDs(t, pidfile)
			t.Logf("outcome %+v, descendants %v", o, pids)
			waitGone(t, then, pids)
		})
	}
	t.Run("cancel", func(t *testing.T) {
		f, pidfile := treeFeather(t, "sleep", time.Minute)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			for i := 0; i < 1000; i++ {
				if _, err := os.Stat(pidfile + ".1"); err == nil {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
		}()
		start := time.Now()
		f.Deliver(ctx, msg())
		if d := time.Since(start); d > 30*time.Second {
			t.Fatalf("the cancel took %s", d)
		}
		waitGone(t, "cancel", treePIDs(t, pidfile))
	})
}

// Plugins run at the same time are contained each on its own: the end of
// one does not kill another's tree, and every tree is killed at its end.
func TestCritical_ConcurrentPluginTrees(t *testing.T) {
	long, longPID := treeFeather(t, "sleep", time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	longDone := make(chan struct{})
	go func() { long.Deliver(ctx, msg()); close(longDone) }()

	var wg sync.WaitGroup
	short := make([]string, 4)
	for i := range short {
		f, pidfile := treeFeather(t, []string{"exit", "crash"}[i%2], 30*time.Second)
		short[i] = pidfile
		wg.Add(1)
		go func() { defer wg.Done(); f.Deliver(context.Background(), msg()) }()
	}
	wg.Wait()
	for i, p := range short {
		waitGone(t, fmt.Sprint("plugin ", i), treePIDs(t, p))
	}
	for i := 0; i < 1000; i++ {
		if _, err := os.Stat(longPID + ".1"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	pids := treePIDs(t, longPID)
	for _, p := range pids {
		if !processAlive(p) {
			t.Fatalf("the end of another plugin killed %d", p)
		}
	}
	cancel()
	<-longDone
	waitGone(t, "the long plugin", pids)
}
