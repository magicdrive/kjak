package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/magicdrive/kjak/"

// imports lists the non-test imports of every package directory under root.
func imports(t *testing.T, root string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		pkg := filepath.ToSlash(filepath.Dir(path))
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			out[pkg] = append(out[pkg], p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The dependency rules of docs/development.md: the domain does not know the
// command line, the core knows only the standard library, the feathers do
// not know the engine, and main and cmd stay thin.
func TestArchitectureDependencyRules(t *testing.T) {
	all := imports(t, "internal")
	for pkg, list := range imports(t, "cmd") {
		all[pkg] = list
	}
	if len(all) < 10 {
		t.Fatalf("only %d packages found", len(all))
	}
	for pkg, list := range all {
		for _, im := range list {
			local := strings.TrimPrefix(im, module)
			isLocal := local != im
			switch {
			case !strings.HasPrefix(pkg, "internal/commandline") && pkg != "cmd" && isLocal && strings.HasPrefix(local, "internal/commandline"):
				t.Errorf("%s imports %s: the domain must not depend on the command line", pkg, im)
			case pkg == "internal/core" && (isLocal || strings.Contains(im, ".")):
				t.Errorf("internal/core imports %s: the core model depends on the standard library only", im)
			case strings.HasPrefix(pkg, "internal/feather") && isLocal && (strings.HasPrefix(local, "internal/delivery") || strings.HasPrefix(local, "internal/routing")):
				t.Errorf("%s imports %s: a feather does not know the engine", pkg, im)
			case pkg == "cmd" && isLocal && local != "internal/commandline":
				t.Errorf("cmd imports %s: cmd hands over to internal/commandline only", im)
			}
		}
	}
}
