package configure

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// ProjectFileName is the project configuration, read from the current
// directory only.
const ProjectFileName = ".kjak.toml"

// MaxConfigSize bounds a configuration file (and the trust store).
const MaxConfigSize = 1 << 20

// Paths are the files kjak reads.
type Paths struct {
	Global     string // the global configuration
	Project    string // the project configuration ("" when disabled)
	TrustStore string // the trusted project configurations

	// GlobalFrom tells how Global was chosen: FromFlag (--config), FromEnv
	// ($KJAK_CONFIG) or FromUser (the configuration directory).
	GlobalFrom Selection
	// Secrets is the personal secrets file: --secrets, else $KJAK_SECRETS,
	// else <ConfigDir>/secrets.toml (SecretsFrom tells which). It serves the
	// global configuration, and the project configuration only for the
	// names its trust allows.
	Secrets     string
	SecretsFrom Selection
	// ProjectSecrets is ./.kjak.secrets.toml ("" when the project is
	// disabled). It serves the project configuration only, and is part of
	// its trust.
	ProjectSecrets string
}

// Selection tells how a file was chosen.
type Selection string

const (
	FromFlag    Selection = "flag"    // explicit: a command-line option
	FromEnv     Selection = "env"     // explicit: an environment variable
	FromUser    Selection = "user"    // the user's configuration directory
	FromCurrent Selection = "current" // the current directory (project)
)

// Kind is the class of the selection: explicit, user or current.
func (s Selection) Kind() string {
	switch s {
	case FromFlag, FromEnv:
		return "explicit"
	}
	return string(s)
}

// Describe names the selection for a person: "explicit (--secrets)".
func (s Selection) Describe(flag, env string) string {
	switch s {
	case FromFlag:
		return "explicit (" + flag + ")"
	case FromEnv:
		return "explicit ($" + env + ")"
	}
	return string(s)
}

// SetExplicitSecrets makes path (--secrets) the personal secrets file.
func (p *Paths) SetExplicitSecrets(path string) {
	if path != "" {
		p.Secrets, p.SecretsFrom = path, FromFlag
	}
}

// Env is what DefaultPaths reads from the environment.
type Env struct {
	Getenv  func(string) string
	HomeDir func() (string, error)
	Getwd   func() (string, error)
	GOOS    string
}

// OSEnv is the environment of the process.
func OSEnv() Env {
	return Env{Getenv: os.Getenv, HomeDir: os.UserHomeDir, Getwd: os.Getwd, GOOS: runtime.GOOS}
}

// ConfigDir is kjak's configuration directory: $XDG_CONFIG_HOME/kjak, else
// ~/.config/kjak (also on macOS), else on Windows %AppData%\kjak.
func ConfigDir(env Env) (string, error) {
	if x := env.Getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "kjak"), nil
	}
	if env.GOOS == "windows" {
		if a := env.Getenv("AppData"); a != "" {
			return filepath.Join(a, "kjak"), nil
		}
	}
	home, err := env.HomeDir()
	if err != nil || home == "" {
		return "", errors.New("cannot find the configuration directory: set XDG_CONFIG_HOME or HOME")
	}
	return filepath.Join(home, ".config", "kjak"), nil
}

// DefaultPaths resolves the paths: the global file is configPath, else
// $KJAK_CONFIG, else <ConfigDir>/config.toml; the trust store is
// <ConfigDir>/trust.toml; the personal secrets file is $KJAK_SECRETS, else
// <ConfigDir>/secrets.toml (SetExplicitSecrets: --secrets); the project
// file and the project secrets file are ./.kjak.toml and
// ./.kjak.secrets.toml unless noProject.
func DefaultPaths(env Env, configPath string, noProject bool) (Paths, error) {
	dir, err := ConfigDir(env)
	if err != nil {
		return Paths{}, err
	}
	p := Paths{
		Global: filepath.Join(dir, "config.toml"), GlobalFrom: FromUser,
		TrustStore: filepath.Join(dir, "trust.toml"),
		Secrets:    filepath.Join(dir, SecretsFileName), SecretsFrom: FromUser,
	}
	if v := env.Getenv("KJAK_CONFIG"); v != "" {
		p.Global, p.GlobalFrom = v, FromEnv
	}
	if configPath != "" {
		p.Global, p.GlobalFrom = configPath, FromFlag
	}
	if v := env.Getenv("KJAK_SECRETS"); v != "" {
		p.Secrets, p.SecretsFrom = v, FromEnv
	}
	if !noProject {
		wd, err := env.Getwd()
		if err != nil {
			return Paths{}, fmt.Errorf("cannot find the current directory: %w", err)
		}
		p.Project = filepath.Join(wd, ProjectFileName)
		p.ProjectSecrets = filepath.Join(wd, ProjectSecretsFileName)
	}
	return p, nil
}

// Source is one configuration file as read.
type Source struct {
	Path    string
	Exists  bool
	Content []byte
	Hash    string // SHA-256 of Content
	File    *File  // nil when the file does not exist
}

