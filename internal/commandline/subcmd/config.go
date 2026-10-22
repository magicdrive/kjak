package subcmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/textbank"
)

// Config runs kjak config <action>.
func Config(ctx context.Context, rt *Runtime, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(rt.Stderr, textbank.ConfigHelp)
		return ExitUsage
	}
	action, rest := args[0], args[1:]
	switch action {
	case "path":
		return configPath(ctx, rt, rest)
	case "show":
		return configShow(ctx, rt, rest)
	case "validate":
		return configValidate(ctx, rt, rest)
	case "init":
		return configInit(ctx, rt, rest)
	case "trust":
		return configTrust(ctx, rt, rest)
	case "untrust":
		return configUntrust(ctx, rt, rest)
	case "-h", "--help", "help":
		return rt.help(textbank.ConfigHelp)
	}
	return rt.fail(ctx, fmt.Errorf("config: unknown action %q (path, show, validate, init, trust, untrust)\nRun 'kjak config --help' for usage", action))
}

// projectState describes the project file: none, trusted, untrusted.
func projectState(p configure.Paths) (state string, src *configure.Source, err error) {
	if p.Project == "" {
		return "disabled (--no-project)", nil, nil
	}
	src, err = configure.ReadFile(p.Project)
	if err != nil {
		return "", nil, err
	}
	if !src.Exists {
		return "none", src, nil
	}
	ts, err := configure.ReadTrustStore(p.TrustStore)
	if err != nil {
		return "", nil, err
	}
	ok, err := ts.IsTrusted(src.Path, src.Hash)
	if err != nil {
		return "", nil, err
	}
	if ok {
		return "trusted", src, nil
	}
	if _, known := ts.Entry(src.Path); known {
		return "untrusted (changed since it was trusted)", src, nil
	}
	return "untrusted", src, nil
}

func configPath(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("config path", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	state, _, err := projectState(p)
	if err != nil {
		state = "invalid: " + err.Error()
	}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, map[string]any{
			"schema_version": 1, "global": p.Global, "project": p.Project,
			"project_state": state, "trust_store": p.TrustStore,
		})
		return ExitOK
	}
	fmt.Fprintf(rt.Stdout, "global       %s\n", p.Global)
	if p.Project != "" {
		fmt.Fprintf(rt.Stdout, "project      %s (%s)\n", p.Project, state)
	} else {
		fmt.Fprintf(rt.Stdout, "project      %s\n", state)
	}
	fmt.Fprintf(rt.Stdout, "trust store  %s\n", p.TrustStore)
	return ExitOK
}

// redacted copies f with every literal credential replaced by Redacted.
func redacted(f *configure.File) *configure.File {
	out := configure.Merge(f, nil)
	hide := func(r *credential.Ref) *credential.Ref {
		if r == nil || r.Literal == nil {
			return r
		}
		v := credential.Redacted
		return &credential.Ref{Literal: &v}
	}
	for name, fc := range out.Feathers {
		fc.WebhookURL = hide(fc.WebhookURL)
		fc.Password = hide(fc.Password)
		out.Feathers[name] = fc
	}
	return out
}

