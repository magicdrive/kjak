//go:build !windows

package e2e

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// SIGINT during a delivery: kjak stops and exits 130. (Windows has no
// SIGINT to send to another process; os.Interrupt is not deliverable there.)
func TestBinarySIGINTExits130(t *testing.T) {
	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // then the server notices the client leave
		received <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	s := newSandbox(t)
	s.env = append(s.env, "HOOK="+srv.URL)
	s.must("feather", "add", "f", "--type", "slack", "--secret-env", "HOOK")

	cmd := exec.Command(binary, "send", "--to", "f", "--timeout", "60s", "-o", "json", "x")
	cmd.Dir, cmd.Env = s.dir, s.env
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the request never arrived")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 130 {
		t.Fatalf("exit = %v", err)
	}
	// the request had been sent: its acceptance is unknown, and reported
	if !strings.Contains(out.String(), `"canceled": true`) || !strings.Contains(out.String(), `"acceptance": "unknown"`) {
		t.Fatalf("report = %s", out.String())
	}
}
