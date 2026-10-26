package subcmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/feather/external"
	"github.com/magicdrive/kjak/internal/fileacl"
	"github.com/magicdrive/kjak/internal/output"
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
	case "migrate-secrets":
		return configMigrateSecrets(ctx, rt, rest)
	case "-h", "--help", "help":
		return rt.help(textbank.ConfigHelp)
	}
	return rt.fail(ctx, fmt.Errorf("config: unknown action %q (path, show, validate, init, trust, untrust, migrate-secrets)\nRun 'kjak config --help' for usage", action))
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
		entry, _ := ts.Entry(src.Path)
		if _, err := configure.CheckProjectSecrets(p, entry); err != nil {
			if errors.Is(err, configure.ErrUntrustedProject) {
				return "untrusted (its secrets file changed since it was trusted, or is not covered by the trust)", src, nil
			}
			return "untrusted (its secrets file is refused: " + err.Error() + ")", src, nil
		}
		return "trusted", src, nil
	}
	if _, known := ts.Entry(src.Path); known {
		return "untrusted (changed since it was trusted)", src, nil
	}
	return "untrusted", src, nil
}

// secretsState describes a secrets file without reading out a value:
// none, the number of secrets, or why it is refused.
func secretsState(path string, noFollow bool) string {
	sf, err := configure.ReadSecretsFile(path, noFollow)
	if err != nil {
		return "refused: " + err.Error()
	}
	if !sf.Exists {
		return "none"
	}
	return fmt.Sprintf("%d secret(s)", len(sf.Names()))
}

// projectSecretsState is the state of ./.kjak.secrets.toml: it serves the
// project configuration only, as trusted with it.
func projectSecretsState(p configure.Paths, projState string) string {
	if p.ProjectSecrets == "" {
		return "disabled (--no-project)"
	}
	st := secretsState(p.ProjectSecrets, true)
	switch {
	case st == "none" || strings.HasPrefix(st, "refused"):
		return st
	case projState == "none":
		return st + ", unused (no project configuration)"
	case projState == "trusted":
		return st + ", trusted with the project"
	}
	return st + ", not trusted"
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
	secrets := secretsState(p.Secrets, false)
	projSecrets := projectSecretsState(p, state)
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, map[string]any{
			"schema_version": 1, "global": p.Global, "project": p.Project,
			"project_state": state, "trust_store": p.TrustStore,
			"global_source":               p.GlobalFrom.Kind(),
			"secrets":                     p.Secrets,
			"secrets_source":              p.SecretsFrom.Kind(),
			"secrets_state":               secrets,
			"project_secrets":             p.ProjectSecrets,
			"project_secrets_state":       projSecrets,
			"secrets_permissions_checked": configure.SecretsChecksPermissions,
			"secrets_permission_model":    fileacl.Model,
		})
		return ExitOK
	}
	tw := tabwriter.NewWriter(rt.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "global (%s)\t%s\n", p.GlobalFrom.Describe("--config", "KJAK_CONFIG"), output.Shown(p.Global))
	if p.Project != "" {
		fmt.Fprintf(tw, "project (current)\t%s (%s)\n", output.Shown(p.Project), state)
	} else {
		fmt.Fprintf(tw, "project\t%s\n", state)
	}
	fmt.Fprintf(tw, "secrets (%s)\t%s (%s)\n", p.SecretsFrom.Describe("--secrets", "KJAK_SECRETS"), output.Shown(p.Secrets), secrets)
	if p.ProjectSecrets != "" {
		fmt.Fprintf(tw, "project secrets (current)\t%s (%s)\n", output.Shown(p.ProjectSecrets), projSecrets)
	} else {
		fmt.Fprintf(tw, "project secrets\t%s\n", projSecrets)
	}
	fmt.Fprintf(tw, "trust store\t%s\n", output.Shown(p.TrustStore))
	fmt.Fprintf(tw, "secrets checks\t%s\n", fileacl.Model)
	_ = tw.Flush()
	return ExitOK
}