// ReadFile reads, parses and validates one file. A file that does not exist
// is no error (Exists false). Unknown keys are errors: a typo is not ignored.
func ReadFile(path string) (*Source, error) {
	src := &Source{Path: path}
	content, exists, err := readOptional(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if !exists {
		return src, nil
	}
	src.Exists, src.Content, src.Hash = true, content, Hash(content)
	f, err := Parse(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	src.File = f
	return src, nil
}

// Parse decodes and validates a configuration document.
func Parse(content []byte) (*File, error) {
	var f File
	dec := toml.NewDecoder(bytes.NewReader(content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, describeTOMLError(err)
	}
	if err := Validate(&f); err != nil {
		return nil, err
	}
	return &f, nil
}

// displayKey is a TOML key path as it can be printed: a part that is not a
// bare key is quoted (with its control characters escaped).
func displayKey(parts []string) string {
	shown := make([]string, len(parts))
	for i, p := range parts {
		if bareKey.MatchString(p) {
			shown[i] = p
		} else {
			shown[i] = strconv.Quote(p)
		}
	}
	return strings.Join(shown, ".")
}

var bareKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func describeTOMLError(err error) error {
	var sm *toml.StrictMissingError
	if errors.As(err, &sm) {
		var keys []string
		for _, e := range sm.Errors {
			row, _ := e.Position()
			keys = append(keys, fmt.Sprintf("%s (line %d)", displayKey(e.Key()), row))
		}
		return fmt.Errorf("%w: unknown key(s): %s", ErrInvalidConfig, strings.Join(keys, ", "))
	}
	var de *toml.DecodeError
	if errors.As(err, &de) {
		row, col := de.Position()
		msg := de.Error()
		if strings.Contains(msg, "credential.Ref") {
			msg = `a credential is a table naming one source: { env = "VAR" }, { keychain = { service = "...", account = "..." } }, { secret = "name" } or { literal = "..." }`
		}
		return fmt.Errorf("%w: line %d, column %d: %s", ErrInvalidConfig, row, col, msg)
	}
	return fmt.Errorf("%w: %v", ErrInvalidConfig, err)
}

// Encode renders f as TOML (kjak's own layout: comments are not kept).
func Encode(f *File) ([]byte, error) {
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.SetIndentTables(false)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ErrUntrustedProject: a project configuration exists but is not trusted
// (or changed since it was trusted).
var ErrUntrustedProject = errors.New("the project configuration is not trusted")

// Loaded is the configuration of a run.
type Loaded struct {
	Paths     Paths
	Global    *Source
	Project   *Source // nil when disabled; Exists false when there is none
	Effective *File
	// ProjectTrusted: the project file exists and is trusted (and used).
	ProjectTrusted bool
	// ProjectTrust is the trust entry of the project file (when trusted).
	ProjectTrust TrustEntry
	// ProjectSecrets is the project secrets file as trusted (when the
	// project is trusted; Exists false when there is none).
	ProjectSecrets *SecretsFile

	personal *lazySecrets
}

// Origin tells which file defines a feather, group or template name.
func (l *Loaded) Origin(kind, name string) string {
	has := func(f *File) bool {
		if f == nil {
			return false
		}
		switch kind {
		case "feather":
			_, ok := f.Feathers[name]
			return ok
		case "group":
			_, ok := f.Groups[name]
			return ok
		default:
			_, ok := f.Templates[name]
			return ok
		}
	}
	if l.ProjectTrusted && has(l.Project.File) {
		return "project"
	}
	if has(l.Global.File) {
		return "global"
	}
	return ""
}

// Load reads the global file and, when it exists and is trusted, the project
// file, and merges them. An untrusted project file is an error
// (ErrUntrustedProject): it is never used silently, and never ignored
// silently either; --no-project skips it explicitly.
func Load(p Paths) (*Loaded, error) {
	l := &Loaded{Paths: p}
	var err error
	if l.Global, err = ReadFile(p.Global); err != nil {
		return nil, err
	}
	if p.Project != "" && !samePath(p.Project, p.Global) {
		if l.Project, err = ReadFile(p.Project); err != nil {
			return nil, err
		}
		if l.Project.Exists {
			ts, err := ReadTrustStore(p.TrustStore)
			if err != nil {
				return nil, err
			}
			ok, err := ts.IsTrusted(l.Project.Path, l.Project.Hash)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, fmt.Errorf("%w: %s\n  review it, then run `kjak config trust` (or pass --no-project to ignore it)", ErrUntrustedProject, l.Project.Path)
			}
			l.ProjectTrust, _ = ts.Entry(l.Project.Path)
			if l.ProjectSecrets, err = CheckProjectSecrets(p, l.ProjectTrust); err != nil {
				return nil, err
			}
			l.ProjectTrusted = true
		}
	}
	l.personal = &lazySecrets{path: p.Secrets}
	var project *File
	if l.ProjectTrusted {
		project = l.Project.File
	}
	l.Effective = Merge(l.Global.File, project)
	if err := ValidateReferences(l.Effective); err != nil {
		return nil, err
	}
	return l, nil
}

func samePath(a, b string) bool {
	ca, err1 := CanonicalPath(a)
	cb, err2 := CanonicalPath(b)
	return err1 == nil && err2 == nil && ca == cb
}
