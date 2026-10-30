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
//     recorded, and checked again just before each run.

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
	id     identity
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
// else the PATH search.
func Resolve(name, pinned, pathList, goos string) (*Executable, error) {
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
		return check(name, pinned)
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
		exe, err := check(name, p)
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

// check checks one candidate and records its identity and digest.
func check(name, path string) (*Executable, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s does not exist", ErrPlugin, path)
		}
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
	}
	if err := checkPlace(path, real); err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrPlugin, path, err)
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
	return &Executable{Name: name, Path: path, Real: real, SHA256: sum, id: identityOf(st)}, nil
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
}

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
			} else if exe, err := check(name, fd.Path); err != nil {
				fd.Problem = strings.TrimPrefix(err.Error(), ErrPlugin.Error()+": ")
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
