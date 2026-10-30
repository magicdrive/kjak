package configure

import (
	"fmt"
	"time"
)

// Scope is the file a change goes to.
type Scope string

const (
	ScopeGlobal  Scope = "global"
	ScopeProject Scope = "project"
)

func ParseScope(s string) (Scope, error) {
	switch Scope(s) {
	case ScopeGlobal, ScopeProject:
		return Scope(s), nil
	}
	return "", fmt.Errorf("unknown scope %q (use global or project)", s)
}

// Update changes the file of scope with fn and writes it atomically (see
// rewrite). The result must be valid on its own and, merged with the other
// file, valid as a whole.
//
// A project file is changed only when it is trusted (or does not exist yet):
// kjak does not edit content the user has not reviewed. The trust then moves
// to the new content, which kjak itself wrote.
func Update(p Paths, scope Scope, now time.Time, fn func(*File) error) error {
	_, err := UpdateFile(p, scope, now, fn)
	return err
}

// UpdateResult is how UpdateFile wrote the file.
type UpdateResult struct {
	Path string
	// CommentsDropped: the file had comments and was rewritten without
	// them. A change that only adds feathers, groups or templates keeps the
	// file as it was and appends them (appendOnly).
	CommentsDropped bool
}

// UpdateFile is Update, and tells whether the comments of the file were
// kept.
func UpdateFile(p Paths, scope Scope, now time.Time, fn func(*File) error) (UpdateResult, error) {
	res, err := update(p, scope, now, fn)
	return res, err
}

func update(p Paths, scope Scope, now time.Time, fn func(*File) error) (UpdateResult, error) {
	var res UpdateResult
	path := p.Global
	expect := Expect{}
	if scope == ScopeProject {
		if p.Project == "" {
			return res, fmt.Errorf("the project configuration is disabled (--no-project)")
		}
		path = p.Project
		src, err := ReadFile(path)
		if err != nil {
			return res, err
		}
		if src.Exists {
			ts, err := ReadTrustStore(p.TrustStore)
			if err != nil {
				return res, err
			}
			ok, err := ts.IsTrusted(path, src.Hash)
			if err != nil {
				return res, err
			}
			if !ok {
				return res, fmt.Errorf("%w: %s (kjak edits a project file only once it is trusted: `kjak config trust`)", ErrUntrustedProject, path)
			}
			expect = Expect{Check: true, Exists: true, Hash: src.Hash}
		} else {
			expect = Expect{Check: true, Exists: false}
		}
	}

	var written *File
	_, newHash, err := rewrite(path, expect, func(current []byte) ([]byte, error) {
		f := &File{}
		if current != nil {
			parsed, err := Parse(current)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", path, err)
			}
			f = parsed
		}
		if err := fn(f); err != nil {
			return nil, err
		}
		if err := Validate(f); err != nil {
			return nil, err
		}
		if err := validateWithOther(p, scope, f); err != nil {
			return nil, err
		}
		written = f
		encoded, err := Encode(f)
		if err != nil || current == nil {
			return encoded, err
		}
		if kept, ok := appendOnly(current, f); ok {
			return kept, nil
		}
		res.CommentsDropped = hasComments(current)
		return encoded, nil
	})
	res.Path = path
	if err != nil {
		return res, err
	}
	if scope == ScopeProject {
		return res, retrust(p.TrustStore, path, newHash, now, append(SecretNames(written), GatedNames(written)...))
	}
	return res, nil
}

// validateWithOther checks the references of f merged with the other file
// (as a run would merge them; an untrusted project file is left out).
func validateWithOther(p Paths, scope Scope, f *File) error {
	var global, project *File
	if scope == ScopeGlobal {
		global = f
		if p.Project != "" {
			if src, err := ReadFile(p.Project); err == nil && src.Exists {
				if ts, err := ReadTrustStore(p.TrustStore); err == nil {
					if ok, _ := ts.IsTrusted(src.Path, src.Hash); ok {
						project = src.File
					}
				}
			}
		}
	} else {
		project = f
		src, err := ReadFile(p.Global)
		if err != nil {
			return err
		}
		global = src.File
	}
	return ValidateReferences(Merge(global, project))
}
