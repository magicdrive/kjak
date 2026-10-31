package external

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/magicdrive/kjak/internal/configure"
)

// Discovery and authorization are separate: kjak finds kjak-feather-*
// executables on PATH (Discover, `kjak feather plugins`) but runs one only
// for a feather the configuration registers (type = "external",
// plugin = NAME), and never by being found.
//
// The search (Resolve):
//   - only absolute PATH elements are searched; an empty element (the
//     current directory) and a relative one ("bin", ".") are skipped: a
//     repository's directory cannot provide a plugin by being the current
//     one;
//   - a name found in several directories as different files is an error,
//     unless the configuration pins the path: which one would run is not
//     left to the order of PATH;
//   - the file (symbolic links resolved) must be a regular, executable file
//     of root or the user, not writable by group or others, in a directory
//     others cannot write to (unless sticky); the ACLs too (internal/fileacl:
//     the POSIX ACL, the macOS ACL, the Windows owner and DACL);
//   - its identity (device, inode, size, modification time) and SHA-256 are
//     recorded, and checked again just before each run;
//   - on macOS, a plugin installed by Homebrew, whose directories the admin
//     group can write to, runs only when its content is what the user
//     stated (ResolveAccept, homebrew.go).

// ErrPlugin wraps every error of finding or checking a plugin.
var ErrPlugin = errors.New("plugin error")

// MaxExecutableSize bounds the file hashed.
const MaxExecutableSize = 512 << 20

// Executable is a checked plugin executable.
type Executable struct {
	Name   string
	Path   string // where it was found (a PATH directory), or the pinned path
	Real   string // symbolic links resolved: what runs
	SHA256 string
	// Homebrew: it passed the place checks only through the Homebrew
	// exception, its content accepted (homebrew.go). Verify then hashes it
	// and checks its place again before every run.
	Homebrew bool
	id       identity
}

// SkippedDir is a PATH element that is not searched.
type SkippedDir struct {
	Element string
	Why     string
}

// searchDirs splits a PATH value into the directories searched and those
// skipped.
func searchDirs(pathList string) (dirs []string, skipped []SkippedDir) {
	for _, e := range filepath.SplitList(pathList) {
		switch {
		case e == "":
			skipped = append(skipped, SkippedDir{e, "an empty element (the current directory)"})
		case !filepath.IsAbs(e):
			skipped = append(skipped, SkippedDir{e, "a relative directory"})
		default:
			dirs = append(dirs, filepath.Clean(e))
		}
	}
	return dirs, skipped
}

// Resolve finds and checks the executable of plugin name: the pinned path,
// else the PATH search. The strict checks only: a plugin in Homebrew's
// admin-writable directories is refused.
func Resolve(name, pinned, pathList, goos string) (*Executable, error) {
	return ResolveAccept(name, pinned, pathList, goos, nil)
}

// ResolveAccept is Resolve, where a candidate the strict checks refuse only
// because it is in Homebrew's admin-writable directories (macOS) is
// accepted when accept accepts its resolved path and SHA-256. A nil accept
// is Resolve.
func ResolveAccept(name, pinned, pathList, goos string, accept Accept) (*Executable, error) {
	if err := configure.ValidPluginName(name); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPlugin, err)
	}
	file := configure.PluginFile(name, goos)
	if pinned != "" {
		if !filepath.IsAbs(pinned) {
			return nil, fmt.Errorf("%w: path %s is not absolute", ErrPlugin, pinned)
		}
		// a missing file is reported by check, as "does not exist"
		if _, err := os.Lstat(pinned); err == nil {
			if err := exactEntry(filepath.Dir(pinned), filepath.Base(pinned)); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, pinned, err)
			}
		}
		return check(name, pinned, accept)
	}
	dirs, _ := searchDirs(pathList)
	var found []*Executable
	var firstErr error
	for _, d := range dirs {
		p := filepath.Join(d, file)
		if _, err := os.Lstat(p); err != nil {
			continue
		}
		// on a file system that ignores case (macOS, Windows) Lstat also
		// finds kjak-feather-Name, a name that is refused: only the exact
		// name is a candidate
		if err := exactEntry(d, file); err != nil {
			if firstErr == nil && !errors.Is(err, errOtherCase) {
				firstErr = fmt.Errorf("%w: %s: %v", ErrPlugin, p, err)
			}
			continue
		}
		exe, err := check(name, p, accept)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		dup := false
		for _, f := range found {
			if f.id.same(exe.id) {
				dup = true
			}
		}
		if !dup {
			found = append(found, exe)
		}
	}
	switch {
	case len(found) == 1 && firstErr == nil:
		return found[0], nil
	case len(found) > 1:
		var where []string
		for _, f := range found {
			where = append(where, f.Path)
		}
		return nil, fmt.Errorf("%w: %s is found in several PATH directories as different files (%s): set path = to the one to run", ErrPlugin, file, strings.Join(where, ", "))
	case firstErr != nil:
		// a refused candidate is never skipped in favor of a later one:
		// what PATH would run first is what was refused
		return nil, firstErr
	}
	return nil, fmt.Errorf("%w: %s is not on PATH (absolute directories only)", ErrPlugin, file)
}

// errOtherCase: the file system found the name, the directory spells it
// otherwise.
var errOtherCase = errors.New("no file of this exact name (the file system ignores case)")

// exactEntry checks that directory dir has an entry spelled exactly name.
func exactEntry(dir, name string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot list its directory to check the exact name: %v", err)
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return fmt.Errorf("cannot list its directory to check the exact name: %v", err)
	}
	if !slices.Contains(names, name) {
		return errOtherCase
	}
	return nil
}

