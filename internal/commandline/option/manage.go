package option

import (
	"strings"
)

// ListOption: kjak <noun> list.
type ListOption struct {
	ConfigOption
	OutputOption
}

// Output is the --output of the options (nil options: none).
func (o *ListOption) Output() *OutputOption {
	if o == nil {
		return nil
	}
	return &o.OutputOption
}

// Output is the --output of the options (nil options: none).
func (o *NameOption) Output() *OutputOption {
	if o == nil {
		return nil
	}
	return &o.OutputOption
}

func ParseList(command string, args []string) (*ListOption, error) {
	opt := &ListOption{}
	fs := newFlagSet(command)
	opt.ConfigOption.register(fs)
	opt.OutputOption.register(fs)
	pos, err := parse(command, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(command, pos, 0, "no argument"); err != nil {
		return opt, err
	}
	return opt, opt.check(command)
}

// NameOption: kjak <noun> show NAME (and other read-only commands of one
// entry).
type NameOption struct {
	ConfigOption
	OutputOption
	Name string
	Vars []string // template render: key=value
}

func ParseShow(command string, args []string) (*NameOption, error) {
	return parseName(command, args, false)
}

func ParseTemplateRender(args []string) (*NameOption, error) {
	return parseName("template render", args, true)
}

func parseName(command string, args []string, withVars bool) (*NameOption, error) {
	opt := &NameOption{}
	fs := newFlagSet(command)
	opt.ConfigOption.register(fs)
	opt.OutputOption.register(fs)
	var vars repeat
	if withVars {
		fs.Var(&vars, "var", "")
	}
	pos, err := parse(command, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(command, pos, 1, "one NAME"); err != nil {
		return opt, err
	}
	opt.Name, opt.Vars = pos[0], vars
	return opt, opt.check(command)
}

// ScopedNameOption: kjak <noun> remove|enable|disable NAME.
type ScopedNameOption struct {
	ConfigOption
	Name string
	// Scope is the file to change; "" means the file that defines Name.
	Scope string
}

func registerScope(fs interface {
	StringVar(*string, string, string, string)
}, scope *string) {
	fs.StringVar(scope, "scope", "", "")
	fs.StringVar(scope, "s", "", "")
}

func checkScope(command, scope string) error {
	if scope != "" && scope != "global" && scope != "project" {
		return usageErr(command, "unknown --scope %q (use global or project)", scope)
	}
	return nil
}

func ParseScopedName(command string, args []string) (*ScopedNameOption, error) {
	opt := &ScopedNameOption{}
	fs := newFlagSet(command)
	opt.register(fs)
	registerScope(fs, &opt.Scope)
	pos, err := parse(command, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(command, pos, 1, "one NAME"); err != nil {
		return opt, err
	}
	opt.Name = pos[0]
	return opt, checkScope(command, opt.Scope)
}

// SecretOption names the secret of a feather (webhook_url of slack and
// discord, password of email). The secret itself is never an argument:
// arguments are visible to other processes and kept in shell history.
type SecretOption struct {
	Env      string // --secret-env VAR
	Keychain string // --secret-keychain SERVICE/ACCOUNT
	Ref      string // --secret-ref NAME: an entry of a secrets file
	Stdin    bool   // --secret-stdin: a literal, read from standard input (deprecated)
}

// Count is the number of sources named.
func (s SecretOption) Count() int {
	n := 0
	for _, b := range []bool{s.Env != "", s.Keychain != "", s.Ref != "", s.Stdin} {
		if b {
			n++
		}
	}
	return n
}

// FeatherAddOption: kjak feather add NAME --type TYPE ...
type FeatherAddOption struct {
	ConfigOption
	Name     string
	Scope    string
	Force    bool
	Type     string
	Disabled bool
	Levels   []string
	Secret   SecretOption

	Username      string
	Host          string
	Port          int
	Security      string
	User          string
	From          string
	MailTo        []string
	SubjectPrefix string
}

func ParseFeatherAdd(args []string) (*FeatherAddOption, error) {
	const cmd = "feather add"
	opt := &FeatherAddOption{}
	fs := newFlagSet(cmd)
	opt.register(fs)
	registerScope(fs, &opt.Scope)
	var levels, mailTo list
	fs.BoolVar(&opt.Force, "force", false, "")
	fs.StringVar(&opt.Type, "type", "", "")
	fs.BoolVar(&opt.Disabled, "disabled", false, "")
	fs.Var(&levels, "levels", "")
	fs.StringVar(&opt.Secret.Env, "secret-env", "", "")
	fs.StringVar(&opt.Secret.Keychain, "secret-keychain", "", "")
	fs.StringVar(&opt.Secret.Ref, "secret-ref", "", "")
	fs.BoolVar(&opt.Secret.Stdin, "secret-stdin", false, "")
	fs.StringVar(&opt.Username, "username", "", "")
	fs.StringVar(&opt.Host, "host", "", "")
	fs.IntVar(&opt.Port, "port", 0, "")
	fs.StringVar(&opt.Security, "security", "", "")
	fs.StringVar(&opt.User, "user", "", "")
	fs.StringVar(&opt.From, "from", "", "")
	fs.Var(&mailTo, "mail-to", "")
	fs.StringVar(&opt.SubjectPrefix, "subject-prefix", "", "")
	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(cmd, pos, 1, "one NAME"); err != nil {
		return opt, err
	}
	opt.Name, opt.Levels, opt.MailTo = pos[0], levels, mailTo
	if opt.Type == "" {
		return nil, usageErr(cmd, "--type is required (slack, discord or email)")
	}
	if n := opt.Secret.Count(); n > 1 {
		return nil, usageErr(cmd, "--secret-env, --secret-keychain, --secret-ref and --secret-stdin exclude each other")
	}
	if opt.Secret.Keychain != "" {
		svc, acct, ok := strings.Cut(opt.Secret.Keychain, "/")
		if !ok || svc == "" || acct == "" {
			return nil, usageErr(cmd, "--secret-keychain takes SERVICE/ACCOUNT")
		}
	}
	return opt, checkScope(cmd, opt.Scope)
}

// GroupSetOption: kjak group set NAME FEATHER...
type GroupSetOption struct {
	ConfigOption
	Name     string
	Feathers []string
	Scope    string
}

func ParseGroupSet(args []string) (*GroupSetOption, error) {
	const cmd = "group set"
	opt := &GroupSetOption{}
	fs := newFlagSet(cmd)
	opt.register(fs)
	registerScope(fs, &opt.Scope)
	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	if len(pos) < 2 {
		return opt, usageErr(cmd, "expected NAME and at least one FEATHER")
	}
	opt.Name = pos[0]
	for _, p := range pos[1:] {
		var l list
		_ = l.Set(p)
		opt.Feathers = append(opt.Feathers, l...)
	}
	return opt, checkScope(cmd, opt.Scope)
}

// TemplateSetOption: kjak template set NAME --body ...
type TemplateSetOption struct {
	ConfigOption
	Name                    string
	Scope                   string
	Title, Body, Level, URL string
	BodyStdin               bool
}

func ParseTemplateSet(args []string) (*TemplateSetOption, error) {
	const cmd = "template set"
	opt := &TemplateSetOption{}
	fs := newFlagSet(cmd)
	opt.register(fs)
	registerScope(fs, &opt.Scope)
	fs.StringVar(&opt.Title, "title", "", "")
	fs.StringVar(&opt.Body, "body", "", "")
	fs.BoolVar(&opt.BodyStdin, "body-stdin", false, "")
	fs.StringVar(&opt.Level, "level", "", "")
	fs.StringVar(&opt.URL, "url", "", "")
	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(cmd, pos, 1, "one NAME"); err != nil {
		return opt, err
	}
	opt.Name = pos[0]
	if (opt.Body != "") == opt.BodyStdin {
		return nil, usageErr(cmd, "give exactly one of --body and --body-stdin")
	}
	return opt, checkScope(cmd, opt.Scope)
}

// ConfigInitOption: kjak config init.
type ConfigInitOption struct {
	ConfigOption
	Force bool
}

func ParseConfigInit(args []string) (*ConfigInitOption, error) {
	const cmd = "config init"
	opt := &ConfigInitOption{}
	fs := newFlagSet(cmd)
	opt.register(fs)
	fs.BoolVar(&opt.Force, "force", false, "")
	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	return opt, exactArgs(cmd, pos, 0, "no argument")
}

// ConfigTrustOption: kjak config trust / untrust.
type ConfigTrustOption struct {
	ConfigOption
	Yes bool
	// AllowUserSecrets (trust): with --yes, allow the project to read the
	// secrets it names from the personal secrets file (asked otherwise).
	AllowUserSecrets bool
}

func ParseConfigTrust(command string, args []string) (*ConfigTrustOption, error) {
	opt := &ConfigTrustOption{}
	fs := newFlagSet(command)
	opt.register(fs)
	fs.BoolVar(&opt.Yes, "yes", false, "")
	fs.BoolVar(&opt.Yes, "y", false, "")
	if command == "config trust" {
		fs.BoolVar(&opt.AllowUserSecrets, "allow-user-secrets", false, "")
	}
	pos, err := parse(command, fs, args)
	if err != nil {
		return nil, err
	}
	return opt, exactArgs(command, pos, 0, "no argument")
}

// ConfigValidateOption: kjak config validate.
type ConfigValidateOption struct {
	ConfigOption
	OutputOption
	// Strict: a literal secret is an error, not a warning.
	Strict bool
}

// Output is the --output of the options (nil options: none).
func (o *ConfigValidateOption) Output() *OutputOption {
	if o == nil {
		return nil
	}
	return &o.OutputOption
}

func ParseConfigValidate(args []string) (*ConfigValidateOption, error) {
	const cmd = "config validate"
	opt := &ConfigValidateOption{}
	fs := newFlagSet(cmd)
	opt.ConfigOption.register(fs)
	opt.OutputOption.register(fs)
	fs.BoolVar(&opt.Strict, "strict", false, "")
	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(cmd, pos, 0, "no argument"); err != nil {
		return opt, err
	}
	return opt, opt.check(cmd)
}

// ConfigMigrateOption: kjak config migrate-secrets.
type ConfigMigrateOption struct {
	ConfigOption
	Scope      string // global (default) or project
	SecretsOut string // the new secrets file (default: the scope's)
	ConfigOut  string // the new configuration file (default: <file>.migrated)
	DryRun     bool
}

func ParseConfigMigrate(args []string) (*ConfigMigrateOption, error) {
	const cmd = "config migrate-secrets"
	opt := &ConfigMigrateOption{}
	fs := newFlagSet(cmd)
	opt.register(fs)
	registerScope(fs, &opt.Scope)
	fs.StringVar(&opt.SecretsOut, "secrets-out", "", "")
	fs.StringVar(&opt.ConfigOut, "config-out", "", "")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "")
	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	if err := exactArgs(cmd, pos, 0, "no argument"); err != nil {
		return opt, err
	}
	return opt, checkScope(cmd, opt.Scope)
}
