package option

import (
	"flag"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/textbank"
)

// contractTable returns the rows (cells, trimmed) of the table that follows
// the marker <!-- contract: name --> in docs/stability.md.
func contractTable(t *testing.T, name string) [][]string {
	t.Helper()
	b, err := os.ReadFile("../../../docs/stability.md")
	if err != nil {
		t.Fatal(err)
	}
	marker := "<!-- contract: " + name + " -->"
	_, rest, ok := strings.Cut(string(b), marker)
	if !ok {
		t.Fatalf("docs/stability.md has no %s", marker)
	}
	var rows [][]string
	for i, line := range strings.Split(strings.TrimLeft(rest, "\n"), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") {
			break
		}
		if i < 2 { // the header and the separator
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		t.Fatalf("docs/stability.md: the %s table is empty", name)
	}
	return rows
}

var codeSpan = regexp.MustCompile("`([^`]*)`")

// commandParsers are the parsers of every command and action, called as the
// subcommands call them (internal/commandline/subcmd).
var commandParsers = map[string]func(args []string) error{
	"kjak send":                   func(a []string) error { _, err := ParseSend(a); return err },
	"kjak feather list":           func(a []string) error { _, err := ParseList("feather list", a); return err },
	"kjak feather show":           func(a []string) error { _, err := ParseShow("feather show", a); return err },
	"kjak feather add":            func(a []string) error { _, err := ParseFeatherAdd(a); return err },
	"kjak feather remove":         func(a []string) error { _, err := ParseScopedName("feather remove", a); return err },
	"kjak feather enable":         func(a []string) error { _, err := ParseScopedName("feather enable", a); return err },
	"kjak feather disable":        func(a []string) error { _, err := ParseScopedName("feather disable", a); return err },
	"kjak feather types":          nil, // no flags
	"kjak feather plugins":        func(a []string) error { _, err := ParseList("feather plugins", a); return err },
	"kjak group list":             func(a []string) error { _, err := ParseList("group list", a); return err },
	"kjak group show":             func(a []string) error { _, err := ParseShow("group show", a); return err },
	"kjak group set":              func(a []string) error { _, err := ParseGroupSet(a); return err },
	"kjak group remove":           func(a []string) error { _, err := ParseScopedName("group remove", a); return err },
	"kjak template list":          func(a []string) error { _, err := ParseList("template list", a); return err },
	"kjak template show":          func(a []string) error { _, err := ParseShow("template show", a); return err },
	"kjak template set":           func(a []string) error { _, err := ParseTemplateSet(a); return err },
	"kjak template remove":        func(a []string) error { _, err := ParseScopedName("template remove", a); return err },
	"kjak template render":        func(a []string) error { _, err := ParseTemplateRender(a); return err },
	"kjak config path":            func(a []string) error { _, err := ParseList("config path", a); return err },
	"kjak config show":            func(a []string) error { _, err := ParseList("config show", a); return err },
	"kjak config validate":        func(a []string) error { _, err := ParseConfigValidate(a); return err },
	"kjak config init":            func(a []string) error { _, err := ParseConfigInit(a); return err },
	"kjak config trust":           func(a []string) error { _, err := ParseConfigTrust("config trust", a); return err },
	"kjak config untrust":         func(a []string) error { _, err := ParseConfigTrust("config untrust", a); return err },
	"kjak config migrate-secrets": func(a []string) error { _, err := ParseConfigMigrate(a); return err },
	"kjak version":                nil,
}

// helpOf is the help text that documents a command.
func helpOf(command string) string {
	switch strings.Fields(command)[1] {
	case "send":
		return textbank.SendHelp
	case "feather":
		return textbank.FeatherHelp
	case "group":
		return textbank.GroupHelp
	case "template":
		return textbank.TemplateHelp
	case "config":
		return textbank.ConfigHelp
	}
	return textbank.HelpMessage
}

// flagsOf lists the flags the parser of a command defines ("-t", "--to").
func flagsOf(t *testing.T, parse func([]string) error) []string {
	t.Helper()
	if parse == nil {
		return nil
	}
	var sets []*flag.FlagSet
	saved := flagSetCreated
	flagSetCreated = func(fs *flag.FlagSet) { sets = append(sets, fs) }
	defer func() { flagSetCreated = saved }()
	if err := parse([]string{"--help"}); err != ErrHelp {
		t.Fatalf("--help: %v", err)
	}
	if len(sets) != 1 {
		t.Fatalf("%d flag sets", len(sets))
	}
	var names []string
	sets[0].VisitAll(func(f *flag.Flag) {
		if len(f.Name) == 1 {
			names = append(names, "-"+f.Name)
		} else {
			names = append(names, "--"+f.Name)
		}
	})
	sort.Strings(names)
	return names
}

// The commands and flags of docs/stability.md are exactly those the parsers
// define, and the help of each command shows every flag (by one of its
// spellings).
func TestContractCommandsAndFlags(t *testing.T) {
	documented := map[string]bool{}
	for _, row := range contractTable(t, "commands") {
		if len(row) != 2 {
			t.Fatalf("row %q", row)
		}
		m := codeSpan.FindStringSubmatch(row[0])
		if m == nil {
			t.Fatalf("row %q: no command", row)
		}
		command := m[1]
		documented[command] = true
		parse, ok := commandParsers[command]
		if !ok {
			t.Errorf("%s: documented, but the test knows no parser of it (add it to commandParsers)", command)
			continue
		}
		var want []string
		for _, f := range codeSpan.FindAllStringSubmatch(row[1], -1) {
			want = append(want, f[1])
		}
		sort.Strings(want)
		got := flagsOf(t, parse)
		if !slices.Equal(got, want) {
			t.Errorf("%s: the parser defines %v, docs/stability.md lists %v", command, got, want)
		}
		// the help names every flag by one of its spellings: the spellings
		// of a flag are written together in the table ("-t"/"--to")
		help := helpOf(command)
		for _, spellings := range strings.Split(row[1], ",") {
			found := false
			for _, f := range codeSpan.FindAllStringSubmatch(spellings, -1) {
				if regexp.MustCompile(`(^|[\s\[|,])` + regexp.QuoteMeta(f[1]) + `([\s=\]|,]|$)`).MatchString(help) {
					found = true
				}
			}
			if strings.TrimSpace(spellings) != "" && !found {
				t.Errorf("%s: the help does not show %s", command, strings.TrimSpace(spellings))
			}
		}
	}
	for command := range commandParsers {
		if !documented[command] {
			t.Errorf("%s: not in docs/stability.md", command)
		}
	}
}
