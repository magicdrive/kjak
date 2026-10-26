//go:build !windows

package external

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// checkFile checks an executable: a regular file, executable, of root or
// the user, not writable by group or others (checkPlace checks where it
// is).
func checkFile(real string, st os.FileInfo) error {
	if !st.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	perm := st.Mode().Perm()
	if perm&0o111 == 0 {
		return errors.New("not executable")
	}
	if perm&0o022 != 0 {
		return fmt.Errorf("writable by group or others (mode %04o)", perm)
	}
	if uid, ok := ownedByRootOrUser(st); !ok {
		return fmt.Errorf("owned by uid %d, neither root nor the user running kjak", uid)
	}
	return nil
}

// openNoBlock opens a file for reading without waiting on a FIFO (the type
// is checked on the opened file).
func openNoBlock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}

// ownedByRootOrUser reports whether st belongs to root or the effective
// user.
func ownedByRootOrUser(st os.FileInfo) (uint32, bool) {
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, true
	}
	return sys.Uid, sys.Uid == 0 || int(sys.Uid) == os.Geteuid()
}

// checkPlace checks where a plugin is: the PATH entry itself when it is a
// symbolic link (its owner), the directory that holds the name and the
// directory of the file it leads to (owned by root or the user, not
// writable by group or others, sticky or not), and every directory above
// them (owned by root or the user; writable by group or others only when
// sticky). Someone else could otherwise put a link or a file there.
func checkPlace(path, real string) error {
	if st, err := os.Lstat(path); err == nil && st.Mode()&os.ModeSymlink != 0 {
		if uid, ok := ownedByRootOrUser(st); !ok {
			return fmt.Errorf("the link %s is owned by uid %d, neither root nor the user running kjak", path, uid)
		}
	}
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(real)} {
		st, err := os.Stat(dir)
		if err != nil {
			return err
		}
		if uid, ok := ownedByRootOrUser(st); !ok {
			return fmt.Errorf("its directory %s is owned by uid %d, neither root nor the user running kjak", dir, uid)
		}
		if st.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("its directory %s is writable by group or others", dir)
		}
		for d := filepath.Dir(dir); ; d = filepath.Dir(d) {
			st, err := os.Stat(d)
			if err != nil {
				return err
			}
			if uid, ok := ownedByRootOrUser(st); !ok {
				return fmt.Errorf("the directory %s above it is owned by uid %d, neither root nor the user running kjak", d, uid)
			}
			if st.Mode().Perm()&0o022 != 0 && st.Mode()&os.ModeSticky == 0 {
				return fmt.Errorf("the directory %s above it is writable by group or others", d)
			}
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	return nil
}

// setProcessGroup puts the plugin in a process group of its own, and makes
// a cancel (timeout, deadline, SIGINT) kill the whole group: a child the
// plugin started does not outlive it.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd) }
}

// killGroup kills the plugin's process group (after the plugin ended too:
// nothing it started stays behind).
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// GroupKill: the whole process group of a plugin is killed.
const GroupKill = true
