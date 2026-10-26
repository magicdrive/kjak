//go:build !windows

package external

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
)

// processAlive: the process exists and is not a zombie (Linux: an orphan
// whose reaper has not reaped it yet is dead already).
func processAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	if b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
		if i := bytes.LastIndexByte(b, ')'); i > 0 && i+2 < len(b) && b[i+2] == 'Z' {
			return false
		}
	}
	return true
}

func killProcess(pid int) { _ = syscall.Kill(pid, syscall.SIGKILL) }
