//go:build !windows

// The release and CI scripts, run with sh (Linux CI runs them; Windows has
// no POSIX shell in its default PATH).

package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runScript(t *testing.T, stdin string, script string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func fakeChecksums(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	var lines []string
	for i, target := range []string{"darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64"} {
		lines = append(lines, fmt.Sprintf("%064x  kjak_%s_%s.tar.gz", i+1, version, target))
	}
	lines = append(lines, fmt.Sprintf("%064x  kjak_%s_windows_amd64.zip", 9, version))
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFormulaScript(t *testing.T) {
	dist := fakeChecksums(t, "0.1.0")
	out, err := runScript(t, "", "scripts/formula.sh", dist, "v0.1.0")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{
		`version "0.1.0"`,
		"https://github.com/magicdrive/kjak/releases/download/v0.1.0/kjak_0.1.0_darwin_arm64.tar.gz",
		fmt.Sprintf(`sha256 "%064x"`, 1),
		fmt.Sprintf(`sha256 "%064x"`, 4),
		`assert_match "kjak version v0.1.0"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("formula lacks %q", want)
		}
	}
	if strings.Contains(out, "windows") {
		t.Error("the formula names the windows archive")
	}
	if out, err := runScript(t, "", "scripts/formula.sh", dist, "v0.2.0"); err == nil {
		t.Errorf("a tag that does not match the archives was accepted: %s", out)
	}
}

// The formula is never written for a pre-release: the tap follows releases.
func TestFormulaScriptRefusesPreRelease(t *testing.T) {
	dist := fakeChecksums(t, "0.2.0-rc.1")
	if out, err := runScript(t, "", "scripts/formula.sh", dist, "v0.2.0-rc.1"); err == nil {
		t.Fatalf("a pre-release formula was written:\n%s", out)
	}
}

func TestFormulaScriptRefusesBadChecksum(t *testing.T) {
	dist := fakeChecksums(t, "0.1.0")
	p := filepath.Join(dist, "checksums.txt")
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, []byte(strings.Replace(string(b), fmt.Sprintf("%064x", 2), "nothex", 1)), 0o644)
	if out, err := runScript(t, "", "scripts/formula.sh", dist, "v0.1.0"); err == nil {
		t.Fatalf("a malformed checksum was accepted:\n%s", out)
	}
}

func gate(t *testing.T, needs string) error {
	t.Helper()
	cmd := exec.Command("sh", "scripts/ci-gate.sh")
	cmd.Env = append(os.Environ(), "RESULTS="+needs, "EXPECTED_JOBS=3")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("%s", out)
	}
	return err
}

func TestCIGateScript(t *testing.T) {
	ok := `{"a":{"result":"success"},"b":{"result":"success"},"c":{"result":"success"}}`
	if err := gate(t, ok); err != nil {
		t.Fatalf("all successful: %v", err)
	}
	for name, needs := range map[string]string{
		"failure":   `{"a":{"result":"success"},"b":{"result":"failure"},"c":{"result":"success"}}`,
		"skipped":   `{"a":{"result":"success"},"b":{"result":"skipped"},"c":{"result":"success"}}`,
		"cancelled": `{"a":{"result":"cancelled"},"b":{"result":"success"},"c":{"result":"success"}}`,
		"missing":   `{"a":{"result":"success"},"b":{"result":"success"}}`,
		"extra":     `{"a":{"result":"success"},"b":{"result":"success"},"c":{"result":"success"},"d":{"result":"success"}}`,
		"empty":     `{}`,
		"no result": `{"a":{},"b":{"result":"success"},"c":{"result":"success"}}`,
		"not json":  `success`,
	} {
		if err := gate(t, needs); err == nil {
			t.Errorf("%s: the gate passed", name)
		}
	}
}

// verify-dist refuses an archive with an extra file and a checksum mismatch.
func TestVerifyDistRefuses(t *testing.T) {
	mk := func(t *testing.T, extra bool, corrupt bool) string {
		dir := t.TempDir()
		var sums []string
		for _, target := range []string{"darwin_arm64", "darwin_amd64", "linux_arm64", "linux_amd64", "windows_amd64"} {
			name := "kjak_0.1.0_" + target + ".tar.gz"
			files := []string{"LICENSE", "README.md", "kjak"}
			if strings.HasPrefix(target, "windows") {
				name = "kjak_0.1.0_" + target + ".zip"
			}
			if extra {
				files = append(files, "evil.sh")
			}
			p := filepath.Join(dir, name)
			writeTarGz(t, p, files)
			b, _ := os.ReadFile(p)
			sum := sha256.Sum256(b)
			s := hex.EncodeToString(sum[:])
			if corrupt && target == "linux_amd64" {
				s = strings.Repeat("0", 64)
			}
			sums = append(sums, s+"  "+name)
		}
		_ = os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(strings.Join(sums, "\n")+"\n"), 0o644)
		return dir
	}
	if out, err := runScript(t, "", "scripts/verify-dist.sh", mk(t, false, true)); err == nil || !strings.Contains(out, "checksum mismatch") {
		t.Errorf("corrupt checksum: %v %s", err, out)
	}
	if out, err := runScript(t, "", "scripts/verify-dist.sh", mk(t, true, false)); err == nil {
		t.Errorf("extra file accepted: %s", out)
	}
}

// writeTarGz writes an archive of empty files (a .zip name gets a tar.gz
// body too: verify-dist fails on it before it matters here).
func writeTarGz(t *testing.T, path string, files []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range files {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: 0})
	}
	_ = tw.Close()
	_ = gz.Close()
}

// fixtureModule writes a module with critical tests: one passes, one skips
// itself (a test that never ran must not count as passed).
func fixtureModule(t *testing.T, manifest string, withSkip bool) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":                  "module example.com/fixture\n\ngo 1.27.0\n",
		"a/a_test.go":             "package a\n\nimport \"testing\"\n\nfunc TestCritical_Passes(t *testing.T) {}\n",
		"test/critical-tests.txt": manifest,
	}
	if withSkip {
		files["a/skip_test.go"] = "package a\n\nimport \"testing\"\n\nfunc TestCritical_Skips(t *testing.T) { t.Skip(\"not today\") }\n"
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func criticalScript(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	abs, _ := filepath.Abs("scripts/critical-tests.sh")
	cmd := exec.Command("sh", append([]string{abs}, args...)...)
	cmd.Env = append(os.Environ(), "CRITICAL_ROOT="+root, "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestCriticalScript(t *testing.T) {
	ok := fixtureModule(t, "a TestCritical_Passes\n", false)
	if out, err := criticalScript(t, ok, "run", "3"); err != nil || !strings.Contains(out, "1 tests passed 3 times each") {
		t.Fatalf("passing fixture: %v\n%s", err, out)
	}
	// listed, but skips itself: it never passed
	skip := fixtureModule(t, "a TestCritical_Passes\na TestCritical_Skips\n", true)
	if out, err := criticalScript(t, skip, "run", "2"); err == nil || !strings.Contains(out, "TestCritical_Skips passed 0 times, want 2") {
		t.Fatalf("a skipped critical test passed the run: %v\n%s", err, out)
	}
	// in the code, not in the manifest
	unlisted := fixtureModule(t, "a TestCritical_Passes\n", true)
	if out, err := criticalScript(t, unlisted, "check"); err == nil || !strings.Contains(out, "missing from") {
		t.Fatalf("an unlisted critical test: %v\n%s", err, out)
	}
	for name, manifest := range map[string]string{
		"empty":     "# nothing\n",
		"duplicate": "a TestCritical_Passes\na TestCritical_Passes\n",
		"absent":    "a TestCritical_Passes\na TestCritical_Gone\n",
		"misnamed":  "a TestCritical_Passes\na TestPasses\n",
		"malformed": "a TestCritical_Passes extra\n",
	} {
		if out, err := criticalScript(t, fixtureModule(t, manifest, false), "check"); err == nil {
			t.Errorf("%s manifest accepted:\n%s", name, out)
		}
	}
}

// make snapshot writes the formula of a local snapshot build (never
// published) with --snapshot; that option accepts "-snapshot" only.
func TestFormulaScriptSnapshot(t *testing.T) {
	snap := fakeChecksums(t, "0.0.1-snapshot")
	if out, err := runScript(t, "", "scripts/formula.sh", "--snapshot", snap); err != nil || !strings.Contains(out, `version "0.0.1-snapshot"`) {
		t.Fatalf("snapshot: %v\n%s", err, out)
	}
	if out, err := runScript(t, "", "scripts/formula.sh", snap); err == nil {
		t.Fatalf("a snapshot formula without --snapshot:\n%s", out)
	}
	rc := fakeChecksums(t, "0.2.0-rc.1")
	if out, err := runScript(t, "", "scripts/formula.sh", "--snapshot", rc); err == nil {
		t.Fatalf("--snapshot let a pre-release through:\n%s", out)
	}
}
