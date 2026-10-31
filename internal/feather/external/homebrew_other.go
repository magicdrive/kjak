//go:build !darwin && !windows

package external

// homebrewGroupWrite: no exception outside macOS (Linux, Windows and the
// other systems keep the strict checks).
func homebrewGroupWrite(dir, path, real string, holds bool) bool { return false }
