//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// startBreakaway tries to start a child outside the plugin's job
// (CREATE_BREAKAWAY_FROM_JOB): kjak's job does not allow it.
func startBreakaway(self, pidfile string) error {
	cmd := exec.Command(self, "child", pidfile)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x01000000}
	return cmd.Start()
}
