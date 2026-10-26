package main

// Tests of the release wiring that `make publish` (hephaestus publish)
// relies on: the Makefile's publish targets, driven with the publish tool
// replaced by a stub, and the workflow conventions it verifies
// (https://github.com/magicdrive/hephaestus/blob/main/docs/publish.md).

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

// TestMakefile_Publish drives the publish targets with PUBLISH replaced by a
// stub that records how it was called: argument handling, make -n and
// make -j, without any git or GitHub access.
func TestMakefile_Publish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the Makefile targets macOS and Linux")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not installed")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "stub")
	script := "#!/bin/sh\nprintf '%s|%s|%s|%s\\n' \"$*\" \"$HEPHAESTUS_PUBLISH_ARGS\" \"$HEPHAESTUS_PUBLISH_VERSION\" \"$HEPHAESTUS_PUBLISH_TAG\" >> " + log + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	mk := func(env []string, args ...string) (string, []string, error) {
		os.Remove(log)
		cmd := exec.Command("make", append([]string{"--no-print-directory", "PUBLISH=" + stub}, args...)...)
		// A clean make environment: no MAKEFLAGS or jobserver from a parent
		// make, and no VERSION/YES/tag unless the case sets them.
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if !slices.Contains([]string{"MAKEFLAGS", "MFLAGS", "MAKELEVEL", "VERSION", "YES", "tag"}, k) && !strings.HasPrefix(k, "HEPHAESTUS_PUBLISH_") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(log)
		var lines []string
		for _, l := range strings.Split(strings.TrimSpace(string(calls)), "\n") {
			if l != "" {
				lines = append(lines, l)
			}
		}
		return string(out), lines, err
	}

	for _, tc := range []struct {
		name string
		env  []string
		args []string
		want string // the one stub call: "args|ARGS|VERSION|TAG"
	}{
		{"positional", nil, []string{"publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		{"VERSION=", nil, []string{"publish", "VERSION=v1.0.0"}, "publish -from-make||v1.0.0|"},
		{"tag= (as before)", nil, []string{"publish", "tag=v1.0.0"}, "publish -from-make|||v1.0.0"},
		{"both reach the tool, which rejects them", nil, []string{"publish", "v1.0.0", "VERSION=v1.0.0"}, "publish -from-make|v1.0.0|v1.0.0|"},
		{"no version reaches the tool, which rejects it", nil, []string{"publish"}, "publish -from-make|||"},
		{"YES=1 on the command line", nil, []string{"publish", "v1.0.0", "YES=1"}, "publish -from-make -yes|v1.0.0||"},
		{"YES, VERSION and tag from the environment are ignored", []string{"YES=1", "VERSION=v9.9.9", "tag=v9.9.9"}, []string{"publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		{"check", nil, []string{"publish-check", "v1.0.0"}, "check -from-make|v1.0.0||"},
		{"check tag=", nil, []string{"publish-check", "tag=v1.0.0"}, "check -from-make|||v1.0.0"},
		{"status", nil, []string{"publish-status", "v1.0.0"}, "status -from-make|v1.0.0||"},
		{"watch", nil, []string{"publish-watch", "v1.0.0"}, "watch -from-make|v1.0.0||"},
		{"help", nil, []string{"publish-help"}, "help|||"},
		{"prerelease", nil, []string{"publish", "v1.0.0-rc.1"}, "publish -from-make|v1.0.0-rc.1||"},
		{"parallel make runs the tool once", nil, []string{"-j4", "publish", "v1.0.0"}, "publish -from-make|v1.0.0||"},
		// Were the goal pasted into the recipe, the shell would run false.
		{"shell metacharacters stay data", nil, []string{"publish", "v1;false"}, "publish -from-make|v1;false||"},
		{"shell substitution stays data", nil, []string{"publish", "v1$(false)"}, "publish -from-make|v1$(false)||"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, calls, err := mk(tc.env, tc.args...)
			if err != nil {
				t.Fatalf("make %v: %v\n%s", tc.args, err, out)
			}
			if len(calls) != 1 || calls[0] != tc.want {
				t.Fatalf("make %v called the tool %q, want once %q\n%s", tc.args, calls, tc.want, out)
			}
		})
	}

	for _, args := range [][]string{
		{"publish", "v1.0.0", "v2.0.0"},
		{"build", "publish", "v1.0.0"},
		{"test", "publish", "v1.0.0"},
		{"publish", "publish-check", "v1.0.0"},
		{"publish-help", "v1.0.0"},
		{"publish", "clean"},
		{"publish", "vet"},
		{"publish", "latest"},
	} {
		out, calls, err := mk(nil, args...)
		if err == nil || len(calls) != 0 {
			t.Errorf("make %v: err %v, calls %q\n%s", args, err, calls, out)
		}
	}

	out, calls, err := mk(nil, "-n", "publish", "v1.0.0")
	if err != nil || len(calls) != 0 || !strings.Contains(out, "publish -from-make") {
		t.Errorf("make -n ran the tool (calls %q, err %v)\n%s", calls, err, out)
	}
}

// workflowJobs returns the top-level jobs of a workflow file and the text
// of each (a line-based reading: two-space job keys under `jobs:`).
func workflowJobs(t *testing.T, path string) (names []string, body map[string]string) {
	t.Helper()
	b := readText(t, path)
	body = map[string]string{}
	key := regexp.MustCompile(`^  ([A-Za-z0-9_-]+):\s*$`)
	in, cur := false, ""
	for _, l := range strings.Split(b, "\n") {
		if l == "jobs:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if l != "" && !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "#") {
			break
		}
		if m := key.FindStringSubmatch(l); m != nil {
			cur = m[1]
			names = append(names, cur)
		}
		if cur != "" {
			body[cur] += l + "\n"
		}
	}
	return names, body
}

// TestCIGateNeedsEveryJob: ci-gate, the check the main ruleset requires,
// runs always and needs every other job of ci.yml.
func TestCIGateNeedsEveryJob(t *testing.T) {
	names, body := workflowJobs(t, ".github/workflows/ci.yml")
	gate, ok := body["ci-gate"]
	if !ok {
		t.Fatal("ci.yml has no ci-gate job")
	}
	if !strings.Contains(gate, "    if: always()\n") {
		t.Error("ci-gate must run with if: always()")
	}
	m := regexp.MustCompile(`(?m)^    needs: \[([^\]]*)\]$`).FindStringSubmatch(gate)
	if m == nil {
		t.Fatal("ci-gate has no needs: [...] list")
	}
	var needs []string
	for _, n := range strings.Split(m[1], ",") {
		needs = append(needs, strings.TrimSpace(n))
	}
	for _, n := range names {
		if n != "ci-gate" && !slices.Contains(needs, n) {
			t.Errorf("ci-gate does not need job %q", n)
		}
	}
	if !strings.Contains(gate, "needs.*.result") {
		t.Error("ci-gate must check the result of every needed job")
	}
}

// TestReleaseWorkflow_TapStepsFollowThePlan: the release job plans the
// release first, the two tap steps have the names hephaestus publish
// verifies, and the tap token reaches only steps that run when the plan
// publishes the tap (never for a prerelease).
func TestReleaseWorkflow_TapStepsFollowThePlan(t *testing.T) {
	_, body := workflowJobs(t, ".github/workflows/release.yml")
	rel, ok := body["release"]
	if !ok {
		t.Fatal("release.yml has no release job")
	}
	if !strings.Contains(rel, "    environment: release\n") {
		t.Error("the release job must run in environment release")
	}
	steps := strings.Split(rel, "\n      - ")[1:]
	plan := -1
	var names []string
	for i, s := range steps {
		name := ""
		if m := regexp.MustCompile(`(?m)^name: (.+)$`).FindStringSubmatch(s); m != nil {
			name = m[1]
		}
		names = append(names, name)
		if strings.Contains(s, "\n        id: plan\n") {
			plan = i
			if !strings.Contains(s, "uses: magicdrive/hephaestus@") || !strings.Contains(s, "command: plan") {
				t.Errorf("the plan step must run the hephaestus action with command: plan:\n%s", s)
			}
		}
		if !strings.Contains(s, "HOMEBREW_TAP_GITHUB_TOKEN") {
			continue
		}
		if plan < 0 {
			t.Errorf("step %q uses the tap token before the plan step", name)
		}
		if !strings.Contains(s, "\n        if: steps.plan.outputs.tap == 'publish'\n") {
			t.Errorf("step %q uses the tap token without if: steps.plan.outputs.tap == 'publish'", name)
		}
	}
	if plan < 0 {
		t.Fatal("the release job has no plan step (id: plan)")
	}
	for _, n := range []string{
		"Rehearse the formula and tap pull request (read-only)",
		"Generate the formula and open a tap pull request",
	} {
		if !slices.Contains(names, n) {
			t.Errorf("the release job has no step %q (hephaestus publish verifies it by name)", n)
		}
	}
	// Every release asset is attested: the archives and the checksum file.
	for _, p := range []string{"dist/kjak_*.tar.gz", "dist/kjak_*.zip", "dist/kjak_checksums.txt"} {
		if !strings.Contains(rel, p) {
			t.Errorf("the attestation does not cover %s", p)
		}
	}
}

// TestHephaestusPinsAgree: every magicdrive/hephaestus action reference is
// the same full commit SHA with the version comment that HEPHAESTUS_VERSION
// in the Makefile names (Renovate moves them together).
func TestHephaestusPinsAgree(t *testing.T) {
	m := regexp.MustCompile(`(?m)^HEPHAESTUS_VERSION \?= (v\S+)$`).FindStringSubmatch(readText(t, "Makefile"))
	if m == nil {
		t.Fatal("Makefile has no HEPHAESTUS_VERSION ?= vX.Y.Z")
	}
	version := m[1]
	use := regexp.MustCompile(`uses: magicdrive/hephaestus@(\S+)(?: # (\S+))?`)
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	shas := map[string]bool{}
	for _, f := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		for _, u := range use.FindAllStringSubmatch(readText(t, f), -1) {
			if !sha.MatchString(u[1]) || u[2] != version {
				t.Errorf("%s: %s; want a full commit SHA # %s", f, u[0], version)
			}
			shas[u[1]] = true
		}
	}
	if len(shas) != 1 {
		t.Errorf("the workflows pin %d different hephaestus commits, want 1", len(shas))
	}
}

// readText reads a text file with LF line endings (a Windows checkout may
// have CRLF).
func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}
