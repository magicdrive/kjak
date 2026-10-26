package subcmd

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/output"
	"github.com/magicdrive/kjak/internal/template"
	"github.com/magicdrive/kjak/internal/textbank"
)

// Template runs kjak template <action>.
func Template(ctx context.Context, rt *Runtime, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(rt.Stderr, textbank.TemplateHelp)
		return ExitUsage
	}
	action, rest := args[0], args[1:]
	switch action {
	case "list":
		return templateList(ctx, rt, rest)
	case "show":
		return templateShow(ctx, rt, rest)
	case "set":
		return templateSet(ctx, rt, rest)
	case "remove":
		return templateRemove(ctx, rt, rest)
	case "render":
		return templateRender(ctx, rt, rest)
	case "-h", "--help", "help":
		return rt.help(textbank.TemplateHelp)
	}
	return rt.fail(ctx, fmt.Errorf("template: unknown action %q (list, show, set, remove, render)\nRun 'kjak template --help' for usage", action))
}

type templateView struct {
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Title  string `json:"title"`
	Body   string `json:"body"`
	Level  string `json:"level"`
	URL    string `json:"url"`
}

func templateList(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("template list", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.TemplateHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	views := []templateView{}
	for _, name := range sortedNames(l.Effective.Templates) {
		t := l.Effective.Templates[name]
		views = append(views, templateView{Name: name, Origin: l.Origin("template", name), Title: t.Title, Body: t.Body, Level: t.Level, URL: t.URL})
	}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, map[string]any{"schema_version": 1, "templates": views})
		return ExitOK
	}
	if len(views) == 0 {
		fmt.Fprintln(rt.Stdout, "no templates (add one with `kjak template set`)")
		return ExitOK
	}
	tw := tabwriter.NewWriter(rt.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tLEVEL\tTITLE\tORIGIN")
	for _, v := range views {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", v.Name, output.Shown(v.Level), output.Shown(v.Title), v.Origin)
	}
	_ = tw.Flush()
	return ExitOK
}

func templateShow(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseShow("template show", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.TemplateHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	t, ok := l.Effective.Templates[opt.Name]
	if !ok {
		return rt.fail(ctx, notFound("template", opt.Name))
	}
	v := templateView{Name: opt.Name, Origin: l.Origin("template", opt.Name), Title: t.Title, Body: t.Body, Level: t.Level, URL: t.URL}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, v)
		return ExitOK
	}
	fmt.Fprintf(rt.Stdout, "name    %s\norigin  %s\ntitle   %s\nlevel   %s\nurl     %s\nbody\n%s\n", v.Name, v.Origin, output.Shown(v.Title), output.Shown(v.Level), output.Shown(v.URL), indent(v.Body))
	return ExitOK
}

// indent is a body as it is printed: indented, its control characters
// (but line breaks and tabs) escaped.
func indent(s string) string {
	return "  " + strings.ReplaceAll(output.EscapeControls(s), "\n", "\n  ")
}

func templateSet(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseTemplateSet(args)
	if code, ok := rt.parsed(ctx, err, textbank.TemplateHelp); !ok {
		return code
	}
	if err := configure.ValidName(opt.Name); err != nil {
		return rt.fail(ctx, err)
	}
	t := configure.TemplateConfig{Title: opt.Title, Body: opt.Body, Level: opt.Level, URL: opt.URL}
	if opt.BodyStdin {
		b, err := readLimited(rt.Stdin, "standard input")
		if err != nil {
			return rt.fail(ctx, err)
		}
		t.Body = strings.TrimRight(string(b), "\r\n")
	}
	// the template must parse (variables are checked when it is used)
	if _, err := template.Check(opt.Name, t); err != nil {
		return rt.fail(ctx, err)
	}
	scoped := &option.ScopedNameOption{ConfigOption: opt.ConfigOption, Name: opt.Name, Scope: opt.Scope}
	return changeEntry(ctx, rt, scoped, "template", func(f *configure.File) error {
		if f.Templates == nil {
			f.Templates = map[string]configure.TemplateConfig{}
		}
		f.Templates[opt.Name] = t
		return nil
	}, "set template %q in the %s configuration\n")
}

func templateRemove(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseScopedName("template remove", args)
	if code, ok := rt.parsed(ctx, err, textbank.TemplateHelp); !ok {
		return code
	}
	return changeEntry(ctx, rt, opt, "template", func(f *configure.File) error {
		if _, ok := f.Templates[opt.Name]; !ok {
			return notFound("template", opt.Name)
		}
		delete(f.Templates, opt.Name)
		return nil
	}, "removed template %q from the %s configuration\n")
}

// templateRender prints the message a template makes, without sending it.
func templateRender(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseTemplateRender(args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.TemplateHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	t, ok := l.Effective.Templates[opt.Name]
	if !ok {
		return rt.fail(ctx, notFound("template", opt.Name))
	}
	vars, err := template.ParseVars(opt.Vars)
	if err != nil {
		return rt.fail(ctx, err)
	}
	msg, err := template.Render(opt.Name, t, vars)
	if err != nil {
		return rt.fail(ctx, err)
	}
	msg.Normalize()
	if err := msg.Validate(); err != nil {
		return rt.fail(ctx, err)
	}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, msg)
		return ExitOK
	}
	fmt.Fprintf(rt.Stdout, "title  %s\nlevel  %s\nurl    %s\nbody\n%s\n", output.Shown(msg.Title), msg.Level, output.Shown(msg.URL), indent(msg.Body))
	return ExitOK
}
