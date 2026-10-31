package subcmd

import (
	"context"
	"fmt"
	"runtime"
	"text/tabwriter"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/feather/external"
	"github.com/magicdrive/kjak/internal/textbank"
)

// featherPlugins lists the plugins of PATH and the feathers that use them.
// Nothing is run: a plugin found is not a plugin allowed.
func featherPlugins(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("feather plugins", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.FeatherHelp); !ok {
		return code
	}
	pathList, _ := rt.LookupEnv("PATH")
	found, skipped := external.Discover(pathList, runtime.GOOS)
	used := map[string][]string{}
	if l, err := rt.load(opt.ConfigOption); err == nil {
		for _, name := range sortedNames(l.Effective.Feathers) {
			if fc := l.Effective.Feathers[name]; fc.Type == configure.TypeExternal {
				used[fc.Plugin] = append(used[fc.Plugin], name)
			}
		}
	}
	type view struct {
		external.Found
		Feathers []string `json:"feathers"`
	}
	views := []view{}
	for _, f := range found {
		views = append(views, view{Found: f, Feathers: nonNil(used[f.Name])})
	}
	if opt.Format == "json" {
		type skip struct {
			Element string `json:"element"`
			Why     string `json:"why"`
		}
		sk := []skip{}
		for _, s := range skipped {
			sk = append(sk, skip{s.Element, s.Why})
		}
		_ = writeJSON(rt.Stdout, map[string]any{"schema_version": 1, "plugins": views, "skipped_path_elements": sk})
		return ExitOK
	}
	if len(views) == 0 {
		fmt.Fprintln(rt.Stdout, "no kjak-feather-* executable on PATH")
	} else {
		tw := tabwriter.NewWriter(rt.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "PLUGIN\tPATH\tSTATE\tFEATHERS")
		for _, v := range views {
			state := "ok"
			switch {
			case v.Homebrew:
				state = fmt.Sprintf("Homebrew: runs only for a feather with sha256 = %q (or a project trust)", v.SHA256)
				if v.Shadowed {
					state += "; duplicate (an earlier PATH directory has it: pin path =)"
				}
			case v.Problem != "":
				state = "refused: " + v.Problem
			case v.Shadowed:
				state = "duplicate (an earlier PATH directory has it: pin path =)"
			}
			feathers := "-"
			if len(v.Feathers) > 0 {
				feathers = fmt.Sprint(v.Feathers)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.Name, v.Path, state, feathers)
		}
		_ = tw.Flush()
	}
	for _, s := range skipped {
		fmt.Fprintf(rt.Stdout, "not searched: PATH element %q (%s)\n", s.Element, s.Why)
	}
	fmt.Fprintln(rt.Stdout, "A plugin runs only for a feather of type \"external\" that names it (docs/plugins.md).")
	return ExitOK
}
