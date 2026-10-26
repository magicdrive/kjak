package subcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/magicdrive/kjak/internal/commandline/option"
	"github.com/magicdrive/kjak/internal/configure"
	"github.com/magicdrive/kjak/internal/credential"
	"github.com/magicdrive/kjak/internal/output"
	"github.com/magicdrive/kjak/internal/textbank"
)

// Feather runs kjak feather <action>.
func Feather(ctx context.Context, rt *Runtime, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(rt.Stderr, textbank.FeatherHelp)
		return ExitUsage
	}
	action, rest := args[0], args[1:]
	switch action {
	case "list":
		return featherList(ctx, rt, rest)
	case "show":
		return featherShow(ctx, rt, rest)
	case "add":
		return featherAdd(ctx, rt, rest)
	case "remove":
		return featherRemove(ctx, rt, rest)
	case "enable", "disable":
		return featherEnable(ctx, rt, rest, action == "enable")
	case "types":
		return featherTypes(rt)
	case "plugins":
		return featherPlugins(ctx, rt, rest)
	case "-h", "--help", "help":
		return rt.help(textbank.FeatherHelp)
	}
	return rt.fail(ctx, fmt.Errorf("feather: unknown action %q (list, show, add, remove, enable, disable, types, plugins)\nRun 'kjak feather --help' for usage", action))
}

