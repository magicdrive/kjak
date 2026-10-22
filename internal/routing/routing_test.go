package routing

import (
	"errors"
	"strings"
	"testing"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
)

func cfg(t *testing.T) *configure.File {
	t.Helper()
	f, err := configure.Parse([]byte(`
[defaults]
targets = ["team"]

[feathers.slack]
type = "slack"
webhook_url = { env = "A" }

[feathers.discord]
type = "discord"
webhook_url = { env = "B" }
levels = ["error", "critical"]

[feathers.off]
type = "slack"
enabled = false
webhook_url = { env = "C" }

[groups.team]
feathers = ["discord", "slack", "off"]

[groups.ops]
feathers = ["slack", "discord"]
`))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func names(p Plan) string {
	var s []string
	for _, e := range p.Entries {
		n := e.Name
		if e.Skip != nil {
			n += "(" + e.Skip.Reason + ")"
		}
		s = append(s, n)
	}
	return strings.Join(s, " ")
}

func TestResolveGroupSkipsExcludedMembers(t *testing.T) {
	p, err := Resolve(cfg(t), []string{"team"}, core.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(p); got != "discord(level_filter) slack off(disabled)" {
		t.Fatalf("plan = %s", got)
	}
	if p.Entries[0].Skip.Group != "team" || len(p.Active()) != 1 {
		t.Fatalf("plan = %+v", p)
	}
	p, _ = Resolve(cfg(t), []string{"team"}, core.LevelError)
	if got := names(p); got != "discord slack off(disabled)" {
		t.Fatalf("plan = %s", got)
	}
}

func TestResolveDirectFeatherMustBeUsable(t *testing.T) {
	if _, err := Resolve(cfg(t), []string{"off"}, core.LevelInfo); !errors.Is(err, ErrRouting) || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled: %v", err)
	}
	if _, err := Resolve(cfg(t), []string{"discord"}, core.LevelInfo); !errors.Is(err, ErrRouting) || !strings.Contains(err.Error(), "level") {
		t.Fatalf("filtered: %v", err)
	}
	// named directly and by a group: direct wins, so it is an error
	if _, err := Resolve(cfg(t), []string{"team", "discord"}, core.LevelInfo); err == nil {
		t.Fatal("a direct feather that is filtered out was skipped")
	}
}

func TestResolveDeduplicatesInFirstOrder(t *testing.T) {
	p, err := Resolve(cfg(t), []string{"ops", "team", "slack"}, core.LevelCritical)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(p); got != "slack discord off(disabled)" {
		t.Fatalf("plan = %s", got)
	}
	if !p.Entries[0].Direct || strings.Join(p.Entries[0].Groups, ",") != "ops,team" {
		t.Fatalf("slack = %+v", p.Entries[0])
	}
}

func TestResolveDefaultsAndErrors(t *testing.T) {
	p, err := Resolve(cfg(t), nil, core.LevelError)
	if err != nil || len(p.Entries) != 3 {
		t.Fatalf("defaults: %v %s", err, names(p))
	}
	if _, err := Resolve(&configure.File{}, nil, core.LevelInfo); !errors.Is(err, ErrRouting) {
		t.Fatalf("no target: %v", err)
	}
	if _, err := Resolve(cfg(t), []string{"ghost"}, core.LevelInfo); !errors.Is(err, ErrRouting) {
		t.Fatalf("unknown: %v", err)
	}
}