// redacted copies f with every literal credential replaced by Redacted.
func redacted(f *configure.File) *configure.File {
	out := configure.Merge(f, nil)
	for _, c := range configure.Credentials(out) {
		if c.Ref.Literal != nil {
			v := credential.Redacted
			c.Set(&credential.Ref{Literal: &v})
		}
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
	fmt.Fprintf(rt.Stdout, "# effective configuration (literal secrets redacted)\n# global:  %s\n", output.Shown(l.Global.Path))
	if l.ProjectTrusted {
		fmt.Fprintf(rt.Stdout, "# project: %s (trusted)\n", output.Shown(l.Project.Path))
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
	opt, err := option.ParseConfigValidate(args)
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
	// the secrets files are checked (private, valid), their values never shown
	if _, err := configure.ReadSecretsFile(p.Secrets, false); err != nil {
		return rt.fail(ctx, err)
	}
	if projectFile != nil {
		if _, err := configure.ReadSecretsFile(p.ProjectSecrets, true); err != nil {
			return rt.fail(ctx, err)
		}
	}
	var literals []string
	for _, src := range []*configure.Source{global, project} {
		if src != nil && src.File != nil {
			if lits := configure.Literals(src.File); len(lits) > 0 {
				literals = append(literals, fmt.Sprintf("%s: %s", src.Path, literalFields(lits)))
			}
		}
	}
	if opt.Strict && len(literals) > 0 {
		return rt.fail(ctx, withCode(output.CodeConfig, fmt.Errorf("%w: literal secrets are refused by --strict (move them to a secrets file: kjak config migrate-secrets):\n  %s", configure.ErrInvalidConfig, strings.Join(literals, "\n  "))))
	}
	note := func(src *configure.Source) string {
		if n := len(configure.Literals(src.File)); n > 0 {
			return fmt.Sprintf(", %d literal secret(s): deprecated, see kjak config migrate-secrets", n)
		}
		return ""
	}
	if global.Exists {
		fmt.Fprintf(rt.Stdout, "%s: OK%s\n", global.Path, note(global))
	} else {
		fmt.Fprintf(rt.Stdout, "%s: none\n", global.Path)
	}
	if project != nil && project.Exists {
		fmt.Fprintf(rt.Stdout, "%s: OK (%s)%s\n", output.Shown(project.Path), state, note(project))
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
	// the hashes trusted are the hashes of the content read (and shown) here
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
	secrets, err := configure.ReadSecretsFile(p.ProjectSecrets, true)
	if err != nil {
		return rt.fail(ctx, err)
	}
	grant := configure.TrustGrant{}
	fmt.Fprintf(rt.Stdout, "project configuration %s\nsha256 %s\n", output.Shown(canon), src.Hash)
	if secrets.Exists {
		if grant.SecretsPath, err = configure.CanonicalPath(secrets.Path); err != nil {
			return rt.fail(ctx, err)
		}
		grant.SecretsSHA256 = secrets.Hash
		if err := configure.NotPersonal(p, secrets); err != nil {
			return rt.fail(ctx, err)
		}
		fmt.Fprintf(rt.Stdout, "project secrets %s\nsha256 %s (values not shown)\n", output.Shown(grant.SecretsPath), secrets.Hash)
		for _, n := range secrets.Names() {
			fmt.Fprintf(rt.Stdout, "  defines secret %s\n", output.Shown(n))
		}
	}
	describeProject(rt, src.File)
	// the executables of its plugins, as they are now: a later change of
	// one withdraws the trust
	pathList, _ := rt.LookupEnv("PATH")
	for _, name := range sortedNames(src.File.Feathers) {
		fc := src.File.Feathers[name]
		if fc.Type != configure.TypeExternal {
			continue
		}
		if _, done := (configure.TrustEntry{Plugins: grant.Plugins}).Plugin(fc.Plugin); done {
			continue
		}
		exe, err := external.Resolve(fc.Plugin, fc.Path, pathList, runtime.GOOS)
		if err != nil {
			return rt.fail(ctx, fmt.Errorf("config trust: feather %s: %w (install the plugin, then trust)", name, err))
		}
		if fc.SHA256 != "" && fc.SHA256 != exe.SHA256 {
			return rt.fail(ctx, fmt.Errorf("config trust: feather %s: %s has SHA-256 %s, the configuration pins %s", name, exe.Real, exe.SHA256, fc.SHA256))
		}
		grant.Plugins = append(grant.Plugins, configure.TrustedPlugin{Name: fc.Plugin, Path: exe.Real, SHA256: exe.SHA256})
		fmt.Fprintf(rt.Stdout, "plugin %s: %s\nsha256 %s\n", output.Shown(fc.Plugin), output.Shown(exe.Real), exe.SHA256)
	}
	// the names the project reads from the personal secrets file: each one
	// is shown and allowed explicitly
	for _, name := range configure.SecretNames(src.File) {
		if !secrets.Has(name) {
			grant.UserSecrets = append(grant.UserSecrets, name)
		}
	}
	if len(grant.UserSecrets) > 0 {
		fmt.Fprintf(rt.Stdout, "The project asks to read these secrets from YOUR %s secrets file %s:\n", p.SecretsFrom.Kind(), output.Shown(p.Secrets))
		for _, name := range grant.UserSecrets {
			fmt.Fprintf(rt.Stdout, "  secret %s\n", output.Shown(name))
		}
	}
	// the environment variables and keychain entries its feathers send to
	// a destination of the project's choosing (a URL, an SMTP host, a
	// plugin)
	if gated := configure.GatedNames(src.File); len(gated) > 0 {
		fmt.Fprintln(rt.Stdout, "The project asks to give these of YOUR environment variables and keychain entries to destinations it chooses (its webhook URLs, SMTP hosts, http URLs, plugins):")
		for _, name := range gated {
			fmt.Fprintf(rt.Stdout, "  %s\n", output.Shown(name))
		}
		grant.UserSecrets = append(grant.UserSecrets, gated...)
	}
	if len(grant.UserSecrets) > 0 {
		fmt.Fprintln(rt.Stdout, "Its feathers would send them to the destinations listed above.")
	}
	if !opt.Yes {
		if !rt.StdinIsTerminal {
			return rt.fail(ctx, errors.New("config trust: not a terminal; review the file, then pass --yes"))
		}
		in := bufio.NewReader(rt.Stdin)
		if !ask(rt, in, "Trust this exact content? [y/N] ") {
			fmt.Fprintln(rt.Stdout, "not trusted")
			return ExitUsage
		}
		if len(grant.UserSecrets) > 0 && !ask(rt, in, fmt.Sprintf("Allow it to read the %d secret(s) above? [y/N] ", len(grant.UserSecrets))) {
			fmt.Fprintln(rt.Stdout, "not trusted")
			return ExitUsage
		}
	} else if len(grant.UserSecrets) > 0 && !opt.AllowUserSecrets {
		return rt.fail(ctx, fmt.Errorf("config trust: the project reads %d of your secret(s) (%s); review them, then pass --allow-user-secrets with --yes", len(grant.UserSecrets), strings.Join(grant.UserSecrets, ", ")))
	}
	if err := configure.TrustWith(p.TrustStore, src.Path, src.Hash, rt.Clock.Now(), grant); err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, "trusted %s\n", output.Shown(canon))
	return ExitOK
}

// ask asks a yes/no question on the terminal (no: anything but y or yes).
func ask(rt *Runtime, in *bufio.Reader, question string) bool {
	fmt.Fprint(rt.Stdout, question)
	answer, _ := in.ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes"
}

// describeProject lists what a project file makes kjak do: the credentials
// it reads and where it sends them.
func describeProject(rt *Runtime, f *configure.File) {
	for _, name := range sortedNames(f.Feathers) {
		fc := f.Feathers[name]
		switch fc.Type {
		case configure.TypeExternal:
			fmt.Fprintf(rt.Stdout, "  feather %s: external plugin %s (runs with your rights)\n", output.Shown(name), output.Shown(fc.Plugin))
			if s := describeSecrets(fc); len(s) > 0 {
				fmt.Fprintf(rt.Stdout, "    secrets given to it: %s\n", output.ShownList(s))
			}
			if len(fc.Env) > 0 {
				fmt.Fprintf(rt.Stdout, "    environment variables given to it: %s\n", output.ShownList(fc.Env))
			}
			if len(fc.Settings) > 0 {
				var kv []string
				for _, k := range sortedNames(fc.Settings) {
					kv = append(kv, output.Shown(k)+"="+strconv.Quote(fc.Settings[k]))
				}
				fmt.Fprintf(rt.Stdout, "    settings: %s\n", strings.Join(kv, ", "))
			}
		case configure.TypeHTTP:
			url, auth, policy := describeHTTP(fc)
			fmt.Fprintf(rt.Stdout, "  feather %s: http %s %s, auth %s\n", output.Shown(name), fc.HTTPMethod(), output.Shown(url), output.Shown(auth))
			if policy != "" {
				fmt.Fprintf(rt.Stdout, "    address policy: %s\n", policy)
			}
			for _, kind := range []struct {
				label string
				m     map[string]string
			}{{"header", fc.Headers}, {"query", fc.Query}} {
				for _, k := range sortedNames(kind.m) {
					fmt.Fprintf(rt.Stdout, "    %s %s: %q\n", kind.label, output.Shown(k), kind.m[k])
				}
			}
			if fc.Body != nil && fc.Body.Template != "" {
				fmt.Fprintf(rt.Stdout, "    body template: %q\n", fc.Body.Template)
			}
		case configure.TypeEmail:
			secret := "no password"
			if fc.Password != nil {
				secret = "password from " + output.Shown(fc.Password.Describe())
			}
			fmt.Fprintf(rt.Stdout, "  feather %s: email via %s, %s, to %s\n", output.Shown(name), output.Shown(fc.Host), secret, output.ShownList(fc.To))
		default:
			src := "(none)"
			if fc.WebhookURL != nil {
				src = output.Shown(fc.WebhookURL.Describe())
				if h := webhookHost(fc.WebhookURL); h != "" {
					// where it sends: the host only (the path and the
					// query of a webhook URL are its secret)
					src += fmt.Sprintf(", to host %q", h)
				}
			}
			fmt.Fprintf(rt.Stdout, "  feather %s: %s, webhook URL from %s\n", output.Shown(name), output.Shown(fc.Type), src)
		}
	}
	for _, name := range sortedNames(f.Groups) {
		fmt.Fprintf(rt.Stdout, "  group %s: %s\n", output.Shown(name), output.ShownList(f.Groups[name].Feathers))
	}
	for _, name := range sortedNames(f.Templates) {
		fmt.Fprintf(rt.Stdout, "  template %s\n", output.Shown(name))
	}
}

// webhookHost is the host (and port) of a literal webhook URL, for the
// trust to show where a project's feather sends; "" when the URL is not in
// the file or is not a URL.
func webhookHost(r *credential.Ref) string {
	if r == nil || r.Literal == nil {
		return ""
	}
	u, err := url.Parse(*r.Literal)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
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
		fmt.Fprintf(rt.Stdout, "untrusted %s\n", output.Shown(p.Project))
	} else {
		fmt.Fprintf(rt.Stdout, "%s was not trusted\n", output.Shown(p.Project))
	}
	return ExitOK
}
