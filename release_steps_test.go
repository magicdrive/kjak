//go:build !windows

// The publish and tap steps of the release workflow, run as they are written
// in release.yml against a fake gh (GitHub's state scripted) and a local tap.

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stepScript returns the `run: |` script of step name in job.
func stepScript(t *testing.T, job, name string) string {
	t.Helper()
	_, jobs := workflowJobs(t, ".github/workflows/release.yml")
	body, ok := jobs[job]
	if !ok {
		t.Fatalf("no job %s", job)
	}
	lines := strings.Split(body, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "- name: "+name {
			continue
		}
		for j := i + 1; j < len(lines) && !strings.HasPrefix(lines[j], "      - "); j++ {
			if strings.TrimSpace(lines[j]) != "run: |" {
				continue
			}
			var script []string
			for _, s := range lines[j+1:] {
				if s != "" && !strings.HasPrefix(s, "          ") {
					break
				}
				script = append(script, strings.TrimPrefix(s, "          "))
			}
			return strings.Join(script, "\n")
		}
		t.Fatalf("step %q of %s has no run script", name, job)
	}
	t.Fatalf("no step %q in %s", name, job)
	return ""
}

// fakeGH is the GitHub of one release, as gh shows it. Every call is logged.
// FAKE_IMMUTABLE_AFTER / FAKE_ATTESTED_AFTER: how many reads see the release
// not yet immutable / not yet attested (-1: never).
const fakeGH = `#!/bin/sh
s=$FAKE_STATE
echo "$*" >> "$s/calls"
bump() { n=$(cat "$s/$1" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$s/$1"; echo "$n"; }
draft=$(cat "$s/draft")
case "$1 $2" in
"api repos/magicdrive/kjak/releases/tags/v0.0.1")
	n=$(bump release-reads)
	imm=false
	if [ "$draft" = false ] && [ "$FAKE_IMMUTABLE_AFTER" -ge 0 ] && [ "$n" -gt "$FAKE_IMMUTABLE_AFTER" ]; then imm=true; fi
	case "$*" in
	*'"\(.draft) \(.immutable)"'*) echo "$draft $imm" ;;
	*.immutable*) echo "$imm" ;;
	*) echo "fake gh: unexpected jq: $*" >&2; exit 2 ;;
	esac
	exit 0 ;;
"api repos/magicdrive/kjak/git/ref/tags/v0.0.1")
	echo b402586a7595e1a50d0f68d7dad30de6e8f62ebd; exit 0 ;;
"api repos/magicdrive/kjak/attestations/sha1:b402586a7595e1a50d0f68d7dad30de6e8f62ebd?predicate_type=release")
	n=$(bump attestation-reads)
	if [ "$FAKE_ATTESTED_AFTER" -lt 0 ] || [ "$n" -le "$FAKE_ATTESTED_AFTER" ]; then echo "gh: Not Found (HTTP 404)" >&2; exit 1; fi
	echo 1; exit 0 ;;
"release view")
	echo "$draft"; exit 0 ;;
"release edit")
	echo false > "$s/draft"; echo "https://github.com/magicdrive/kjak/releases/tag/v0.0.1"; exit 0 ;;
"release verify")
	n=$(cat "$s/attestation-reads" 2>/dev/null || echo 0)
	if [ "$FAKE_ATTESTED_AFTER" -lt 0 ] || [ "$n" -le "$FAKE_ATTESTED_AFTER" ]; then
		bump attestation-reads >/dev/null
		echo "no attestations for tag v0.0.1 (sha1:b402586a7595e1a50d0f68d7dad30de6e8f62ebd)" >&2; exit 1
	fi
	exit "${FAKE_VERIFY_EXIT:-0}" ;;
"release verify-asset")
	[ "${4##*/}" != "${FAKE_BAD_ASSET:-}" ] || { echo "not in the release" >&2; exit 1; }
	exit 0 ;;
esac
echo "fake gh: unexpected call: $*" >&2
exit 2
`

