//go:build !linux && !darwin && !windows

package fileacl

import "os"

// Model is what is checked on this OS (kjak config path): kjak is released
// for Linux, macOS and Windows; on another Unix only the owner and the mode
// bits are checked (by the callers).
const Model = "owner, mode bits (ACLs not read on this OS)"

// CheckFile: ACLs are not read on this OS.
func CheckFile(*os.File, Access) error { return nil }

// CheckPath: ACLs are not read on this OS.
func CheckPath(string, Access) error { return nil }