// check checks one candidate and records its identity and digest. The
// strict place checks come first; only when they refuse it, and accept is
// set, are they run again with the Homebrew exception, and the content must
// then be accepted.
func check(name, path string, accept Accept) (*Executable, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s does not exist", ErrPlugin, path)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
	}
	var hb *relaxed
	if err := checkPlace(path, real, nil); err != nil {
		hb = &relaxed{}
		if accept == nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
		}
		rerr := checkPlace(path, real, hb)
		switch {
		case len(hb.used) == 0:
			// not Homebrew's layout: the strict refusal, as before
			return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
		case rerr != nil:
			// Homebrew's layout, refused for another reason (an ACL, a keg
			// directory its group can write to): that reason
			return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, rerr)
		}
	}
	f, err := openNoBlock(real)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
	}
	if err := checkFile(f, real, st); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
	}
	sum, err := digest(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
	}
	exe := &Executable{Name: name, Path: path, Real: real, SHA256: sum, id: identityOf(st)}
	if hb != nil {
		// the keg's file is the plugin's own name, and a program that loads
		// nothing the admin group could replace (homebrew.go)
		if base := filepath.Base(real); base != configure.PluginFile(name, runtime.GOOS) {
			return nil, fmt.Errorf("%w: %s: %s in Homebrew's directories is not named %s", ErrPlugin, path, real, configure.PluginFile(name, runtime.GOOS))
		}
		if err := selfContained(f); err != nil {
			return nil, fmt.Errorf("%w: %s: %s in Homebrew's directories %v", ErrPlugin, path, real, err)
		}
		if err := accept(real, sum); err != nil {
			return nil, &HomebrewError{Path: path, Real: real, SHA256: sum, Dirs: hb.used, Reason: err}
		}
		exe.Homebrew = true
	}
	return exe, nil
}

func digest(r io.Reader) (string, error) {
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, MaxExecutableSize+1))
	if err != nil {
		return "", err
	}
	if n > MaxExecutableSize {
		return "", fmt.Errorf("larger than %d bytes", MaxExecutableSize)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Verify checks, just before a run, that the file is still the one that was
// checked (same identity; same digest when pinned). The file can still be
// replaced between this check and the exec: docs/plugins.md.
func (e *Executable) Verify(rehash bool) error {
	if e.Homebrew {
		// in directories the admin group can write to: the content and the
		// place are checked again, whatever the caller asks
		rehash = true
		real, err := filepath.EvalSymlinks(e.Path)
		if err != nil || real != e.Real {
			return fmt.Errorf("%w: %s no longer leads to %s", ErrPlugin, e.Path, e.Real)
		}
		if err := checkPlace(e.Path, e.Real, &relaxed{}); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrPlugin, e.Path, err)
		}
	}
	f, err := openNoBlock(e.Real)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrPlugin, e.Real, err)
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrPlugin, e.Real, err)
	}
	if !identityOf(st).same(e.id) || st.Size() != e.id.size || !st.ModTime().Equal(e.id.mtime) {
		return fmt.Errorf("%w: %s changed since it was checked", ErrPlugin, e.Real)
	}
	if err := checkFile(f, e.Real, st); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrPlugin, e.Real, err)
	}
	if rehash {
		sum, err := digest(f)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrPlugin, e.Real, err)
		}
		if sum != e.SHA256 {
			return fmt.Errorf("%w: %s changed since it was checked (SHA-256)", ErrPlugin, e.Real)
		}
	}
	return nil
}

// Found is one kjak-feather-* file of a PATH directory, as Discover saw it.
type Found struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256,omitempty"`
	Problem  string `json:"problem,omitempty"`  // why it would be refused
	Shadowed bool   `json:"shadowed,omitempty"` // a directory before it has the same name
	// Homebrew: in Homebrew's admin-writable directories (macOS); it runs
	// only for a feather that pins SHA256 (or a project trust that records
	// it). Problem says so; Discover never accepts it.
	Homebrew bool `json:"homebrew,omitempty"`
}

// discoverAccept accepts nothing: Discover knows no configuration, so a
// plugin in Homebrew's directories is reported with the SHA-256 to pin.
func discoverAccept(real, sum string) error { return errNotAccepted }

// Discover lists the plugins of the PATH directories, without running any.
func Discover(pathList, goos string) (found []Found, skipped []SkippedDir) {
	dirs, skipped := searchDirs(pathList)
	seen := map[string]bool{}
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		var names []string
		for _, en := range entries {
			if strings.HasPrefix(en.Name(), configure.PluginPrefix) {
				names = append(names, en.Name())
			}
		}
		sort.Strings(names)
		for _, file := range names {
			name := strings.TrimPrefix(file, configure.PluginPrefix)
			if goos == "windows" {
				if !strings.HasSuffix(strings.ToLower(name), ".exe") {
					continue
				}
				name = name[:len(name)-4]
			}
			fd := Found{Name: name, Path: filepath.Join(d, file)}
			if err := configure.ValidPluginName(name); err != nil {
				fd.Problem = err.Error()
			} else if exe, err := check(name, fd.Path, discoverAccept); err != nil {
				fd.Problem = strings.TrimPrefix(err.Error(), ErrPlugin.Error()+": ")
				var he *HomebrewError
				if errors.As(err, &he) {
					fd.Homebrew, fd.SHA256 = true, he.SHA256
				}
			} else {
				fd.SHA256 = exe.SHA256
			}
			key := strings.ToLower(name)
			fd.Shadowed = seen[key]
			seen[key] = true
			found = append(found, fd)
		}
	}
	return found, skipped
}
