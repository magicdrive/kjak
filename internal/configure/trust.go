package configure

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// A project configuration can name credentials (env variables, keychain
// entries) and the URLs they are sent to, so a repository could make kjak
// send a secret anywhere. kjak therefore uses a project file only after the
// user trusted that exact file: its canonical path and the SHA-256 of its
// content. Any change of the content withdraws the trust.

// TrustEntry is one trusted project file.
type TrustEntry struct {
	Path      string    `toml:"path"`
	SHA256    string    `toml:"sha256"`
	TrustedAt time.Time `toml:"trusted_at"`

	// The project secrets file (./.kjak.secrets.toml) trusted with the
	// project file: its canonical path and the SHA-256 of its content
	// (empty: there was none). A project secrets file the entry does not
	// name, or a changed one, withdraws the trust of the project.
	SecretsPath   string `toml:"secrets_path,omitempty"`
	SecretsSHA256 string `toml:"secrets_sha256,omitempty"`
	// UserSecrets are the secret names the project configuration may read
	// from the personal secrets file (explicit or user), as the user
	// allowed them; any other name it refers to must be in the project
	// secrets file. They also hold the env and keychain references of its
	// feathers (GatedType), and the variables of its plugins, the
	// user allowed ("env:NAME", "keychain:S/A", GatedNames).
	UserSecrets []string `toml:"user_secrets,omitempty"`
	// Plugins are the executables of the project's external feathers as
	// they were when the project was trusted: one that is another file, or
	// changed, needs a new trust.
	Plugins []TrustedPlugin `toml:"plugins,omitempty"`
}

// TrustedPlugin is the executable of a plugin a trusted project runs.
type TrustedPlugin struct {
	Name   string `toml:"name"`
	Path   string `toml:"path"`
	SHA256 string `toml:"sha256"`
}

// Plugin returns the trusted executable of a plugin.
func (e TrustEntry) Plugin(name string) (TrustedPlugin, bool) {
	for _, p := range e.Plugins {
		if p.Name == name {
			return p, true
		}
	}
	return TrustedPlugin{}, false
}

// TrustGrant is what a trust covers besides the project file itself.
type TrustGrant struct {
	SecretsPath   string // canonical; "" when there is no project secrets file
	SecretsSHA256 string
	UserSecrets   []string
	Plugins       []TrustedPlugin
}

// AllowsUserSecret reports whether the project may read name from the
// personal secrets file.
func (e TrustEntry) AllowsUserSecret(name string) bool {
	for _, n := range e.UserSecrets {
		if n == name {
			return true
		}
	}
	return false
}

// TrustStore is the list of trusted project files.
type TrustStore struct {
	Projects []TrustEntry `toml:"projects"`
}

// CanonicalPath is the absolute path of p with every symbolic link resolved.
// A file that does not exist yet resolves through its directory.
func CanonicalPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(abs)), nil
}

