package configure

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"

	"github.com/magicdrive/kjak/internal/credential"
)

// Secrets files keep secrets out of the configuration: a credential
// reference { secret = "slack.bot_token" } names an entry of one, which is a
// TOML document of string values in (nested) tables:
//
//	[slack]
//	bot_token = "xoxb-..."
//
// Two files are read, each on its own, never merged into one:
//   - the personal file: --secrets, else $KJAK_SECRETS (explicit), else
//     <ConfigDir>/secrets.toml (user). It serves the global configuration,
//     and the project configuration only for the names its trust allows
//     (TrustEntry.UserSecrets);
//   - the project file ./.kjak.secrets.toml (current). It serves the
//     project configuration only, and only as trusted with it
//     (TrustEntry.SecretsSHA256): a secrets file a repository brings along
//     can neither redirect a global feather nor change after its review.
//
// A secrets file must be a regular file of the user, readable and writable
// by the owner only (Unix), at most MaxConfigSize bytes; the project file
// must not be a symbolic link (a repository could link it to the personal
// file). The checks run on the opened file (fstat), not on its path.

const (
	// SecretsFileName is the personal secrets file in the configuration
	// directory.
	SecretsFileName = "secrets.toml"
	// ProjectSecretsFileName is the project secrets file, next to
	// .kjak.toml.
	ProjectSecretsFileName = ".kjak.secrets.toml"
)

// ErrSecretsFile wraps every error of a secrets file (it also wraps
// credential.ErrCredential: a send stops before anything is sent).
var ErrSecretsFile = errors.New("secrets file error")

// ErrInsecureSecrets: the file can be read or changed by someone else, is
// not the user's, is not a regular file, or is a refused symbolic link.
var ErrInsecureSecrets = errors.New("the secrets file is not private")

// SecretsFile is one secrets file as read.
type SecretsFile struct {
	Path   string
	Exists bool
	Hash   string // SHA-256 of the content read
	values map[string]string
}

// Names are the secret names the file defines, sorted.
func (s *SecretsFile) Names() []string {
	if s == nil {
		return nil
	}
	names := make([]string, 0, len(s.values))
	for n := range s.values {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Has reports whether the file defines name.
func (s *SecretsFile) Has(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.values[name]
	return ok
}

func secretsErr(path string, err error) error {
	return fmt.Errorf("%w: %w: %s: %w", credential.ErrCredential, ErrSecretsFile, path, err)
}

// ReadSecretsFile reads, checks and parses a secrets file. A file that does
// not exist is no error (Exists false). With noFollow, a symbolic link at
// path is refused (the project file); otherwise it is followed and the file
// it leads to is checked.
func ReadSecretsFile(path string, noFollow bool) (*SecretsFile, error) {
	sf := &SecretsFile{Path: path}
	f, err := openSecrets(path, noFollow)
	if errors.Is(err, fs.ErrNotExist) {
		return sf, nil
	}
	if err != nil {
		return nil, secretsErr(path, err)
	}
	defer f.Close()
	st, err := f.Stat() // the opened file, not the path: no check-then-use race
	if err != nil {
		return nil, secretsErr(path, err)
	}
	if err := checkSecretsInfo(path, st); err != nil {
		return nil, secretsErr(path, err)
	}
	content, err := io.ReadAll(io.LimitReader(f, MaxConfigSize+1))
	if err != nil {
		return nil, secretsErr(path, err)
	}
	if len(content) > MaxConfigSize {
		return nil, secretsErr(path, fmt.Errorf("the file is larger than %d bytes", MaxConfigSize))
	}
	values, err := ParseSecrets(content)
	if err != nil {
		return nil, secretsErr(path, err)
	}
	sf.Exists, sf.Hash, sf.values = true, Hash(content), values
	return sf, nil
}

// ParseSecrets decodes a secrets document into its names and values. Every
// leaf must be a string; every key a segment of a secret name. Its errors
// name keys and positions, never a value.
func ParseSecrets(content []byte) (map[string]string, error) {
	var doc map[string]any
	dec := toml.NewDecoder(bytes.NewReader(content))
	if err := dec.Decode(&doc); err != nil {
		var de *toml.DecodeError
		if errors.As(err, &de) {
			row, col := de.Position()
			return nil, fmt.Errorf("invalid TOML at line %d, column %d", row, col)
		}
		return nil, errors.New("invalid TOML")
	}
	out := map[string]string{}
	var walk func(prefix []string, m map[string]any) error
	walk = func(prefix []string, m map[string]any) error {
		for k, v := range m {
			path := append(append([]string(nil), prefix...), k)
			name := strings.Join(path, ".")
			if strings.Contains(k, ".") {
				return fmt.Errorf("key %q: a key cannot contain '.' (use a table: [a] b = ...)", name)
			}
			switch val := v.(type) {
			case string:
				if err := credential.ValidSecretName(name); err != nil {
					return err
				}
				out[name] = val
			case map[string]any:
				if len(path) >= credential.MaxSecretDepth {
					return fmt.Errorf("key %q: tables nest at most %d deep", name, credential.MaxSecretDepth)
				}
				if err := walk(path, val); err != nil {
					return err
				}
			default:
				return fmt.Errorf("key %q: a secret is a string", name)
			}
		}
		return nil
	}
	if err := walk(nil, doc); err != nil {
		return nil, err
	}
	return out, nil
}

// EncodeSecrets renders name -> value as a secrets document (tables by the
// segments of the names). Names must be valid and none a prefix of another.
func EncodeSecrets(values map[string]string) ([]byte, error) {
	root := map[string]any{}
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := credential.ValidSecretName(name); err != nil {
			return nil, err
		}
		parts := strings.Split(name, ".")
		m := root
		for _, p := range parts[:len(parts)-1] {
			next, ok := m[p]
			if !ok {
				child := map[string]any{}
				m[p] = child
				m = child
				continue
			}
			child, ok := next.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("secret %q: %q is a secret already", name, p)
			}
			m = child
		}
		last := parts[len(parts)-1]
		if _, taken := m[last]; taken {
			return nil, fmt.Errorf("secret %q is defined twice, or is a table", name)
		}
		m[last] = values[name]
	}
	var buf bytes.Buffer
	buf.WriteString("# kjak secrets: keep this file private (mode 0600) and out of repositories.\n\n")
	enc := toml.NewEncoder(&buf)
	enc.SetIndentTables(false)
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// CreateSecretsFile writes content to a new file at path, readable and
// writable by the owner only. An existing file (or link) is never
// overwritten.
func CreateSecretsFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return createExclusive(path, content)
}

