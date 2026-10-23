//go:build !windows && !darwin

package subcmd

import "os"

// fifoEndPolled: poll(2) tells that the writer of a FIFO closed (POLLHUP),
// so every input is polled (readPolled).
const fifoEndPolled = true

func fifoPath(*os.File) (string, bool) { return "", false }

func selectFIFO(int, int) (bool, bool, error) { return false, false, nil }
