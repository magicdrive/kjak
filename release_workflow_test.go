package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var jobHeader = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):\s*$`)

// workflowJobs splits a workflow file into its top part and its jobs' text.
func workflowJobs(t *testing.T, path string) (string, map[string]string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i := strings.Index(s, "\njobs:\n")
	if i < 0 {
		t.Fatalf("%s: no jobs", path)
	}
	top, body := s[:i], s[i+len("\njobs:\n"):]
	jobs := map[string]string{}
	locs := jobHeader.FindAllStringSubmatchIndex(body, -1)
	for n, loc := range locs {
		end := len(body)
		if n+1 < len(locs) {
			end = locs[n+1][0]
		}
		jobs[body[loc[2]:loc[3]]] = body[loc[1]:end]
	}
	return top, jobs
}

func names(m map[string]string) []string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}

// The order of the release (docs/release.md): every job needs the one
// before it, so a failure stops everything after it.
func TestReleaseWorkflow_Order(t *testing.T) {
	top, jobs := workflowJobs(t, ".github/workflows/release.yml")
	if !strings.Contains(top, "permissions:\n  contents: read") {
		t.Error("release.yml: the default permissions must be contents: read")
	}
	want := map[string]string{
		"ci":           "needs: tag",
		"build":        "needs: [tag, ci]",
		"draft":        "needs: [tag, build]",
		"verify":       "needs: [tag, draft]",
		"publish":      "needs: [tag, verify]",
		"homebrew":     "needs: [tag, publish]",
		"post-release": "needs: [tag, publish, homebrew]",
	}
	for job, needs := range want {
		if !strings.Contains(jobs[job], needs) {
			t.Errorf("release.yml: job %s must have %q", job, needs)
		}
	}
	if len(jobs) != len(want)+1 {
		t.Errorf("release.yml jobs: %v", names(jobs))
	}
	if !strings.Contains(jobs["ci"], "uses: ./.github/workflows/ci.yml") {
		t.Error("the ci job must run ci.yml")
	}
}

// Build once: only the build job builds; the others use its artifact.
func TestReleaseWorkflow_BuildOnce(t *testing.T) {
	_, jobs := workflowJobs(t, ".github/workflows/release.yml")
	for name, body := range jobs {
		builds := strings.Contains(body, "goreleaser release") || strings.Contains(body, "go build") || strings.Contains(body, "make build") || strings.Contains(body, "make release-build")
		if builds != (name == "build") {
			t.Errorf("job %s: builds = %t", name, builds)
		}
		if name != "build" && name != "tag" && name != "ci" && !strings.Contains(body, "name: release") && name != "post-release" {
			t.Errorf("job %s does not use the release artifact", name)
		}
		if strings.Contains(body, "id-token: write") != (name == "build") {
			t.Errorf("job %s: id-token: write only in build", name)
		}
	}
	for _, subject := range []string{"dist/*.tar.gz", "dist/*.zip", "dist/checksums.txt"} {
		if !strings.Contains(jobs["build"], subject) {
			t.Errorf("the attestation does not cover %s", subject)
		}
	}
}

// A pre-release (vX.Y.Z-rc.N) never updates the Homebrew formula.
func TestReleaseWorkflow_PreReleaseLeavesTheTap(t *testing.T) {
	_, jobs := workflowJobs(t, ".github/workflows/release.yml")
	if !strings.Contains(jobs["tag"], "prerelease=") || !strings.Contains(jobs["tag"], "prerelease: ${{ steps.check.outputs.prerelease }}") {
		t.Error("the tag job must output prerelease")
	}
	if !strings.Contains(jobs["homebrew"], "if: needs.tag.outputs.prerelease == 'false'") {
		t.Error("the homebrew job must not run for a pre-release")
	}
	if !strings.Contains(jobs["post-release"], "needs.tag.outputs.prerelease == 'false'") {
		t.Error("post-release must not install a pre-release from the tap")
	}
}

// A draft left by an earlier run (drafts are not bound to the tag, and are
// invisible to the read-only token of the tag job) stops the release.
func TestReleaseWorkflow_NoStaleDraft(t *testing.T) {
	_, jobs := workflowJobs(t, ".github/workflows/release.yml")
	if !strings.Contains(jobs["draft"], "/releases?per_page=100") || !strings.Contains(jobs["draft"], "select(.tag_name == env.TAG)") {
		t.Error("the draft job must refuse an existing release or draft of the tag")
	}
}

// The CI Gate runs scripts/ci-gate.sh over every required job.
func TestCIWorkflow_Gate(t *testing.T) {
	_, jobs := workflowJobs(t, ".github/workflows/ci.yml")
	gate := jobs["ci-gate"]
	if !strings.Contains(gate, "if: always()") || !strings.Contains(gate, "sh scripts/ci-gate.sh") {
		t.Fatal("ci-gate must run always and use scripts/ci-gate.sh")
	}
	for name := range jobs {
		if name != "ci-gate" && !strings.Contains(gate, "      - "+name+"\n") {
			t.Errorf("ci-gate does not need %s", name)
		}
	}
	if !strings.Contains(gate, fmt.Sprintf("EXPECTED_JOBS: %d\n", len(jobs)-1)) {
		t.Errorf("ci-gate: EXPECTED_JOBS must be %d (every other job)", len(jobs)-1)
	}
	if !strings.Contains(gate, "name: CI Gate") {
		t.Error("the required check is named CI Gate")
	}
}
