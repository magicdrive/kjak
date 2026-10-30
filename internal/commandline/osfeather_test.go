package commandline_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/magicdrive/kjak/internal/commandline"
	"github.com/magicdrive/kjak/internal/feather/osnotify"
)

// osRecorder stands for the OS backend: it records what it was asked to
// show and answers with err (by title).
type osRecorder struct {
	mu    sync.Mutex
	shown []osnotify.Notification
	err   map[string]error
	block bool
}

func (r *osRecorder) Notify(ctx context.Context, n osnotify.Notification) error {
	r.mu.Lock()
	r.shown = append(r.shown, n)
	err := r.err[n.Body]
	r.mu.Unlock()
	if r.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (r *osRecorder) with(rt *commandline.Runtime) { rt.OSNotify = r }

const osConfig = `
[feathers.desk]
type = "os"

[feathers.desk_errors]
type = "os"
levels = ["error", "critical"]

[groups.desktops]
feathers = ["desk", "desk_errors"]
`

func TestOSFeatherSend(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(osConfig)
	rec := &osRecorder{}
	r := e.runWith(rec.with, "send", "--to", "desktops", "--title", "Build done", "--level", "success", "-o", "json", "All tests passed")
	if r.code != 0 {
		t.Fatalf("%+v", r)
	}
	rep := parseReport(t, r.stdout)
	if rep.Outcome != "success" || rep.Summary.Delivered != 1 || rep.Summary.Skipped != 1 {
		t.Fatalf("report: %s", r.stdout)
	}
	res := rep.Results[0]
	if res.Feather != "desk" || res.Type != "os" || res.Status != "delivered" || res.Acceptance != "confirmed" || res.Attempts != 1 || res.History[0].StatusCode != 0 {
		t.Fatalf("result: %+v", res)
	}
	if strings.Contains(r.stdout, `"status_code"`) {
		t.Fatalf("an os feather has no status code: %s", r.stdout)
	}
	if rep.Results[1].Skipped == nil || rep.Results[1].Skipped.Reason != "level_filter" {
		t.Fatalf("level filter: %+v", rep.Results[1])
	}
	if len(rec.shown) != 1 || rec.shown[0].Title != "Build done" || rec.shown[0].Subtitle != "success" || rec.shown[0].Body != "All tests passed" {
		t.Fatalf("shown: %+v", rec.shown)
	}
}

func TestOSFeatherDryRun(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(osConfig)
	rec := &osRecorder{}
	r := e.runWith(rec.with, "send", "--to", "desk", "--dry-run", "-o", "json", "x")
	if r.code != 0 || !strings.Contains(r.stdout, `"status": "planned"`) || len(rec.shown) != 0 {
		t.Fatalf("%+v %+v", r, rec.shown)
	}
}

// A machine that cannot show a notification fails that feather only, with a
// code that says why; the others are delivered (exit 1, partial).
func TestOSFeatherUnavailableIsOneFailure(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(osConfig + "\n[feathers.desk2]\ntype = \"os\"\n")
	for _, c := range []struct {
		err  error
		code string
	}{
		{fmt.Errorf("%w: no GUI session", osnotify.ErrUnavailable), "unavailable"},
		{fmt.Errorf("%w (plan9)", osnotify.ErrUnsupported), "unsupported"},
		{fmt.Errorf("%w for Windows PowerShell", osnotify.ErrDisabled), "rejected"},
	} {
		rec := &osRecorder{err: map[string]error{"x": c.err}}
		r := e.runWith(rec.with, "send", "--to", "desk", "-o", "json", "x")
		if r.code != 1 {
			t.Fatalf("%s: exit %d %s", c.code, r.code, r.stdout)
		}
		res := parseReport(t, r.stdout).Results[0]
		if res.Status != "failed" || res.Acceptance != "rejected" || res.Error == nil || res.Error.Code != c.code || res.Attempts != 1 {
			t.Fatalf("%s: %+v", c.code, res)
		}
	}
	rec := &osRecorder{err: map[string]error{"y": fmt.Errorf("%w: no GUI session", osnotify.ErrUnavailable)}}
	r := e.runWith(func(rt *commandline.Runtime) { rt.OSNotify = partly{rec, "desk"} }, "send", "--to", "desk,desk2", "-o", "json", "y")
	rep := parseReport(t, r.stdout)
	if r.code != 1 || rep.Outcome != "partial" || rep.Summary.Delivered != 1 || rep.Summary.Failed != 1 {
		t.Fatalf("partial: %d %s", r.code, r.stdout)
	}
}

// partly fails only the first notification (the first target).
type partly struct {
	rec  *osRecorder
	fail string
}

func (p partly) Notify(ctx context.Context, n osnotify.Notification) error {
	p.rec.mu.Lock()
	first := len(p.rec.shown) == 0
	p.rec.mu.Unlock()
	err := p.rec.Notify(ctx, n)
	if first {
		return err
	}
	return nil
}

// Ctrl-C while the OS program runs: the delivery is unknown (it may have
// been shown), the send is canceled (exit 130).
func TestOSFeatherCanceled(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal(osConfig)
	rec := &osRecorder{block: true}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			rec.mu.Lock()
			n := len(rec.shown)
			rec.mu.Unlock()
			if n > 0 {
				cancel()
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	r := e.runFull(ctx, rec.with, "send", "--to", "desk", "-o", "json", "x")
	if r.code != 130 {
		t.Fatalf("%+v", r)
	}
	var rep struct {
		Canceled bool `json:"canceled"`
		Results  []struct {
			Acceptance string `json:"acceptance"`
			Error      struct {
				Code string `json:"code"`
			} `json:"error"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &rep); err != nil || !rep.Canceled || rep.Results[0].Acceptance != "unknown" || rep.Results[0].Error.Code != "canceled" {
		t.Fatalf("%v %s", err, r.stdout)
	}
}

func TestOSFeatherManagement(t *testing.T) {
	e := newEnv(t)
	e.must("feather", "add", "desk", "--type", "os", "--levels", "warning,error")
	r := e.must("feather", "show", "desk", "-o", "json")
	var v map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil || v["type"] != "os" || v["name"] != "desk" {
		t.Fatalf("%v %s", err, r.stdout)
	}
	if r := e.must("feather", "list"); !strings.Contains(r.stdout, "desk") || !strings.Contains(r.stdout, "os") {
		t.Fatal(r.stdout)
	}
	for _, args := range [][]string{
		{"feather", "add", "d2", "--type", "os", "--host", "smtp.example.com"},
		{"feather", "add", "d3", "--type", "os", "--username", "bot"},
		{"feather", "add", "d4", "--type", "os", "--secret-ref", "x.y"},
	} {
		if r := e.run(args...); r.code != 2 {
			t.Errorf("%v: %+v", args, r)
		}
	}
	e.writeGlobal("[feathers.d]\ntype = \"os\"\nallow_hosts = [\"x\"]\n")
	if r := e.run("config", "validate"); r.code != 2 || !strings.Contains(r.stderr, "no allow_networks") {
		t.Fatalf("%+v", r)
	}
}
