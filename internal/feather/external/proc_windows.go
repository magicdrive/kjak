//go:build windows

package external

import (
	"errors"
	"os"
	"os/exec"
	"strings"
)

// checkFile checks an executable: a regular .exe file. Its access control
// list is not checked on Windows (docs/plugins.md).
func checkFile(real string, st os.FileInfo) error {
	if !st.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if !strings.EqualFold(real[max(0, len(real)-4):], ".exe") {
		return errors.New("not an .exe file")
	}
	return nil
}

// setProcessGroup: Windows has no process group kill in kjak (no Job
// Object): a cancel kills the plugin's process only; a child it started
// may outlive it (docs/plugins.md).
func setProcessGroup(cmd *exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error { return nil }

// GroupKill: only the plugin's own process is killed on Windows.
const GroupKill = false
