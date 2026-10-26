//go:build !windows

package main

import "os/exec"

// startBreakaway: a child that leaves the process group (Setsid would; the
// test runs on Windows only).
func startBreakaway(self, pidfile string) error {
	return exec.Command(self, "child", pidfile).Start()
}
