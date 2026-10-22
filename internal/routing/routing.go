// Package routing resolves the targets of a send (feather and group names)
// into the ordered list of feathers to deliver to.
package routing

import (
	"errors"
	"fmt"
	"slices"

	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/core"
)

// ErrRouting wraps every preflight error of routing.
var ErrRouting = errors.New("routing error")

// Skip reasons.
const (
	SkipDisabled    = "disabled"
	SkipLevelFilter = "level_filter"
)

// Entry is one feather of a plan.
type Entry struct {
	Name   string
	Config configure.FeatherConfig
	Direct bool     // named by the send itself
	Groups []string // the groups that name it, in the order of the send
	Skip   *core.SkipInfo
}

// Plan is the feathers of a send, each once, in the order they are first
// named (a group names its members in its own order).
type Plan struct {
	Entries []Entry
}

// Active are the entries that are delivered to.
func (p Plan) Active() []Entry {
	var out []Entry
	for _, e := range p.Entries {
		if e.Skip == nil {
			out = append(out, e)
		}
	}
	return out
}

// Resolve builds the plan of targets (feather or group names; empty: the
// configuration's defaults.targets) for a message of level.
//
// A feather named directly must be enabled and accept the level: anything
// else is an error, since the user asked for it explicitly. A group member
// that is disabled or filtered out is skipped and reported.
func Resolve(cfg *configure.File, targets []string, level core.Level) (Plan, error) {
	if len(targets) == 0 {
		targets = cfg.Defaults.Targets
	}
	if len(targets) == 0 {
		return Plan{}, fmt.Errorf("%w: no target: name one with --to, or set defaults.targets", ErrRouting)
	}
	var entries []*Entry
	index := map[string]*Entry{}
	reach := func(name string) *Entry {
		if e, ok := index[name]; ok {
			return e
		}
		e := &Entry{Name: name, Config: cfg.Feathers[name]}
		index[name] = e
		entries = append(entries, e)
		return e
	}
	for _, t := range targets {
		if _, ok := cfg.Feathers[t]; ok {
			reach(t).Direct = true
			continue
		}
		g, ok := cfg.Groups[t]
		if !ok {
			return Plan{}, fmt.Errorf("%w: no feather or group named %q", ErrRouting, t)
		}
		for _, m := range g.Feathers {
			if _, ok := cfg.Feathers[m]; !ok {
				return Plan{}, fmt.Errorf("%w: group %q: no feather named %q", ErrRouting, t, m)
			}
			e := reach(m)
			if !slices.Contains(e.Groups, t) {
				e.Groups = append(e.Groups, t)
			}
		}
	}

	plan := Plan{Entries: make([]Entry, 0, len(entries))}
	for _, e := range entries {
		reason := exclusion(e.Config, level)
		if reason != "" {
			if e.Direct {
				return Plan{}, fmt.Errorf("%w: feather %q %s", ErrRouting, e.Name, describe(reason, e.Config, level))
			}
			e.Skip = &core.SkipInfo{Reason: reason, Group: e.Groups[0]}
		}
		plan.Entries = append(plan.Entries, *e)
	}
	return plan, nil
}

func exclusion(fc configure.FeatherConfig, level core.Level) string {
	if !fc.IsEnabled() {
		return SkipDisabled
	}
	if len(fc.Levels) > 0 {
		for _, l := range fc.Levels {
			if pl, err := core.ParseLevel(l); err == nil && pl == level {
				return ""
			}
		}
		return SkipLevelFilter
	}
	return ""
}

func describe(reason string, fc configure.FeatherConfig, level core.Level) string {
	if reason == SkipDisabled {
		return "is disabled"
	}
	return fmt.Sprintf("does not accept level %s (levels = %v)", level, fc.Levels)
}