// featherView is how a feather is shown: credentials by their source only.
type featherView struct {
	Name          string   `json:"name"`
	Origin        string   `json:"origin"`
	Type          string   `json:"type"`
	Enabled       bool     `json:"enabled"`
	Levels        []string `json:"levels"`
	WebhookURL    string   `json:"webhook_url,omitempty"`
	Username      string   `json:"username,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          int      `json:"port,omitempty"`
	Security      string   `json:"security,omitempty"`
	User          string   `json:"user,omitempty"`
	Password      string   `json:"password,omitempty"`
	From          string   `json:"from,omitempty"`
	To            []string `json:"to,omitempty"`
	SubjectPrefix string   `json:"subject_prefix,omitempty"`
	// http
	URL    string `json:"url,omitempty"`
	Method string `json:"method,omitempty"`
	Auth   string `json:"auth,omitempty"`
	Policy string `json:"policy,omitempty"`
	// external
	Plugin  string   `json:"plugin,omitempty"`
	Path    string   `json:"path,omitempty"`
	SHA256  string   `json:"sha256,omitempty"`
	Secrets []string `json:"secrets,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// describeSecrets lists the secrets of a plugin by their source.
func describeSecrets(fc configure.FeatherConfig) []string {
	var out []string
	for _, k := range sortedNames(fc.PluginSecrets) {
		out = append(out, k+" from "+fc.PluginSecrets[k].Describe())
	}
	return out
}

// describeHTTP tells what an http feather sends where, with its secrets by
// their source only (a url_secret is not shown).
func describeHTTP(fc configure.FeatherConfig) (url, auth, policy string) {
	url = fc.URL
	if fc.URLSecret != nil {
		url = "from " + fc.URLSecret.Describe()
	}
	auth = "none"
	if a := fc.Auth; a != nil {
		switch a.Type {
		case configure.AuthBearer:
			auth = "bearer from " + a.Token.Describe()
		case configure.AuthBasic:
			auth = "basic " + a.Username + " from " + a.Password.Describe()
		case configure.AuthHeader:
			auth = "header " + a.Header + " from " + a.Value.Describe()
		}
	}
	var p []string
	if fc.AllowHTTP {
		p = append(p, "allows http")
	}
	if len(fc.AllowNetworks) > 0 {
		p = append(p, "allows networks "+strings.Join(fc.AllowNetworks, ","))
	}
	if len(fc.AllowHosts) > 0 {
		p = append(p, "allows private hosts "+strings.Join(fc.AllowHosts, ","))
	}
	if fc.MaxRedirects > 0 {
		p = append(p, fmt.Sprintf("follows %d redirect(s)", fc.MaxRedirects))
	}
	return url, auth, strings.Join(p, "; ")
}

func viewFeather(l *configure.Loaded, name string, fc configure.FeatherConfig) featherView {
	v := featherView{
		Name: name, Origin: l.Origin("feather", name), Type: fc.Type, Enabled: fc.IsEnabled(),
		Levels: fc.Levels, Username: fc.Username, Host: fc.Host, Port: fc.Port, Security: fc.Security,
		User: fc.User, From: fc.From, To: fc.To, SubjectPrefix: fc.SubjectPrefix,
	}
	if v.Levels == nil {
		v.Levels = []string{}
	}
	if fc.WebhookURL != nil {
		v.WebhookURL = fc.WebhookURL.Describe()
	}
	if fc.Password != nil {
		v.Password = fc.Password.Describe()
	}
	if fc.Type == configure.TypeHTTP {
		v.URL, v.Auth, v.Policy = describeHTTP(fc)
		v.Method = fc.HTTPMethod()
	}
	if fc.Type == configure.TypeExternal {
		v.Plugin, v.Path, v.SHA256, v.Env = fc.Plugin, fc.Path, fc.SHA256, fc.Env
		v.Secrets = describeSecrets(fc)
	}
	return v
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func featherList(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseList("feather list", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.FeatherHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	views := []featherView{}
	for _, name := range sortedNames(l.Effective.Feathers) {
		views = append(views, viewFeather(l, name, l.Effective.Feathers[name]))
	}
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, map[string]any{"schema_version": 1, "feathers": views})
		return ExitOK
	}
	if len(views) == 0 {
		fmt.Fprintln(rt.Stdout, "no feathers (add one with `kjak feather add`)")
		return ExitOK
	}
	tw := tabwriter.NewWriter(rt.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tTYPE\tENABLED\tLEVELS\tORIGIN")
	for _, v := range views {
		levels := "all"
		if len(v.Levels) > 0 {
			levels = strings.Join(v.Levels, ",")
		}
		fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%s\n", v.Name, v.Type, v.Enabled, levels, v.Origin)
	}
	_ = tw.Flush()
	return ExitOK
}

func featherShow(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseShow("feather show", args)
	rt.jsonOutput(opt.Output(), false)
	if code, ok := rt.parsed(ctx, err, textbank.FeatherHelp); !ok {
		return code
	}
	l, err := rt.load(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	fc, ok := l.Effective.Feathers[opt.Name]
	if !ok {
		return rt.fail(ctx, notFound("feather", opt.Name))
	}
	v := viewFeather(l, opt.Name, fc)
	if opt.Format == "json" {
		_ = writeJSON(rt.Stdout, v)
		return ExitOK
	}
	tw := tabwriter.NewWriter(rt.Stdout, 0, 4, 2, ' ', 0)
	row := func(k string, val any) {
		if s := fmt.Sprint(val); s != "" && s != "0" && s != "[]" {
			fmt.Fprintf(tw, "%s\t%s\n", k, s)
		}
	}
	row("name", v.Name)
	row("origin", v.Origin)
	row("type", v.Type)
	fmt.Fprintf(tw, "enabled\t%t\n", v.Enabled)
	if len(v.Levels) == 0 {
		row("levels", "all")
	} else {
		row("levels", strings.Join(v.Levels, ", "))
	}
	row("webhook_url", v.WebhookURL)
	row("username", v.Username)
	row("host", v.Host)
	row("port", v.Port)
	row("security", v.Security)
	row("user", v.User)
	row("password", v.Password)
	row("from", v.From)
	row("to", strings.Join(v.To, ", "))
	row("subject_prefix", v.SubjectPrefix)
	row("method", v.Method)
	row("url", v.URL)
	row("auth", v.Auth)
	row("policy", v.Policy)
	row("plugin", v.Plugin)
	row("path", v.Path)
	row("sha256", v.SHA256)
	row("secrets", strings.Join(v.Secrets, ", "))
	row("env", strings.Join(v.Env, ", "))
	_ = tw.Flush()
	return ExitOK
}

func featherAdd(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseFeatherAdd(args)
	if code, ok := rt.parsed(ctx, err, textbank.FeatherHelp); !ok {
		return code
	}
	if err := configure.ValidName(opt.Name); err != nil {
		return rt.fail(ctx, err)
	}
	if opt.Type == configure.TypeHTTP {
		return rt.fail(ctx, withCode(output.CodeUsage, fmt.Errorf("feather add: an http feather is configured in the file ([feathers.%s] type = \"http\", docs/http.md); then run kjak config validate", opt.Name)))
	}
	if opt.Type == configure.TypeExternal {
		return rt.fail(ctx, withCode(output.CodeUsage, fmt.Errorf("feather add: an external feather is configured in the file ([feathers.%s] type = \"external\", plugin = \"NAME\", docs/plugins.md); kjak feather plugins lists what PATH has", opt.Name)))
	}
	fc := configure.FeatherConfig{Type: opt.Type, Levels: opt.Levels}
	if opt.Disabled {
		f := false
		fc.Enabled = &f
	}
	var ref *credential.Ref
	switch {
	case opt.Secret.Env != "":
		v := opt.Secret.Env
		ref = &credential.Ref{Env: &v}
	case opt.Secret.Keychain != "":
		svc, acct, _ := strings.Cut(opt.Secret.Keychain, "/")
		ref = &credential.Ref{Keychain: &credential.KeychainRef{Service: svc, Account: acct}}
	case opt.Secret.Ref != "":
		v := opt.Secret.Ref
		ref = &credential.Ref{Secret: &v}
	case opt.Secret.Stdin:
		b, err := readLimited(rt.Stdin, "standard input")
		if err != nil {
			return rt.fail(ctx, err)
		}
		v := strings.TrimRight(string(b), "\r\n")
		ref = &credential.Ref{Literal: &v}
		fmt.Fprintln(rt.Stderr, "kjak: warning: --secret-stdin keeps the secret in the configuration file as a literal (deprecated); prefer a secrets file and --secret-ref NAME")
	}
	switch opt.Type {
	case configure.TypeSlack, configure.TypeDiscord:
		fc.WebhookURL = ref
		fc.Username = opt.Username
	case configure.TypeEmail:
		fc.Password = ref
		fc.Host, fc.Port, fc.Security, fc.User = opt.Host, opt.Port, opt.Security, opt.User
		fc.From, fc.To, fc.SubjectPrefix = opt.From, opt.MailTo, opt.SubjectPrefix
	}
	if err := configure.ValidateFeather(fc); err != nil {
		return rt.fail(ctx, fmt.Errorf("feather %q: %w", opt.Name, err))
	}
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	scope := configure.ScopeGlobal
	if opt.Scope != "" {
		scope = configure.Scope(opt.Scope)
	}
	err = configure.Update(p, scope, rt.Clock.Now(), func(f *configure.File) error {
		if _, exists := f.Feathers[opt.Name]; exists && !opt.Force {
			return fmt.Errorf("feather %q exists in the %s configuration (--force replaces it)", opt.Name, scope)
		}
		if _, isGroup := f.Groups[opt.Name]; isGroup {
			return fmt.Errorf("%q is a group", opt.Name)
		}
		if f.Feathers == nil {
			f.Feathers = map[string]configure.FeatherConfig{}
		}
		f.Feathers[opt.Name] = fc
		return nil
	})
	if err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, "added feather %q (%s) to the %s configuration\n", opt.Name, opt.Type, scope)
	return ExitOK
}

func featherRemove(ctx context.Context, rt *Runtime, args []string) int {
	opt, err := option.ParseScopedName("feather remove", args)
	if code, ok := rt.parsed(ctx, err, textbank.FeatherHelp); !ok {
		return code
	}
	return changeEntry(ctx, rt, opt, "feather", func(f *configure.File) error {
		if _, ok := f.Feathers[opt.Name]; !ok {
			return notFound("feather", opt.Name)
		}
		for _, g := range sortedNames(f.Groups) {
			for _, m := range f.Groups[g].Feathers {
				if m == opt.Name {
					return fmt.Errorf("feather %q is a member of group %q: change the group first", opt.Name, g)
				}
			}
		}
		delete(f.Feathers, opt.Name)
		return nil
	}, "removed feather %q from the %s configuration\n")
}

func featherEnable(ctx context.Context, rt *Runtime, args []string, enable bool) int {
	command := "feather disable"
	if enable {
		command = "feather enable"
	}
	opt, err := option.ParseScopedName(command, args)
	if code, ok := rt.parsed(ctx, err, textbank.FeatherHelp); !ok {
		return code
	}
	done := "disabled feather %q in the %s configuration\n"
	if enable {
		done = "enabled feather %q in the %s configuration\n"
	}
	return changeEntry(ctx, rt, opt, "feather", func(f *configure.File) error {
		fc, ok := f.Feathers[opt.Name]
		if !ok {
			return notFound("feather", opt.Name)
		}
		v := enable
		fc.Enabled = &v
		f.Feathers[opt.Name] = fc
		return nil
	}, done)
}

// changeEntry applies fn to the file that defines (or should define) a name.
func changeEntry(ctx context.Context, rt *Runtime, opt *option.ScopedNameOption, kind string, fn func(*configure.File) error, done string) int {
	p, err := rt.paths(opt.ConfigOption)
	if err != nil {
		return rt.fail(ctx, err)
	}
	var l *configure.Loaded
	if opt.Scope == "" {
		// the origin of the name; an untrusted project file is an error here
		// as in a run
		if l, err = configure.Load(p); err != nil {
			return rt.fail(ctx, err)
		}
	}
	scope, err := resolveScope(l, opt.Scope, kind, opt.Name)
	if err != nil {
		return rt.fail(ctx, err)
	}
	if err := configure.Update(p, scope, rt.Clock.Now(), fn); err != nil {
		return rt.fail(ctx, err)
	}
	fmt.Fprintf(rt.Stdout, done, opt.Name, scope)
	return ExitOK
}

func featherTypes(rt *Runtime) int {
	fmt.Fprintln(rt.Stdout, "slack    Slack incoming webhook (secret: webhook_url)")
	fmt.Fprintln(rt.Stdout, "discord  Discord webhook (secret: webhook_url)")
	fmt.Fprintln(rt.Stdout, "email    E-mail over SMTP (secret: password)")
	fmt.Fprintln(rt.Stdout, "http     Any HTTP API: method, headers, auth, body (secrets: url_secret, auth); configured in the file")
	fmt.Fprintln(rt.Stdout, "external A plugin, the executable kjak-feather-<plugin> on PATH (secrets: the ones it is given); configured in the file")
	fmt.Fprintln(rt.Stdout, "")
	fmt.Fprintln(rt.Stdout, "Not supported in v0.1.0: OS-native notifications (a future feather).")
	return ExitOK
}

func sortedNames[V any](m map[string]V) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