// createExclusive creates path (O_EXCL: never an existing file, never
// through a link) with mode 0600, writes and syncs content. A failed write
// removes the file it created.
func createExclusive(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s exists: kjak does not overwrite it", path)
	}
	if err != nil {
		return err
	}
	_, werr := f.Write(content)
	if werr == nil {
		werr = syncFile(f)
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return werr
	}
	syncDir(filepath.Dir(path))
	return nil
}

// lazySecrets reads the personal secrets file at the first lookup (a run
// whose feathers refer to no secret never opens it).
type lazySecrets struct {
	path string
	once sync.Once
	file *SecretsFile
	err  error
}

func (l *lazySecrets) get() (*SecretsFile, error) {
	l.once.Do(func() { l.file, l.err = ReadSecretsFile(l.path, false) })
	return l.file, l.err
}

// secretScope is what one configuration file's secret references may read.
type secretScope struct {
	personal *lazySecrets
	// project: the references are the project configuration's; they read
	// the project secrets file, and the personal file for granted names.
	project   bool
	projFile  *SecretsFile
	trust     TrustEntry
	projPath  string
	persFrom  Selection
	persLabel string
}

func (s *secretScope) LookupSecret(_ context.Context, name string) (credential.Secret, error) {
	if err := credential.ValidSecretName(name); err != nil {
		return credential.Secret{}, fmt.Errorf("%w: %v", credential.ErrCredential, err)
	}
	if s.project {
		if v, ok := s.projFile.lookup(name); ok {
			return credential.NewSecret(v), nil
		}
		if !s.trust.AllowsUserSecret(name) {
			return credential.Secret{}, fmt.Errorf("%w: secret:%s (used by the project configuration) is not in the project secrets file %s, and the trust of the project does not allow it to be read from your %s secrets file: review the project, then run `kjak config trust`", credential.ErrCredential, name, s.projPath, s.persFrom.Kind())
		}
	}
	f, err := s.personal.get()
	if err != nil {
		return credential.Secret{}, err
	}
	if !f.Exists {
		return credential.Secret{}, fmt.Errorf("%w: secret:%s: the %s secrets file %s does not exist", credential.ErrCredential, name, s.persLabel, f.Path)
	}
	v, ok := f.lookup(name)
	if !ok {
		return credential.Secret{}, fmt.Errorf("%w: secret:%s is not in the %s secrets file %s", credential.ErrCredential, name, s.persLabel, f.Path)
	}
	return credential.NewSecret(v), nil
}

func (s *SecretsFile) lookup(name string) (string, bool) {
	if s == nil || !s.Exists {
		return "", false
	}
	v, ok := s.values[name]
	return v, ok
}

// SecretSource is where the secret references of a configuration file
// resolve: origin "project" (the project configuration) or anything else
// (the global configuration).
func (l *Loaded) SecretSource(origin string) credential.SecretSource {
	personal := l.personal
	if personal == nil {
		personal = &lazySecrets{path: l.Paths.Secrets}
	}
	s := &secretScope{personal: personal, persFrom: l.Paths.SecretsFrom, persLabel: l.Paths.SecretsFrom.Kind()}
	if origin == "project" && l.ProjectTrusted {
		s.project, s.projFile, s.trust, s.projPath = true, l.ProjectSecrets, l.ProjectTrust, l.Paths.ProjectSecrets
	}
	return s
}

// CheckProjectSecrets reads the project secrets file of a trusted project
// and checks that the trust covers it as it is: one the trust does not name,
// a changed one, or a missing one withdraws the trust.
func CheckProjectSecrets(p Paths, e TrustEntry) (*SecretsFile, error) {
	if p.ProjectSecrets == "" {
		return &SecretsFile{}, nil
	}
	sf, err := ReadSecretsFile(p.ProjectSecrets, true)
	if err != nil {
		return nil, err
	}
	untrusted := func(why string) error {
		return fmt.Errorf("%w: %s: %s\n  review it, then run `kjak config trust` (or pass --no-project to ignore the project)", ErrUntrustedProject, p.Project, why)
	}
	switch {
	case sf.Exists && e.SecretsSHA256 == "":
		return nil, untrusted("its secrets file " + p.ProjectSecrets + " is not covered by the trust")
	case sf.Exists && e.SecretsSHA256 != sf.Hash:
		return nil, untrusted("its secrets file " + p.ProjectSecrets + " changed since it was trusted")
	case !sf.Exists && e.SecretsSHA256 != "":
		return nil, untrusted("its secrets file " + p.ProjectSecrets + " was trusted with it and is gone")
	}
	if sf.Exists {
		canon, err := CanonicalPath(sf.Path)
		if err != nil {
			return nil, err
		}
		if canon != e.SecretsPath {
			return nil, untrusted("its secrets file is not the one that was trusted")
		}
	}
	return sf, nil
}