func configShow(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("config show", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	eff := redacted(l.Effective)
	if opt.Format == "json" {
		feathers := map[string]featherView{}
		for name, fc := range l.Effective.Feathers {
			feathers[name] = viewFeather(l, name, fc)
		}
		s := l.Effective.Settings()
		_ = writeJSON(rt.Stdout, map[string]any{
			"schema_version": 1,
			"sources": map[string]any{
				"global": l.Global.Path, "project": l.Paths.Project, "project_trusted": l.ProjectTrusted,
			},
			"defaults": map[string]any{
				"timeout": s.Timeout.String(), "max_retries": s.MaxRetries, "concurrency": s.Concurrency,
				"level": s.Level, "targets": nonNil(s.Targets),
			},
			"feathers":  feathers,
			"groups":    eff.Groups,
			"templates": eff.Templates,
		})
		return ExitOK
	}
	body, err := configure.Encode(eff)
	if err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, "# effective configuration (literal secrets redacted)\n# global:  %s\n", l.Global.Path)
	if l.ProjectTrusted {
		fmt.Fprintf(rt.Stdout, "# project: %s (trusted)\n", l.Project.Path)
	}
	fmt.Fprintf(rt.Stdout, "\n%s", body)
	return ExitOK
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func configValidate(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("config validate", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	global, err := configure.ReadFile(p.Global)
	if err != nil {
		return rt.fail(ctx, err)
	}
	state, project, err := projectState(p)
	if err != nil {
		return rt.fail(ctx, err)
	}
	var projectFile *configure.File
	if project != nil && project.Exists {
		projectFile = project.File
	}
	// references are checked as if the project file were used
	if err := configure.ValidateReferences(configure.Merge(global.File, projectFile)); err != nil {
		return rt.fail(ctx, err)
	}
	if global.Exists {
		fmt.Fprintf(rt.Stdout, "%s: OK\n", global.Path)
	} else {
		fmt.Fprintf(rt.Stdout, "%s: none\n", global.Path)
	}
	if project != nil && project.Exists {
		fmt.Fprintf(rt.Stdout, "%s: OK (%s)\n", project.Path, state)
		if state != "trusted" {
			fmt.Fprintln(rt.Stderr, "kjak: the project configuration is not trusted: a run refuses it until `kjak config trust`")
		}
	}
	return ExitOK
}

func configInit(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseConfigInit(args)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if _, err := configure.Parse([]byte(textbank.DefaultConfigToml)); err != nil {
		return rt.fail(ctx, fmt.Errorf("internal error: the default configuration is invalid: %w", err))
	}
	if err := configure.WriteNew(p.Global, []byte(textbank.DefaultConfigToml), opt.Force); err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, "created %s\n", p.Global)
	return ExitOK
}

func configTrust(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseConfigTrust("config trust", args)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if p.Project == "" {
		return rt.fail(ctx, errors.New("config trust: --no-project leaves no project file to trust"))
	}
	// the hash trusted is the hash of the content read (and shown) here
	src, err := configure.ReadFile(p.Project)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if !src.Exists {
		return rt.fail(ctx, fmt.Errorf("no project configuration: %s", p.Project))
	}
	canon, err := configure.CanonicalPath(src.Path)
	if err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, "project configuration %s\nsha256 %s\n", canon, src.Hash)
	describeProject(rt, src.File)
	if !opt.Yes {
		if !rt.StdinIsTerminal {
			return rt.fail(ctx, errors.New("config trust: not a terminal; review the file, then pass --yes"))
		}
		fmt.Fprint(rt.Stdout, "Trust this exact content? [y/N] ")
		answer, _ := bufio.NewReader(rt.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Fprintln(rt.Stdout, "not trusted")
			return ExitUsage
		}
	}
	if err := configure.Trust(p.TrustStore, src.Path, src.Hash, rt.Clock.Now()); err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, "trusted %s\n", canon)
	return ExitOK
}

// describeProject lists what a project file makes kjak do: the credentials
// it reads and where it sends them.
func describeProject(rt *Runtime, f *configure.File) {
	for _, name := range sortedNames(f.Feathers) {
		fc := f.Feathers[name]
		switch fc.Type {
		case configure.TypeEmail:
			secret := "no password"
			if fc.Password != nil {
				secret = "password from " + fc.Password.Describe()
			}
			fmt.Fprintf(rt.Stdout, "  feather %s: email via %s, %s, to %s\n", name, fc.Host, secret, strings.Join(fc.To, ", "))
		default:
			src := "(none)"
			if fc.WebhookURL != nil {
				src = fc.WebhookURL.Describe()
			}
			fmt.Fprintf(rt.Stdout, "  feather %s: %s, webhook URL from %s\n", name, fc.Type, src)
		}
	}
	for _, name := range sortedNames(f.Groups) {
		fmt.Fprintf(rt.Stdout, "  group %s: %s\n", name, strings.Join(f.Groups[name].Feathers, ", "))
	}
	for _, name := range sortedNames(f.Templates) {
		fmt.Fprintf(rt.Stdout, "  template %s\n", name)
	}
}

func configUntrust(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseConfigTrust("config untrust", args)
	if code, ok := rt.parsed(ctx, err, textbank.ConfigHelp); !ok {
		return code
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if p.Project == "" {
		return rt.fail(ctx, errors.New("config untrust: --no-project leaves no project file"))
	}
	removed, err := configure.Untrust(p.TrustStore, p.Project)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if removed {
		fmt.Fprintf(rt.Stdout, "untrusted %s\n", p.Project)
	} else {
		fmt.Fprintf(rt.Stdout, "%s was not trusted\n", p.Project)
	}
	return ExitOK
}
