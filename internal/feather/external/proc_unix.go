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
// the user, not writable by group or others, in a directory others cannot
// write to unless it is sticky.
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
	if sys, ok := st.Sys().(*syscall.Stat_t); ok {
		if uid := os.Geteuid(); sys.Uid != 0 && int(sys.Uid) != uid {
			return fmt.Errorf("owned by uid %d, neither root nor the user running kjak", sys.Uid)
		}
	}
	dir, err := os.Stat(filepath.Dir(real))
	if err != nil {
		return err
	}
	if dir.Mode().Perm()&0o002 != 0 && dir.Mode()&os.ModeSticky == 0 {
		return fmt.Errorf("its directory %s is writable by others", filepath.Dir(real))
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