// ReadTrustStore reads the store; a missing store is an empty one.
func ReadTrustStore(path string) (*TrustStore, error) {
	content, exists, err := readOptional(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return parseTrustStore(path, content, exists)
}

func parseTrustStore(path string, content []byte, exists bool) (*TrustStore, error) {
	ts := &TrustStore{}
	if !exists {
		return ts, nil
	}
	dec := toml.NewDecoder(bytes.NewReader(content))
	dec.DisallowUnknownFields()
	if err := dec.Decode(ts); err != nil {
		return nil, fmt.Errorf("%s: broken trust store: %w", path, describeTOMLError(err))
	}
	return ts, nil
}

// IsTrusted reports whether the file at path with content hash is trusted.
func (ts *TrustStore) IsTrusted(path, hash string) (bool, error) {
	canon, err := CanonicalPath(path)
	if err != nil {
		return false, err
	}
	for _, e := range ts.Projects {
		if e.Path == canon {
			return e.SHA256 == hash, nil
		}
	}
	return false, nil
}

// Entry returns the entry of path, if any.
func (ts *TrustStore) Entry(path string) (TrustEntry, bool) {
	canon, err := CanonicalPath(path)
	if err != nil {
		return TrustEntry{}, false
	}
	for _, e := range ts.Projects {
		if e.Path == canon {
			return e, true
		}
	}
	return TrustEntry{}, false
}

// Trust records the file at path with content hash as trusted (replacing an
// earlier entry of that path), with no project secrets file and no personal
// secret allowed.
func Trust(storePath, path, hash string, now time.Time) error {
	return TrustWith(storePath, path, hash, now, TrustGrant{})
}

// TrustWith records the file at path with content hash as trusted, with the
// project secrets file and the personal secrets of g.
func TrustWith(storePath, path, hash string, now time.Time, g TrustGrant) error {
	return trust(storePath, path, hash, now, func(TrustEntry, bool) TrustGrant { return g })
}

// retrust moves the trust of path to new content kjak itself wrote,
// keeping the grant of the earlier entry (only for names the new content
// still refers to: a new reference to a personal secret needs a new
// `kjak config trust`).
func retrust(storePath, path, hash string, now time.Time, refs []string) error {
	return trust(storePath, path, hash, now, func(old TrustEntry, ok bool) TrustGrant {
		if !ok {
			return TrustGrant{}
		}
		g := TrustGrant{SecretsPath: old.SecretsPath, SecretsSHA256: old.SecretsSHA256, Plugins: old.Plugins}
		for _, n := range old.UserSecrets {
			if slices.Contains(refs, n) {
				g.UserSecrets = append(g.UserSecrets, n)
			}
		}
		return g
	})
}

func trust(storePath, path, hash string, now time.Time, grant func(old TrustEntry, ok bool) TrustGrant) error {
	canon, err := CanonicalPath(path)
	if err != nil {
		return err
	}
	_, _, err = rewrite(storePath, Expect{}, func(current []byte) ([]byte, error) {
		ts, err := parseTrustStore(storePath, current, current != nil)
		if err != nil {
			return nil, err
		}
		kept := ts.Projects[:0]
		var old TrustEntry
		found := false
		for _, e := range ts.Projects {
			if e.Path != canon {
				kept = append(kept, e)
			} else {
				old, found = e, true
			}
		}
		g := grant(old, found)
		users := append([]string(nil), g.UserSecrets...)
		sort.Strings(users)
		ts.Projects = append(kept, TrustEntry{
			Path: canon, SHA256: hash, TrustedAt: now.UTC().Truncate(time.Second),
			SecretsPath: g.SecretsPath, SecretsSHA256: g.SecretsSHA256, UserSecrets: slices.Compact(users),
			Plugins: g.Plugins,
		})
		sort.Slice(ts.Projects, func(i, j int) bool { return ts.Projects[i].Path < ts.Projects[j].Path })
		return encodeTrustStore(ts)
	})
	return err
}

// Untrust removes the entry of path; it reports whether there was one.
func Untrust(storePath, path string) (bool, error) {
	canon, err := CanonicalPath(path)
	if err != nil {
		return false, err
	}
	removed := false
	_, _, err = rewrite(storePath, Expect{}, func(current []byte) ([]byte, error) {
		ts, err := parseTrustStore(storePath, current, current != nil)
		if err != nil {
			return nil, err
		}
		kept := ts.Projects[:0]
		for _, e := range ts.Projects {
			if e.Path == canon {
				removed = true
				continue
			}
			kept = append(kept, e)
		}
		ts.Projects = kept
		return encodeTrustStore(ts)
	})
	return removed, err
}

func encodeTrustStore(ts *TrustStore) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString("# Trusted kjak project configurations (written by `kjak config trust`).\n")
	buf.WriteString("# An entry trusts exactly this content: any change needs a new trust.\n\n")
	if err := toml.NewEncoder(&buf).Encode(ts); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
