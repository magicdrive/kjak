package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// Every relative link of the README files and docs/ leads to a file that
// exists and, with a #fragment, to a heading of it (GitHub's anchors).

var (
	mdLink    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	mdHeading = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
)

// githubAnchor is the anchor GitHub gives a heading: lower case, spaces to
// hyphens, punctuation other than "-" and "_" removed.
func githubAnchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

func anchorsOf(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]bool{}
	seen := map[string]int{}
	inCode := false
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			continue
		}
		m := mdHeading.FindStringSubmatch(line)
		if inCode || m == nil {
			continue
		}
		a := githubAnchor(strings.ReplaceAll(m[1], "`", ""))
		if n := seen[a]; n > 0 {
			anchors[a+"-"+strconv.Itoa(n)] = true
		} else {
			anchors[a] = true
		}
		seen[a]++
	}
	return anchors
}

func TestDocsLinks(t *testing.T) {
	files, err := filepath.Glob("docs/*.md")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "README.md", "README_ja.md", "SECURITY.md")
	checked := 0
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		inCode := false
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "```") {
				inCode = !inCode
				continue
			}
			if inCode {
				continue
			}
			for _, m := range mdLink.FindAllStringSubmatch(line, -1) {
				link := m[1]
				if strings.Contains(link, "://") || strings.HasPrefix(link, "mailto:") {
					continue
				}
				target, fragment, _ := strings.Cut(link, "#")
				path := file
				if target != "" {
					path = filepath.Join(filepath.Dir(file), target)
				}
				fi, err := os.Stat(path)
				if err != nil {
					t.Errorf("%s: %s: %v", file, link, err)
					continue
				}
				checked++
				if fragment == "" || fi.IsDir() || !strings.HasSuffix(path, ".md") {
					continue
				}
				if !anchorsOf(t, path)[fragment] {
					t.Errorf("%s: %s: no heading with the anchor #%s in %s", file, link, fragment, path)
				}
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d links checked: the parsing found nothing", checked)
	}
}
