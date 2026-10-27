package main

// Exit status tests of the quality gates: a failing tool (gofmt, go test
// -list, scripts/mutation.sh) fails the make target or the workflow step that
// runs it, instead of disappearing in a command substitution or a pipe.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// stubPath writes the executable stubs (name -> /bin/sh script) into a new
// directory and returns a PATH that finds them first.
func stubPath(t *testing.T, stubs map[string]string) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	for name, script := range stubs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir, dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// makeEnv is the environment without the variables of a parent make.
func makeEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains([]string{"MAKEFLAGS", "MFLAGS", "MAKELEVEL", "PATH"}, k) {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

func needMake(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Makefile targets macOS and Linux")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}
}

// TestMakefile_FmtCheckFailsWhenGofmtFails: gofmt -l prints nothing on
// stdout when it fails (a file it cannot parse, or no gofmt at all); the
// check must fail then, not read the empty output as "formatted".
func TestMakefile_FmtCheckFailsWhenGofmtFails(t *testing.T) {
	needMake(t)
	for _, tc := range []struct {
		name, gofmt, git string
		ok               bool
	}{
		{"formatted", "exit 0", "", true},
		{"unformatted", `echo main.go`, "", false},
		{"gofmt fails", `echo "broken.go:1:1: expected 'package', found 'EOF'" >&2; exit 2`, "", false},
		{"git ls-files fails", "exit 0", `echo "fatal: not a git repository" >&2; exit 128`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubs := map[string]string{"gofmt": tc.gofmt}
			if tc.git != "" {
				stubs["git"] = tc.git
			}
			_, path := stubPath(t, stubs)
			cmd := exec.Command("make", "--no-print-directory", "fmt-check")
			cmd.Env = makeEnv("PATH=" + path)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Errorf("make fmt-check: err %v, want ok %t\n%s", err, tc.ok, out)
			}
		})
	}
}

// TestMakefile_CheckRunsFmtCheck: make check, the local gate, formats through
// fmt-check (tested above) and has no gofmt check of its own.
func TestMakefile_CheckRunsFmtCheck(t *testing.T) {
	src, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^check:(.*)\n((?:\t.*\n)*)`).FindSubmatch(src)
	if m == nil {
		t.Fatal("no check target")
	}
	if !slices.Contains(strings.Fields(string(m[1])), "fmt-check") {
		t.Errorf("check does not depend on fmt-check: check:%s", m[1])
	}
	if strings.Contains(string(m[2]), "gofmt") {
		t.Errorf("check runs gofmt itself:\n%s", m[2])
	}
}

// TestMakefile_FuzzStopsWhenListingFails: a package whose fuzz targets
// cannot be listed (its tests do not build) fails make fuzz instead of being
// skipped.
func TestMakefile_FuzzStopsWhenListingFails(t *testing.T) {
	needMake(t)
	for _, tc := range []struct {
		fail   string
		ok     bool
		fuzzed bool
	}{
		{"", true, true},
		{"list", false, false},
		{"test-list", false, false},
	} {
		t.Run("fail="+tc.fail, func(t *testing.T) {
			dir, path := stubPath(t, map[string]string{"go": `
echo "$*" >> "$STUB_LOG"
case "$1 $2" in
"list ./...") [ "$STUB_FAIL" = list ] && { echo "go: broken module" >&2; exit 1; }; echo example.com/p example.com/q | tr ' ' '\n' ;;
"test -list") [ "$STUB_FAIL" = test-list ] && [ "$3" = '^Fuzz' ] && [ "$4" = example.com/p ] && { echo "p_test.go:1: syntax error" >&2; exit 1; }
  echo FuzzParse; echo "ok  $4 0.1s" ;;
esac
`})
			log := filepath.Join(dir, "calls")
			cmd := exec.Command("make", "--no-print-directory", "fuzz", "FUZZTIME=1s")
			cmd.Env = makeEnv("PATH="+path, "STUB_LOG="+log, "STUB_FAIL="+tc.fail)
			out, err := cmd.CombinedOutput()
			calls, _ := os.ReadFile(log)
			fuzzed := strings.Contains(string(calls), "-fuzz ^FuzzParse$")
			if (err == nil) != tc.ok || fuzzed != tc.fuzzed {
				t.Errorf("err %v (want ok %t), fuzzed %t (want %t)\ncalls:\n%s\noutput:\n%s", err, tc.ok, fuzzed, tc.fuzzed, calls, out)
			}
		})
	}
}

// TestMutationWorkflow_FailsWhenAMutantSurvives runs the mutation step of
// mutation.yml as the runner does (the step's shell: `bash -e {0}` when none
// is set, `bash --noprofile --norc -eo pipefail {0}` for shell: bash) with
// scripts/mutation.sh failing: the step must fail although its output goes
// through tee.
func TestMutationWorkflow_FailsWhenAMutantSurvives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash step")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	src, err := os.ReadFile(".github/workflows/mutation.yml")
	if err != nil {
		t.Fatal(err)
	}
	// The step that runs the script: its one-line run and its shell (no YAML
	// library in this module; the steps are "      - " items).
	var shell, run string
	for _, step := range strings.Split(string(src), "\n      - ") {
		r := regexp.MustCompile(`(?m)^\s*run: (.*scripts/mutation\.sh.*)$`).FindStringSubmatch(step)
		if r == nil {
			continue
		}
		run = r[1]
		if s := regexp.MustCompile(`(?m)^\s*shell: (\S+)\s*$`).FindStringSubmatch(step); s != nil {
			shell = s[1]
		}
	}
	if run == "" {
		t.Fatal("no one-line step runs scripts/mutation.sh")
	}
	var args []string
	switch shell {
	case "":
		args = []string{"-e"}
	case "bash":
		args = []string{"--noprofile", "--norc", "-eo", "pipefail"}
	default:
		t.Fatalf("shell %q: this test models bash only", shell)
	}
	work := t.TempDir()
	script := filepath.Join(work, "step.sh")
	if err := os.WriteFile(script, []byte(run), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, mutation string
		ok             bool
	}{
		{"every mutant killed", `echo "mutation: 1 mutant(s): 1 killed"; exit 0`, true},
		{"a mutant survived", `echo "SURVIVED m1"; exit 1`, false},
		{"a mutant is invalid", `echo "INVALID m1"; exit 2`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The step runs `sh scripts/mutation.sh ...`: the stub sh stands
			// in for the script.
			_, path := stubPath(t, map[string]string{"sh": tc.mutation})
			cmd := exec.Command("bash", append(args, script)...)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "PATH="+path, "MUTANTS=")
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Errorf("step (shell %q): err %v, want ok %t\n%s", shell, err, tc.ok, out)
			}
			if got, _ := os.ReadFile(filepath.Join(work, "mutation.txt")); len(got) == 0 {
				t.Error("mutation.txt lacks the script's output")
			}
		})
	}
}
