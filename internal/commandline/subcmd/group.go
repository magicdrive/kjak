package subcmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/textbank"
)

// Group runs kjak group <action>.
func Group(ctx context.Context, rt *Runtime, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(rt.Stderr, textbank.GroupHelp)
		return ExitUsage
	}
	action, rest := args[0], args[1:]
	switch action {
	case "list":
		return groupList(ctx, rt, rest)
	case "show":
		return groupShow(ctx, rt, rest)
	case "set":
		return groupSet(ctx, rt, rest)
	case "remove":
		return groupRemove(ctx, rt, rest)
	case "-h", "--help", "help":
		return rt.help(textbank.GroupHelp)
	}
	return rt.fail(ctx, fmt.Errorf("group: unknown action %q (list, show, set, remove)\nRun 'kjak group --help' for usage", action))
}

type groupView struct {
	Name     string   `json:"name"`
	Origin   string   `json:"origin"`
	Feathers []string `json:"feathers"`
}

func groupList(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("group list", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.GroupHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	views := []groupView{}
	for _, name := range sortedNames(l.Effective.Groups) {
		views = append(views, groupView{Name: name, Origin: l.Origin("group", name), Feathers: l.Effective.Groups[name].Feathers})
	}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, map[string]any{"schema_version": 1, "groups": views})
		return ExitOK
	}
	if len(views) == 0 {
		fmt.Fprintln(rt.Stdout, "no groups (add one with `kjak group set`)")
		return ExitOK
	}
	tw := tabwriter.NewWriter(rt.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tFEATHERS\tORIGIN")
	for _, v := range views {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", v.Name, strings.Join(v.Feathers, ","), v.Origin)
	}
	_ = tw.Flush()
	return ExitOK
}

func groupShow(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseShow("group show", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.GroupHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	g, ok := l.Effective.Groups[opt.Name]
	if !ok {
		return rt.fail(ctx, notFound("group", opt.Name))
	}
	v := groupView{Name: opt.Name, Origin: l.Origin("group", opt.Name), Feathers: g.Feathers}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, v)
		return ExitOK
	}
	fmt.Fprintf(rt.Stdout, "name      %s\norigin    %s\nfeathers  %s\n", v.Name, v.Origin, strings.Join(v.Feathers, ", "))
	return ExitOK
}

func groupSet(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseGroupSet(args)
	if code, ok := rt.parsed(ctx, err, textbank.GroupHelp); !ok {
		return code
	}
	if err := configure.ValidName(opt.Name); err != nil {
		return rt.fail(ctx, err)
	}
	scoped := &option.ScopedNameOption{ConfigOption: opt.ConfigOption, Name: opt.Name, Scope: opt.Scope}
	return changeEntry(ctx, rt, scoped, "group", func(f *configure.File) error {
		if _, isFeather := f.Feathers[opt.Name]; isFeather {
			return fmt.Errorf("%q is a feather", opt.Name)
		}
		if f.Groups == nil {
			f.Groups = map[string]configure.GroupConfig{}
		}
		// a group is replaced whole
		f.Groups[opt.Name] = configure.GroupConfig{Feathers: opt.Feathers}
		return nil
	}, "set group %q in the %s configuration\n")
}

func groupRemove(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseScopedName("group remove", args)
	if code, ok := rt.parsed(ctx, err, textbank.GroupHelp); !ok {
		return code
	}
	return changeEntry(ctx, rt, opt, "group", func(f *configure.File) error {
		if _, ok := f.Groups[opt.Name]; !ok {
			return notFound("group", opt.Name)
		}
		delete(f.Groups, opt.Name)
		return nil
	}, "removed group %q from the %s configuration\n")
}
