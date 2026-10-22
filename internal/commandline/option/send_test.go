package option

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseSendInputs(t *testing.T) {
	cases := []struct {
		args []string
		mode InputMode
	}{
		{[]string{"hello", "world"}, InputArgs},
		{[]string{"--stdin"}, InputStdin},
		{[]string{"--json-input", "-"}, InputJSON},
		{[]string{"--json-input", "m.json"}, InputJSON},
		{[]string{"--template", "deploy", "--var", "a=1"}, InputTemplate},
		{[]string{"--template-file", "t.toml"}, InputTemplateFile},
	}
	for _, tc := range cases {
		opt, err := ParseSend(tc.args)
		if err != nil || opt.Mode != tc.mode {
			t.Errorf("%v: mode %v, %v", tc.args, opt, err)
		}
	}
}

func TestCritical_SendInputsExcludeEachOther(t *testing.T) {
	inputs := map[string][]string{
		"args":          {"body"},
		"stdin":         {"--stdin"},
		"json-input":    {"--json-input", "-"},
		"template":      {"--template", "t"},
		"template-file": {"--template-file", "f"},
	}
	for a, aa := range inputs {
		for b, bb := range inputs {
			if a >= b {
				continue
			}
			args := append(append([]string{}, aa...), bb...)
			_, err := ParseSend(args)
			var ue *UsageError
			if !errors.As(err, &ue) || !strings.Contains(err.Error(), "exclude each other") {
				t.Errorf("%s + %s: %v", a, b, err)
			}
		}
	}
	if _, err := ParseSend(nil); err == nil || !strings.Contains(err.Error(), "no message") {
		t.Fatalf("no input: %v", err)
	}
}

func TestParseSendFlagsAnywhere(t *testing.T) {
	opt, err := ParseSend([]string{"build", "--to", "a,b", "done", "-t", "c", "--level", "error", "-T", "CI", "--", "--not-a-flag"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Body != "build done --not-a-flag" || strings.Join(opt.Targets, " ") != "a b c" {
		t.Fatalf("opt = %+v", opt)
	}
	if !opt.HasLevel || opt.Level != "error" || !opt.HasTitle || opt.Title != "CI" || opt.HasURL {
		t.Fatalf("opt = %+v", opt)
	}
}

func TestParseSendRejects(t *testing.T) {
	bad := [][]string{
		{"body", "--var", "a=1"},
		{"--json-input", "-", "--title", "x"},
		{"--json-input", "-", "--level", "error"},
		{"--json-input", ""},
		{"--template", ""},
		{"body", "--timeout", "0s"},
		{"body", "--max-retries", "-1"},
		{"body", "--concurrency", "0"},
		{"body", "--output", "yaml"},
		{"body", "--bogus"},
		{"body", "--timeout", "soon"},
	}
	for _, args := range bad {
		if _, err := ParseSend(args); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestParseSendHelpAndDelivery(t *testing.T) {
	if _, err := ParseSend([]string{"--help"}); !errors.Is(err, ErrHelp) {
		t.Fatalf("help: %v", err)
	}
	opt, err := ParseSend([]string{"x", "--timeout", "3s", "--max-retries", "0", "--concurrency", "2", "--dry-run", "-o", "json", "-q", "-c", "/c.toml", "--no-project"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.Timeout != 3*time.Second || opt.MaxRetries != 0 || opt.Concurrency != 2 || !opt.DryRun || opt.Format != "json" || !opt.Quiet || opt.ConfigPath != "/c.toml" || !opt.NoProject {
		t.Fatalf("opt = %+v", opt)
	}
	opt, _ = ParseSend([]string{"x"})
	if opt.MaxRetries != -1 || opt.Timeout != 0 || opt.Concurrency != 0 {
		t.Fatalf("unset delivery options = %+v", opt)
	}
}

func TestParseManage(t *testing.T) {
	if _, err := ParseFeatherAdd([]string{"x", "--type", "slack", "--secret-env", "A", "--secret-stdin"}); err == nil {
		t.Fatal("two secret sources accepted")
	}
	if _, err := ParseFeatherAdd([]string{"x", "--type", "slack", "--secret-keychain", "nosplit"}); err == nil {
		t.Fatal("a keychain reference without account accepted")
	}
	if _, err := ParseFeatherAdd([]string{"x", "--secret-env", "A"}); err == nil {
		t.Fatal("no type accepted")
	}
	opt, err := ParseFeatherAdd([]string{"mail", "--type", "email", "--mail-to", "a@x.example,b@x.example", "--mail-to", "c@x.example", "--levels", "error", "--scope", "project"})
	if err != nil || len(opt.MailTo) != 3 || opt.Scope != "project" || opt.Levels[0] != "error" {
		t.Fatalf("%+v %v", opt, err)
	}
	if _, err := ParseScopedName("feather remove", []string{"x", "--scope", "everywhere"}); err == nil {
		t.Fatal("bad scope accepted")
	}
	if _, err := ParseScopedName("feather remove", []string{}); err == nil {
		t.Fatal("missing name accepted")
	}
	g, err := ParseGroupSet([]string{"dev", "a,b", "c"})
	if err != nil || strings.Join(g.Feathers, " ") != "a b c" {
		t.Fatalf("%+v %v", g, err)
	}
	if _, err := ParseGroupSet([]string{"dev"}); err == nil {
		t.Fatal("empty group accepted")
	}
	if _, err := ParseTemplateSet([]string{"t"}); err == nil {
		t.Fatal("template without body accepted")
	}
	if _, err := ParseTemplateSet([]string{"t", "--body", "x", "--body-stdin"}); err == nil {
		t.Fatal("two bodies accepted")
	}
	r, err := ParseTemplateRender([]string{"t", "--var", "a=1", "--var", "b=2"})
	if err != nil || len(r.Vars) != 2 {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := ParseList("feather list", []string{"extra"}); err == nil {
		t.Fatal("extra argument accepted")
	}
	tr, err := ParseConfigTrust("config trust", []string{"-y"})
	if err != nil || !tr.Yes {
		t.Fatalf("%+v %v", tr, err)
	}
}
