//go:build !windows

package e2e

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// SIGINT while a group is being delivered and its plugin (which started a
// child of its own) has not answered: kjak exits 130 with the report of
// what happened — the http member delivered, the plugin's delivery
// canceled — prints no secret, and leaves no process of the plugin behind
// (its process group is killed).
func TestBinarySIGINTCancelsPluginGroup(t *testing.T) {
	ok := newE2EAPI(t, false, 200)
	s := newSandbox(t)
	s.installPlugin("pager")
	pidfile := filepath.Join(t.TempDir(), "child.pid")
	s.writeConfig(httpFeatherTOML("api_ok", ok.URL, "")+fmt.Sprintf(`[feathers.pager_fork]
type = "external"
plugin = "pager"
settings = { mode = "fork", pidfile = %q }
secrets = { routing_key = { secret = "pd.key" } }

[groups.g]
feathers = ["api_ok", "pager_fork"]
`, pidfile), e2eSecrets)

	cmd := exec.Command(binary, "send", "--to", "g", "--timeout", "60s", "-o", "json", "x")
	cmd.Dir, cmd.Env = s.dir, s.env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var child int
	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(pidfile); err == nil && len(b) > 0 {
			if child, err = strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				break
			}
		}
		if time.Since(start) > 30*time.Second {
			_ = cmd.Process.Kill()
			t.Fatal("the plugin never started its child")
		}
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 130 {
		t.Fatalf("exit = %v\n%s%s", err, out.String(), errOut.String())
	}
	r := run{130, out.String(), errOut.String()}
	noSecret(t, "SIGINT", r)
	rep := parseReport(t, r.stdout)
	if !rep.Canceled || rep.Outcome != "canceled" || rep.result(t, "api_ok").Status != "delivered" {
		t.Fatalf("report: %s", r.stdout)
	}
	if x := rep.result(t, "pager_fork"); x.Status != "failed" || x.Error == nil || x.Error.Code != "canceled" {
		t.Fatalf("pager_fork: %+v", x)
	}
	for start := time.Now(); syscall.Kill(child, 0) == nil; time.Sleep(50 * time.Millisecond) {
		if time.Since(start) > 10*time.Second {
			_ = syscall.Kill(child, syscall.SIGKILL)
			t.Fatalf("the plugin's child %d outlived the send", child)
		}
	}
}
