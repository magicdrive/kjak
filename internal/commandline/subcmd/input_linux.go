//go:build !windows && !darwin

package subcmd

import "os"

// fifoNeedsWake: poll(2) tells that the writer of a FIFO closed (POLLHUP),
// so every input is polled (readPolled).
const fifoNeedsWake = false

func fifoPath(*os.File) (string, bool) { return "", false }
