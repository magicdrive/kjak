package osnotify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Command is one run of an OS program.
type Command struct {
	Path  string
	Args  []string // without the program
	Env   []string // added to kjak's environment
	Stdin []byte
}

// Runner runs a command and returns its standard output, its standard error
// and how it ended (an *exec.ExitError for a non-zero exit). Tests replace
// it.
type Runner func(ctx context.Context, c Command) (stdout, stderr []byte, err error)

// maxOutput bounds what kjak keeps of a program's output.
const maxOutput = 64 << 10

// Exec runs c as a child process. It is killed when ctx is done; its output
// pipes are given up 2 seconds later.
func Exec(ctx context.Context, c Command) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	if len(c.Env) > 0 {
		cmd.Env = append(cmd.Environ(), c.Env...)
	}
	cmd.Stdin = bytes.NewReader(c.Stdin)
	stdout, stderr := &capped{max: maxOutput}, &capped{max: maxOutput}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	return stdout.buf.Bytes(), stderr.buf.Bytes(), err
}

// capped keeps the first max bytes written to it. It is only a Writer (no
// ReadFrom, which io.Copy would use instead of Write).
type capped struct {
	buf bytes.Buffer
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); room > 0 {
		if len(p) > room {
			c.buf.Write(p[:room])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

// failure describes a run that did not succeed: the program could not be
// started (ErrUnavailable), or it ended with an error (its message, the last
// line of its error output, shortened).
func failure(what string, stderr []byte, err error) error {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return fmt.Errorf("%w: %s could not be run: %v", ErrUnavailable, what, err)
	}
	msg := lastLine(stderr)
	if msg == "" {
		msg = ee.String()
	}
	return fmt.Errorf("%w: %s failed: %s", ErrUnavailable, what, msg)
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return clip(oneLine(lines[len(lines)-1]), 300)
}
