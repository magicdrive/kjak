//go:build !darwin

package external

import (
	"errors"
	"io"
)

// selfContained: never reached outside macOS (no Homebrew exception).
func selfContained(io.ReaderAt) error { return errors.New("no Homebrew exception on this system") }
