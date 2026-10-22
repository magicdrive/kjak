package option

import (
	"strings"
	"time"

	"github.com/magicdrive/kjak/internal/configure"
)

// InputMode is where the message of a send comes from. Exactly one is used.
type InputMode string

const (
	InputArgs         InputMode = "args"          // kjak send BODY...
	InputStdin        InputMode = "stdin"         // --stdin
	InputJSON         InputMode = "json-input"    // --json-input FILE|-
	InputTemplate     InputMode = "template"      // --template NAME
	InputTemplateFile InputMode = "template-file" // --template-file FILE
)

// SendOption are the options of kjak send.
type SendOption struct {
	ConfigOption
	OutputOption

	Mode         InputMode
	Body         string // InputArgs: the arguments joined by spaces
	JSONInput    string // InputJSON: a file, "-" for standard input
	Template     string
	TemplateFile string
	Vars         []string // key=value

	Title, Level, URL string
	HasTitle          bool
	HasLevel          bool
	HasURL            bool

	Targets []string

	Timeout     time.Duration // 0: the configuration's
	MaxRetries  int           // -1: the configuration's
	Concurrency int           // 0: the configuration's

	DryRun bool
	Quiet  bool
}

// ParseSend parses the arguments of kjak send. Once the flags are parsed,
// an error comes with the options (their --output decides how the error is
// reported); a flag that cannot be parsed returns no options.
func ParseSend(args []string) (*SendOption, error) {
	const cmd = "send"
	opt := &SendOption{MaxRetries: -1}
	fs := newFlagSet(cmd)
	opt.ConfigOption.register(fs)
	opt.OutputOption.register(fs)

	var targets list
	var vars repeat
	var stdin bool
	fs.Var(&targets, "to", "")
	fs.Var(&targets, "t", "")
	fs.StringVar(&opt.Title, "title", "", "")
	fs.StringVar(&opt.Title, "T", "", "")
	fs.StringVar(&opt.Level, "level", "", "")
	fs.StringVar(&opt.Level, "l", "", "")
	fs.StringVar(&opt.URL, "url", "", "")
	fs.StringVar(&opt.URL, "u", "", "")
	fs.BoolVar(&stdin, "stdin", false, "")
	fs.StringVar(&opt.JSONInput, "json-input", "", "")
	fs.StringVar(&opt.Template, "template", "", "")
	fs.StringVar(&opt.TemplateFile, "template-file", "", "")
	fs.Var(&vars, "var", "")
	fs.DurationVar(&opt.Timeout, "timeout", 0, "")
	fs.IntVar(&opt.MaxRetries, "max-retries", -1, "")
	fs.IntVar(&opt.Concurrency, "concurrency", 0, "")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "")
	fs.BoolVar(&opt.Quiet, "quiet", false, "")
	fs.BoolVar(&opt.Quiet, "q", false, "")

	pos, err := parse(cmd, fs, args)
	if err != nil {
		return nil, err
	}
	given := set(fs)
	opt.Targets = targets
	opt.Vars = vars
	opt.HasTitle = given["title"] || given["T"]
	opt.HasLevel = given["level"] || given["l"]
	opt.HasURL = given["url"] || given["u"]

	// the five inputs exclude each other
	var modes []InputMode
	if len(pos) > 0 {
		modes = append(modes, InputArgs)
		opt.Body = strings.Join(pos, " ")
	}
	if stdin {
		modes = append(modes, InputStdin)
	}
	if given["json-input"] {
		modes = append(modes, InputJSON)
		if opt.JSONInput == "" {
			return opt, usageErr(cmd, "--json-input needs a file (or - for standard input)")
		}
	}
	if given["template"] {
		modes = append(modes, InputTemplate)
		if opt.Template == "" {
			return opt, usageErr(cmd, "--template needs a template name")
		}
	}
	if given["template-file"] {
		modes = append(modes, InputTemplateFile)
		if opt.TemplateFile == "" {
			return opt, usageErr(cmd, "--template-file needs a file")
		}
	}
	switch len(modes) {
	case 0:
		return opt, usageErr(cmd, "no message: give a body, or one of --stdin, --json-input, --template, --template-file")
	case 1:
		opt.Mode = modes[0]
	default:
		names := make([]string, len(modes))
		for i, m := range modes {
			if m == InputArgs {
				names[i] = "a body argument"
			} else {
				names[i] = "--" + string(m)
			}
		}
		return opt, usageErr(cmd, "%s exclude each other: give only one message input", strings.Join(names, " and "))
	}

	if len(opt.Vars) > 0 && opt.Mode != InputTemplate && opt.Mode != InputTemplateFile {
		return opt, usageErr(cmd, "--var applies to --template and --template-file only")
	}
	if opt.Mode == InputJSON && (opt.HasTitle || opt.HasLevel || opt.HasURL) {
		return opt, usageErr(cmd, "--title, --level and --url cannot be combined with --json-input (put them in the JSON)")
	}
	if opt.Mode == InputStdin && opt.JSONInput == "-" {
		return opt, usageErr(cmd, "standard input can be read once")
	}
	// the bounds of the configuration's [defaults]
	if given["timeout"] && (opt.Timeout <= 0 || opt.Timeout > configure.MaxTimeout) {
		return opt, usageErr(cmd, "--timeout must be between 0s (exclusive) and %s", configure.MaxTimeout)
	}
	if given["max-retries"] && (opt.MaxRetries < 0 || opt.MaxRetries > configure.MaxMaxRetries) {
		return opt, usageErr(cmd, "--max-retries must be between 0 and %d", configure.MaxMaxRetries)
	}
	if given["concurrency"] && (opt.Concurrency < 1 || opt.Concurrency > configure.MaxConcurrency) {
		return opt, usageErr(cmd, "--concurrency must be between 1 and %d", configure.MaxConcurrency)
	}
	if err := opt.OutputOption.check(cmd); err != nil {
		return opt, err
	}
	return opt, nil
}
