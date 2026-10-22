package credential

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Keychain reads a secret from the OS keychain. kjak only reads it: entries
// are created with the OS tools (docs/security.md), never by kjak, because
// the tools that write without cgo take the secret as an argument, which
// other processes can see.
type Keychain interface {
	// Lookup returns when ctx is done (SIGINT) at the latest.
	Lookup(ctx context.Context, service, account string) (string, error)
}

var (
	ErrKeychainUnsupported = errors.New("the OS keychain is not supported on " + runtime.GOOS + " in kjak v0.1.0")
	ErrKeychainNotFound    = errors.New("no such keychain entry")
	ErrKeychainUnavailable = errors.New("the keychain tool is not available")
)

// Runner runs a command and returns its standard output. Tests replace it.
type Runner func(ctx context.Context, name string, args ...string) (stdout []byte, exitCode int, err error)

func execRunner(ctx context.Context, name string, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil // the tools print nothing secret there, but nothing useful either
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), ee.ExitCode(), nil
	}
	return out.Bytes(), 0, err
}

// keychainTimeout bounds a lookup; the macOS tool may wait for a user
// confirmation dialog.
const keychainTimeout = 30 * time.Second

// DefaultKeychain is the keychain of this OS, nil where kjak supports none
// (Windows in v0.1.0: the Credential Manager API needs either cgo or a
// syscall binding kjak does not ship yet).
func DefaultKeychain() Keychain {
	return keychainFor(runtime.GOOS, execRunner, exec.LookPath)
}

func keychainFor(goos string, run Runner, lookPath func(string) (string, error)) Keychain {
	switch goos {
	case "darwin":
		return &macKeychain{run: run}
	case "linux":
		return &secretToolKeychain{run: run, lookPath: lookPath}
	}
	return nil
}

// macKeychain uses /usr/bin/security (the login keychain).
type macKeychain struct{ run Runner }

func (k *macKeychain) Lookup(ctx context.Context, service, account string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, keychainTimeout)
	defer cancel()
	out, code, err := k.run(ctx, "/usr/bin/security", "find-generic-password", "-s", service, "-a", account, "-w")
	if ctx.Err() != nil {
		return "", context.Cause(ctx)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrKeychainUnavailable, err)
	}
	switch code {
	case 0:
		return strings.TrimSuffix(string(out), "\n"), nil
	case 44: // errSecItemNotFound
		return "", ErrKeychainNotFound
	}
	return "", fmt.Errorf("security exited with status %d", code)
}

// secretToolKeychain uses secret-tool (libsecret: GNOME Keyring, KWallet via
// the Secret Service API).
type secretToolKeychain struct {
	run      Runner
	lookPath func(string) (string, error)
}

func (k *secretToolKeychain) Lookup(ctx context.Context, service, account string) (string, error) {
	path, err := k.lookPath("secret-tool")
	if err != nil {
		return "", fmt.Errorf("%w: secret-tool (libsecret-tools) is not installed", ErrKeychainUnavailable)
	}
	ctx, cancel := context.WithTimeout(ctx, keychainTimeout)
	defer cancel()
	out, code, err := k.run(ctx, path, "lookup", "service", service, "account", account)
	if ctx.Err() != nil {
		return "", context.Cause(ctx)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrKeychainUnavailable, err)
	}
	if code != 0 {
		// secret-tool exits 1 both for a missing entry and for a missing
		// Secret Service; it prints nothing for a missing entry.
		return "", ErrKeychainNotFound
	}
	v := strings.TrimSuffix(string(out), "\n")
	if v == "" {
		return "", ErrKeychainNotFound
	}
	return v, nil
}
