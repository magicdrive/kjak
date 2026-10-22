package commandline_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// errorDoc is the JSON document of a failed command (docs/cli.md "JSON
// output").
type errorDoc struct {
	SchemaVersion int    `json:"schema_version"`
	Outcome       string `json:"outcome"`
	Error         *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Summary *struct {
		Total int `json:"total"`
	} `json:"summary"`
	Results []json.RawMessage `json:"results"`
}

func parseErrorDoc(t *testing.T, r result) errorDoc {
	t.Helper()
	var d errorDoc
	dec := json.NewDecoder(strings.NewReader(r.stdout))
	if err := dec.Decode(&d); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\nstdout: %q\nstderr: %q", err, r.stdout, r.stderr)
	}
	if dec.More() {
		t.Fatalf("more than one document on stdout: %q", r.stdout)
	}
	return d
}

// With -o json, a send that fails before anything is sent prints a JSON
// error document on stdout (the diagnostic stays on stderr) and exits 2.
func TestJSONErrorContract(t *testing.T) {
	p := newProvider(t, 200, "ok")
	cases := []struct {
		name   string
		global string
		proj   string
		stdin  string
		vars   map[string]string
		args   []string
		code   string
	}{
		{name: "invalid config", global: "[feathers.a\n", args: []string{"send", "--to", "a", "x"}, code: "config"},
		{name: "untrusted project", global: featherConfig("a", "slack", p.URL), proj: "[templates.t]\nbody = \"x\"\n", args: []string{"send", "--to", "a", "x"}, code: "untrusted_project"},
		{name: "invalid feather", global: "[feathers.a]\ntype = \"os\"\n", args: []string{"send", "--to", "a", "x"}, code: "config"},
		{name: "unknown target", global: featherConfig("a", "slack", p.URL), args: []string{"send", "--to", "ghost", "x"}, code: "routing"},
		{name: "disabled direct feather", global: "[feathers.a]\ntype = \"slack\"\nenabled = false\nwebhook_url = { literal = \"" + p.URL + "\" }\n", args: []string{"send", "--to", "a", "x"}, code: "routing"},
		{name: "credential missing", global: "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"NOT_SET\" }\n", args: []string{"send", "--to", "a", "x"}, code: "credential"},
		{name: "credential not https", global: "[feathers.a]\ntype = \"slack\"\nwebhook_url = { env = \"HOOK\" }\n", vars: map[string]string{"HOOK": "http://hooks.example.com/services/T1/B1/SECRETX"}, args: []string{"send", "--to", "a", "x"}, code: "credential"},
		{name: "two inputs", global: featherConfig("a", "slack", p.URL), args: []string{"send", "--to", "a", "--stdin", "x"}, code: "usage"},
		{name: "no input", global: featherConfig("a", "slack", p.URL), args: []string{"send", "--to", "a"}, code: "usage"},
		{name: "invalid level", global: featherConfig("a", "slack", p.URL), args: []string{"send", "--to", "a", "--level", "loud", "x"}, code: "input"},
		{name: "empty stdin", global: featherConfig("a", "slack", p.URL), stdin: "\n", args: []string{"send", "--to", "a", "--stdin"}, code: "input"},
		{name: "bad json input", global: featherConfig("a", "slack", p.URL), stdin: `{"body":"x","body":"y"}`, args: []string{"send", "--to", "a", "--json-input", "-"}, code: "input"},
		{name: "missing template", global: featherConfig("a", "slack", p.URL), args: []string{"send", "--to", "a", "--template", "nope"}, code: "not_found"},
		{name: "bound", global: featherConfig("a", "slack", p.URL), args: []string{"send", "--to", "a", "--timeout", "1h", "x"}, code: "usage"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.writeGlobal(tc.global)
			if tc.proj != "" {
				e.writeProject(tc.proj)
			}
			for k, v := range tc.vars {
				e.vars[k] = v
			}
			e.stdin = tc.stdin
			r := e.run(append(tc.args, "-o", "json")...)
			if r.code != 2 {
				t.Fatalf("exit %d", r.code)
			}
			d := parseErrorDoc(t, r)
			if d.SchemaVersion != 1 || d.Outcome != "error" || d.Error == nil || d.Error.Code != tc.code || d.Error.Message == "" {
				t.Fatalf("doc = %s", r.stdout)
			}
			if d.Summary == nil || d.Summary.Total != 0 || d.Results == nil || len(d.Results) != 0 {
				t.Fatalf("an error document has the report fields, empty: %s", r.stdout)
			}
			if strings.TrimSpace(r.stderr) == "" {
				t.Fatal("no diagnostic on stderr")
			}
			for _, leak := range []string{"SECRETX", "T1/B1", p.URL} {
				if strings.Contains(r.stdout+r.stderr, leak) && tc.name != "untrusted project" {
					t.Fatalf("%q in the output: %s %s", leak, r.stdout, r.stderr)
				}
			}
		})
	}
	if p.count() != 0 {
		t.Fatal("a failed preflight sent something")
	}
}