type ghWorld struct {
	immutableAfter, attestedAfter int
	draft                         bool
	verifyExit                    string
	badAsset                      string
}

type stepRun struct {
	out   string
	err   error
	calls []string
}

func (r stepRun) count(prefix string) int {
	n := 0
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// runPublishStep runs step of the publish job in a directory holding the
// built files, with gh faked as w.
func runPublishStep(t *testing.T, step string, w ghWorld, env ...string) stepRun {
	t.Helper()
	dir, state, bin := t.TempDir(), t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}
	draft := "false"
	if w.draft {
		draft = "true"
	}
	if err := os.WriteFile(filepath.Join(state, "draft"), []byte(draft+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "built"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"kjak_0.0.1_darwin_arm64.tar.gz", "kjak_0.0.1_linux_amd64.tar.gz", "kjak_0.0.1_windows_amd64.zip", "checksums.txt"} {
		if err := os.WriteFile(filepath.Join(dir, "built", f), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// an unbounded wait fails instead of hanging the test
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-e", "-c", stepScript(t, "publish", step))
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_STATE="+state, "TAG=v0.0.1", "GITHUB_REPOSITORY=magicdrive/kjak",
		"FAKE_IMMUTABLE_AFTER="+itoa(w.immutableAfter), "FAKE_ATTESTED_AFTER="+itoa(w.attestedAfter),
		"FAKE_VERIFY_EXIT="+w.verifyExit, "FAKE_BAD_ASSET="+w.badAsset,
		"RELEASE_WAIT_PAUSE=0")
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	calls, _ := os.ReadFile(filepath.Join(state, "calls"))
	return stepRun{string(out), err, strings.Split(strings.TrimSpace(string(calls)), "\n")}
}

func itoa(n int) string {
	if n < 0 {
		return "-" + itoa(-n)
	}
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

const verifyStep = "The release is immutable and attested"

// GitHub makes a release immutable, and attests it, a moment after it is
// published (run 39766468249: immutable=false 0.4s after publishing; then
// "no attestations" 0.7s after). The check waits, bounded, then verifies.
func TestReleasePublish_WaitsForImmutableAndAttested(t *testing.T) {
	r := runPublishStep(t, verifyStep, ghWorld{immutableAfter: 2, attestedAfter: 3})
	if r.err != nil {
		t.Fatalf("%v\n%s", r.err, r.out)
	}
	if r.count("release verify ") != 1 || r.count("release verify-asset ") != 4 {
		t.Fatalf("verify %d, verify-asset %d: %v", r.count("release verify "), r.count("release verify-asset "), r.calls)
	}
}

// What never becomes true fails, after a bounded wait, with the reason.
func TestReleasePublish_FailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    ghWorld
		want string
	}{
		{"never immutable", ghWorld{immutableAfter: -1, attestedAfter: 0}, "not immutable"},
		{"still a draft", ghWorld{draft: true, immutableAfter: 0, attestedAfter: 0}, "still a draft"},
		{"never attested", ghWorld{immutableAfter: 0, attestedAfter: -1}, "no release attestation"},
		{"attestation invalid", ghWorld{immutableAfter: 0, attestedAfter: 0, verifyExit: "1"}, ""},
		{"an asset not in the release", ghWorld{immutableAfter: 0, attestedAfter: 0, badAsset: "kjak_0.0.1_windows_amd64.zip"}, "not in the release"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := runPublishStep(t, verifyStep, tc.w, "RELEASE_WAIT_TRIES=3")
			if r.err == nil {
				t.Fatalf("passed: %s", r.out)
			}
			if !strings.Contains(r.out, tc.want) {
				t.Fatalf("output lacks %q:\n%s", tc.want, r.out)
			}
			if n := r.count("api repos/magicdrive/kjak/releases/tags/") + r.count("api repos/magicdrive/kjak/attestations/"); n > 6 {
				t.Fatalf("%d reads: the wait is not bounded by RELEASE_WAIT_TRIES", n)
			}
			if tc.w.draft && r.count("api repos/magicdrive/kjak/releases/tags/") != 1 {
				t.Fatalf("a draft is waited for: %v", r.calls)
			}
			if tc.w.verifyExit != "" && r.count("release verify ") != 1 {
				t.Fatalf("a failed verification is retried: %v", r.calls)
			}
		})
	}
}

