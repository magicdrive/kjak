package configure

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
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
// earlier entry of that path).
func Trust(storePath, path, hash string, now time.Time) error {
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
		for _, e := range ts.Projects {
			if e.Path != canon {
				kept = append(kept, e)
			}
		}
		ts.Projects = append(kept, TrustEntry{Path: canon, SHA256: hash, TrustedAt: now.UTC().Truncate(time.Second)})
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
