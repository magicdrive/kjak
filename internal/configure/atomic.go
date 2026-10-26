package configure

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

var (
	// ErrLocked: another kjak is changing the file (its lock file exists).
	ErrLocked = errors.New("the configuration is being changed by another kjak process")
	// ErrConcurrentModification: the file changed while kjak was changing it,
	// or since the content kjak was told to expect.
	ErrConcurrentModification = errors.New("the configuration file was modified concurrently")
)

// Hash is the SHA-256 of content, in hex.
func Hash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// beforeCommit runs just before the new content replaces the old one. Tests
// set it to change the file concurrently.
var beforeCommit func(path string)

// syncFile syncs the temporary file. Tests replace it to fail (a full disk).
var syncFile = func(f *os.File) error { return f.Sync() }

// Expect is the content an update requires the file to have.
type Expect struct {
	Check  bool   // false: any content
	Exists bool   // the file must exist (else: must not)
	Hash   string // with Exists: the SHA-256 of its content
}

// rewrite replaces the file at path with fn(current content) atomically:
//  1. it takes path+".lock" (created exclusively; ErrLocked if it exists);
//  2. it reads the file (nil content: no file) and checks expect;
//  3. it writes fn's content to a temporary file in the same directory,
//     syncs it and checks that the file did not change since step 2
//     (ErrConcurrentModification: an editor that ignores the lock);
//  4. it renames the temporary file over path and syncs the directory.
//
// The new content keeps the old file's mode (never group- or
// other-writable; 0600 for a new file), and a symbolic link at path stays:
// its target is written. It returns the hashes of the old and the new
// content ("" for no file).
func rewrite(path string, expect Expect, fn func(current []byte) ([]byte, error)) (oldHash, newHash string, err error) {
	if path, err = writeTarget(path); err != nil {
		return "", "", err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	unlock, err := lockFile(path)
	if err != nil {
		return "", "", err
	}
	defer unlock()

	current, existed, err := readOptional(path)
	if err != nil {
		return "", "", err
	}
	if existed {
		oldHash = Hash(current)
	}
	if expect.Check {
		if expect.Exists != existed || (existed && expect.Hash != oldHash) {
			return oldHash, "", fmt.Errorf("%w: %s", ErrConcurrentModification, path)
		}
	}
	next, err := fn(current)
	if err != nil {
		return oldHash, "", err
	}

	tmp, err := os.CreateTemp(dir, ".kjak-*.tmp")
	if err != nil {
		return oldHash, "", err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(fileMode(path)); err != nil && runtime.GOOS != "windows" {
		_ = tmp.Close()
		return oldHash, "", err
	}
	if _, err := tmp.Write(next); err != nil {
		_ = tmp.Close()
		return oldHash, "", err
	}
	if err := syncFile(tmp); err != nil {
		_ = tmp.Close()
		return oldHash, "", err
	}
	if err := tmp.Close(); err != nil {
		return oldHash, "", err
	}

	if beforeCommit != nil {
		beforeCommit(path)
	}
	again, stillExists, err := readOptional(path)
	if err != nil {
		return oldHash, "", err
	}
	if stillExists != existed || !bytes.Equal(again, current) {
		return oldHash, "", fmt.Errorf("%w: %s", ErrConcurrentModification, path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return oldHash, "", err
	}
	committed = true
	syncDir(dir)
	return oldHash, Hash(next), nil
}

// readOptional reads a file of at most MaxConfigSize bytes (nil, false: no
// file).
func readOptional(path string) ([]byte, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, MaxConfigSize+1))
	if err != nil {
		return nil, false, err
	}
	if len(b) > MaxConfigSize {
		return nil, false, fmt.Errorf("the file is larger than %d bytes", MaxConfigSize)
	}
	return b, true, nil
}

// writeTarget is the file a write of path changes: path itself, or the file
// a symbolic link at path points to (a dotfiles link stays a link). A link
// to nothing is refused: kjak does not create files where a link points.
func writeTarget(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&fs.ModeSymlink == 0 {
		return path, nil
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("%s is a symbolic link to a file that does not exist", path)
	}
	return target, nil
}

// fileMode is the mode of the new content: the old file's, never writable by
// group or others; 0600 for a new file.
func fileMode(path string) os.FileMode {
	if st, err := os.Stat(path); err == nil {
		return st.Mode().Perm() &^ 0o022
	}
	return 0o600
}

// lockFile takes the lock of path: an exclusive OS lock (flock, LockFileEx)
// on path+".lock". The lock file stays (removing it would let two writers
// lock two files); the lock itself ends with its process, so a kjak that
// crashed blocks nobody.
func lockFile(path string) (func(), error) {
	lock := path + ".lock"
	f, err := openLock(lock)
	if err != nil {
		return nil, fmt.Errorf("cannot open the lock file %s: %w", lock, err)
	}
	held, err := tryLock(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot lock %s: %w", lock, err)
	}
	if held {
		_ = f.Close()
		return nil, fmt.Errorf("%w (lock file %s)", ErrLocked, lock)
	}
	return func() { _ = f.Close() }, nil
}

func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return // a directory cannot be opened for sync there
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// WriteNew writes content to path atomically. Unless force, the file must
// not exist yet.
func WriteNew(path string, content []byte, force bool) error {
	expect := Expect{Check: !force, Exists: false}
	_, _, err := rewrite(path, expect, func([]byte) ([]byte, error) { return content, nil })
	if errors.Is(err, ErrConcurrentModification) && !force {
		return fmt.Errorf("%s exists (--force replaces it)", path)
	}
	return err
}
