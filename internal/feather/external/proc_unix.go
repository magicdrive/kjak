//go:build !windows

package external

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/magicdrive/kjak/internal/fileacl"
)

// checkFile checks an executable: a regular file, executable, of root or
// the user, not writable by group or others, by the mode bits or the ACL
// (checkPlace checks where it is).
func checkFile(f *os.File, real string, st os.FileInfo) error {
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
	return fileacl.CheckFile(f, fileacl.NoWrite)
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
// sticky). The ACLs are checked the same way (fileacl: NoWrite for the
// directories that hold it, NoReplace above them). Someone else could
// otherwise put a link or a file there.
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
		if err := fileacl.CheckPath(dir, fileacl.NoWrite); err != nil {
			return fmt.Errorf("its directory %s: %v", dir, err)
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
			if err := fileacl.CheckPath(d, fileacl.NoReplace); err != nil {
				return fmt.Errorf("the directory %s above it: %v", d, err)
			}
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	return nil
}

// containment is the process group of a plugin: the plugin is started in
// a group of its own (Setpgid, made by the child before the exec: no race),
// and a cancel (timeout, deadline, SIGINT) or the end of the run kills the
// whole group with SIGKILL. What it does not cover (docs/security.md): a
// descendant that leaves the group (setsid, setpgid) or is started by
// another service (launchd, systemd --user) outlives the plugin; after the
// leader was reaped, an empty group's id could in principle be reused
// before the final kill (pids are allocated in sequence: not seen in
// practice).
type containment struct {
	cmd *exec.Cmd
}

// newContainment prepares cmd to start in a group of its own.
func newContainment(cmd *exec.Cmd) (*containment, error) {
	c := &containment{cmd: cmd}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = c.kill
	return c, nil
}

// start starts the plugin.
func (c *containment) start() error { return c.cmd.Start() }

// kill kills the plugin's process group.
func (c *containment) kill() error {
	if c.cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-c.cmd.Process.Pid, syscall.SIGKILL)
}

// close kills what is left of the group once the plugin ended: nothing it
// started stays behind.
func (c *containment) close() { _ = c.kill() }

// GroupKill: the whole process group of a plugin is killed.
const GroupKill = true
