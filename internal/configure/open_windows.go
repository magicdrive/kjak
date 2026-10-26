//go:build windows

package configure

import "os"

// openConfig opens a configuration file for reading (its type is checked
// after the open, on the handle).
func openConfig(path string) (*os.File, error) {
	return os.Open(path)
}