// The wait cannot be configured away: the tries and the pause are positive
// integers, nothing else.
func TestReleasePublish_WaitBounds(t *testing.T) {
	for _, env := range []string{"RELEASE_WAIT_TRIES=0", "RELEASE_WAIT_TRIES=x", "RELEASE_WAIT_PAUSE=-1"} {
		r := runPublishStep(t, verifyStep, ghWorld{}, env)
		if r.err == nil || r.count("release verify") != 0 {
			t.Errorf("%s: accepted (%v)", env, r.calls)
		}
	}
}

// Publishing happens once: a re-run of the job (the release is public
// already) does not edit the release again; it goes on to the verification.
func TestReleasePublish_PublishesOnce(t *testing.T) {
	r := runPublishStep(t, "Publish the draft", ghWorld{draft: true})
	if r.err != nil || r.count("release edit v0.0.1 --repo magicdrive/kjak --draft=false --latest") != 1 {
		t.Fatalf("draft: %v %v\n%s", r.err, r.calls, r.out)
	}
	r = runPublishStep(t, "Publish the draft", ghWorld{draft: false})
	if r.err != nil || r.count("release edit") != 0 {
		t.Fatalf("re-run: %v %v\n%s", r.err, r.calls, r.out)
	}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newTap is a tap repository (a bare origin and its checkout at tap/ in a
// job directory) holding files.
func newTap(t *testing.T, files ...string) (job, origin string) {
	t.Helper()
	job, origin = t.TempDir(), filepath.Join(t.TempDir(), "tap.git")
	git(t, job, "init", "-q", "--bare", origin)
	git(t, job, "clone", "-q", origin, "tap")
	for _, f := range files {
		p := filepath.Join(job, "tap", f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("class X < Formula\nend\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, filepath.Join(job, "tap"), "add", "-A")
	git(t, filepath.Join(job, "tap"), "commit", "-q", "--allow-empty", "-m", "init")
	git(t, filepath.Join(job, "tap"), "push", "-q", "origin", "HEAD:main")
	if err := os.MkdirAll(filepath.Join(job, "built"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(job, "built", "kjak.rb"), []byte("class Kjak < Formula\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return job, origin
}

const tapStep = "Publish the formula to the tap"

func runTapStep(t *testing.T, job string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", "-e", "-c", stepScript(t, "homebrew", tapStep))
	cmd.Dir = job
	cmd.Env = append(os.Environ(), "TAG=v0.0.1")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The formula goes where Homebrew looks in the tap (Tap#formula_dir: the
// first of Formula/, HomebrewFormula/ and the root that exists). A new
// Formula/ in a tap with its formulas at the root would hide all of them.
func TestReleaseTap_FormulaWhereHomebrewLooks(t *testing.T) {
	for _, tc := range []struct {
		files []string
		want  string
	}{
		{[]string{"README.md", "ark.rb", "enma.rb"}, "kjak.rb"},
		{[]string{"Formula/ark.rb"}, "Formula/kjak.rb"},
		{[]string{"HomebrewFormula/ark.rb"}, "HomebrewFormula/kjak.rb"},
	} {
		job, origin := newTap(t, tc.files...)
		if out, err := runTapStep(t, job); err != nil {
			t.Fatalf("%v: %v\n%s", tc.files, err, out)
		}
		tree := git(t, job, "--git-dir", origin, "ls-tree", "-r", "--name-only", "main")
		if !strings.Contains("\n"+tree+"\n", "\n"+tc.want+"\n") {
			t.Fatalf("%v: the tap holds\n%s\nwant %s", tc.files, tree, tc.want)
		}
		for _, d := range []string{"Formula/", "HomebrewFormula/"} {
			if !strings.HasPrefix(tc.want, d) && strings.Contains("\n"+tree, "\n"+d) {
				t.Fatalf("%v: a new %s:\n%s", tc.files, d, tree)
			}
		}
		// a re-run with the same formula changes nothing
		before := git(t, job, "--git-dir", origin, "rev-parse", "main")
		if out, err := runTapStep(t, job); err != nil || !strings.Contains(out, "up to date") {
			t.Fatalf("re-run: %v\n%s", err, out)
		}
		if after := git(t, job, "--git-dir", origin, "rev-parse", "main"); after != before {
			t.Fatal("a re-run committed again")
		}
	}
}

// A kjak.rb somewhere Homebrew does not look is a conflict to resolve by
// hand, not a second copy to add.
func TestReleaseTap_RefusesAStrayFormula(t *testing.T) {
	job, origin := newTap(t, "Formula/ark.rb", "kjak.rb")
	before := git(t, job, "--git-dir", origin, "rev-parse", "main")
	out, err := runTapStep(t, job)
	if err == nil || !strings.Contains(out, "kjak.rb") {
		t.Fatalf("accepted: %v\n%s", err, out)
	}
	if git(t, job, "--git-dir", origin, "rev-parse", "main") != before {
		t.Fatal("pushed")
	}
}

// fakeSettingsGH answers the two settings make publish-check reads.
const fakeSettingsGH = `#!/bin/sh
case "$*" in
"api repos/magicdrive/kjak/immutable-releases --jq .enabled")
	[ "$FAKE_GH_DOWN" != 1 ] || { echo "gh: HTTP 401" >&2; exit 1; }
	echo "$FAKE_IMMUTABLE"; exit 0 ;;
"secret list --repo magicdrive/kjak --json name --jq .[].name")
	[ "$FAKE_GH_DOWN" != 1 ] || { echo "gh: HTTP 401" >&2; exit 1; }
	printf '%s' "$FAKE_SECRETS"; exit 0 ;;
esac
echo "fake gh: unexpected call: $*" >&2
exit 2
`

// make publish-check refuses, before any tag exists, a release the
// workflow could only find broken after publishing it: immutable releases
// off (v0.0.1, attempt 1), or no tap token for a stable release.
func TestPublishCheck_RepositorySettings(t *testing.T) {
	makefile, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	job, _ := newTap(t) // any repository with an origin will do
	work := filepath.Join(job, "tap")
	if err := os.WriteFile(filepath.Join(work, "Makefile"), makefile, 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, work, "add", "Makefile")
	git(t, work, "commit", "-q", "-m", "makefile")
	git(t, work, "push", "-q", "origin", "HEAD:main")
	git(t, work, "switch", "-q", "-C", "main", "--track", "origin/main")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeSettingsGH), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tag, immutable, secrets, down string
		ok                                  bool
		want                                string
	}{
		{"stable, all set", "v0.0.2", "true", "OTHER\nTAP_GITHUB_TOKEN\n", "", true, "OK"},
		{"pre-release needs no tap token", "v0.0.2-rc.1", "true", "", "", true, "OK"},
		{"immutable releases off", "v0.0.2-rc.1", "false", "TAP_GITHUB_TOKEN\n", "", false, "immutable releases are not enabled"},
		{"no tap token", "v0.0.2", "true", "OTHER\n", "", false, "TAP_GITHUB_TOKEN is not set"},
		{"a token named like it", "v0.0.2", "true", "TAP_GITHUB_TOKEN_OLD\n", "", false, "TAP_GITHUB_TOKEN is not set"},
		{"gh fails", "v0.0.2", "true", "TAP_GITHUB_TOKEN\n", "1", false, "immutable releases are not enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("make", "-s", "publish-check", "tag="+tc.tag)
			cmd.Dir = work
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FAKE_IMMUTABLE="+tc.immutable, "FAKE_SECRETS="+tc.secrets, "FAKE_GH_DOWN="+tc.down)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok || !strings.Contains(string(out), tc.want) {
				t.Fatalf("err %v, want ok=%v and %q:\n%s", err, tc.ok, tc.want, out)
			}
		})
	}
}
