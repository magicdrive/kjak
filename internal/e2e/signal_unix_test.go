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
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// SIGINT or SIGTERM (a CI job canceled, a service stopped) during a
// delivery: kjak stops, reports and exits 130. (Windows has no signal to
// send to another process; os.Interrupt is not deliverable there.)
func TestBinarySIGINTExits130(t *testing.T) {
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) { testSignalExits130(t, sig) })
	}
}

func testSignalExits130(t *testing.T, sig os.Signal) {
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
	if err := cmd.Process.Signal(sig); err != nil {
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

// A reader that goes away (kjak send ... | head -c0) makes the report
// unwritable, not the delivery unreported: kjak is not killed by SIGPIPE,
// its exit status is still the delivery's.
func TestBinaryClosedStdoutKeepsExitStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	s := newSandbox(t)
	s.env = append(s.env, "HOOK="+srv.URL)
	s.must("feather", "add", "f", "--type", "slack", "--secret-env", "HOOK")

	for _, tc := range []struct {
		args []string
		want int
	}{
		{[]string{"send", "--to", "f", "x"}, 0},
		{[]string{"send", "--to", "nobody", "x"}, 2},
	} {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		r.Close()
		cmd := exec.Command(binary, tc.args...)
		cmd.Dir, cmd.Env, cmd.Stdout = s.dir, s.env, w
		var errOut bytes.Buffer
		cmd.Stderr = &errOut
		err = cmd.Run()
		w.Close()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != tc.want {
			t.Fatalf("%v: exit %d (%v), want %d; stderr %q", tc.args, code, err, tc.want, errOut.String())
		}
	}
}

// SIGTERM while kjak reads a --json-input FIFO whose writer is silent: it
// ends (130), also when every descriptor it opens is 1024 or more (beyond
// what select(2) can wait for: it inherits 1100 more).
func TestBinarySIGTERMWhileReadingFIFO(t *testing.T) {
	t.Run("low descriptors", func(t *testing.T) { testSIGTERMWhileReadingFIFO(t, 0) })
	t.Run("high descriptors", func(t *testing.T) { testSIGTERMWhileReadingFIFO(t, 1100) })
}

func testSIGTERMWhileReadingFIFO(t *testing.T, extra int) {
	if extra > 0 {
		var rl syscall.Rlimit
		if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rl); err != nil || rl.Cur < uint64(extra)+256 {
			if os.Getenv("CI") != "" { // CI must run it
				t.Fatalf("RLIMIT_NOFILE is %d (%v): kjak cannot inherit %d descriptors", rl.Cur, err, extra)
			}
			t.Skipf("RLIMIT_NOFILE is %d (%v): kjak cannot inherit %d descriptors", rl.Cur, err, extra)
		}
	}
	s := newSandbox(t)
	s.env = append(s.env, "HOOK=http://127.0.0.1:9/never")
	s.must("feather", "add", "f", "--type", "slack", "--secret-env", "HOOK")
	fifo := filepath.Join(t.TempDir(), "in")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "send", "--to", "f", "--json-input", fifo)
	cmd.Dir, cmd.Env = s.dir, s.env
	if extra > 0 {
		null, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		defer null.Close()
		for range extra {
			cmd.ExtraFiles = append(cmd.ExtraFiles, null) // descriptors 3 to 3+extra-1
		}
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// kjak has the FIFO open for reading once a writer can open it; the
	// writer stays, silent (one that closed would be the end of the input)
	for start := time.Now(); ; {
		w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			defer w.Close()
			break
		}
		if time.Since(start) > 30*time.Second {
			_ = cmd.Process.Kill()
			t.Fatalf("kjak never opened the FIFO: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	err := cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 130 {
		t.Fatalf("exit = %v", err)
	}
}