// The other documents of a send carry "error": null (one contract for
// success, partial failure, failure and preflight failure).
func TestJSONReportHasNullError(t *testing.T) {
	ok := newProvider(t, 200, "ok")
	bad := newProvider(t, 400, "no")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", ok.URL) + featherConfig("b", "slack", bad.URL))
	for args, want := range map[string]string{"a": "success", "a,b": "partial", "b": "failure"} {
		r := e.run("send", "--to", args, "-o", "json", "x")
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
			t.Fatal(err)
		}
		if string(doc["error"]) != "null" || !strings.Contains(string(doc["outcome"]), want) {
			t.Errorf("--to %s: outcome %s error %s", args, doc["outcome"], doc["error"])
		}
	}
}

// A command line that cannot be parsed (an unknown flag, a value of the wrong
// type) is not a JSON request yet: the error is text on stderr, stdout empty.
func TestUnparsableCommandLineIsText(t *testing.T) {
	e := newEnv(t)
	for _, args := range [][]string{
		{"send", "-o", "json", "--bogus", "x"},
		{"send", "-o", "json", "--timeout", "soon", "x"},
		{"send", "-o", "yaml", "x"},
	} {
		r := e.run(args...)
		if r.code != 2 || r.stdout != "" || r.stderr == "" {
			t.Errorf("%v: exit %d stdout %q", args, r.code, r.stdout)
		}
	}
}

// The management commands that print JSON fail with a JSON document too.
func TestJSONErrorsOfManagementCommands(t *testing.T) {
	e := newEnv(t)
	e.writeGlobal("")
	for args, code := range map[string]string{
		"feather show ghost": "not_found",
		"group show ghost":   "not_found",
		"template render x":  "not_found",
		"feather show":       "usage",
	} {
		r := e.run(append(strings.Fields(args), "-o", "json")...)
		if r.code != 2 {
			t.Errorf("%s: exit %d", args, r.code)
			continue
		}
		d := parseErrorDoc(t, r)
		if d.SchemaVersion != 1 || d.Outcome != "error" || d.Error == nil || d.Error.Code != code {
			t.Errorf("%s: %s", args, r.stdout)
		}
	}
	e.writeProject("[templates.t]\nbody = \"x\"\n")
	if d := parseErrorDoc(t, e.run("feather", "list", "-o", "json")); d.Error == nil || d.Error.Code != "untrusted_project" {
		t.Error("feather list with an untrusted project")
	}
}

// SIGINT before any delivery: the error document says "interrupted", exit 130.
func TestJSONInterruptedBeforeDelivery(t *testing.T) {
	p := newProvider(t, 200, "ok")
	e := newEnv(t)
	e.writeGlobal(featherConfig("a", "slack", p.URL))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := e.runCtx(ctx, "send", "--to", "a", "-o", "json", "x")
	d := parseErrorDoc(t, r)
	if r.code != 130 || d.Error == nil || d.Error.Code != "interrupted" || p.count() != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stdout)
	}
}
