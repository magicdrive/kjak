package external

import (
	"errors"
	"fmt"
)

// Homebrew (macOS only, homebrew_darwin.go). Homebrew's installer makes
// <prefix>/bin, <prefix>/Cellar and <prefix>/opt writable by the admin
// group (mode 775), so a plugin installed with brew fails the place checks:
// another member of admin could replace the link in bin, or the keg under
// Cellar, without authenticating as root. kjak does not treat that group as
// root. A plugin there runs only when the user's own statement of its
// content is met:
//
//   - a feather of the global configuration pins its sha256, and the file's
//     SHA-256 is that value;
//   - a feather of a project: the project's trust records its resolved path
//     and SHA-256, and the file is that path and has that SHA-256.
//
// Only those three directories of the two standard prefixes are excepted,
// and only from the group-write rule, under conditions (owner, group,
// mode, a real directory, the plugin's keg layout) that keep every other
// check as it is; the strict checks run first, and the exception is tried
// only for a candidate they refused. Verify hashes such a plugin again, and
// checks its place again, before every run. Discover never reports it as
// ok: it reports the SHA-256 to pin.
//
// The SHA-256 covers the plugin's own file only, not what it loads. A
// script (its interpreter in <prefix>/bin) or a Mach-O loading a library of
// the prefix (<prefix>/opt/<dep>/lib) would run whatever another admin user
// put there, pin or not, so the exception takes only a Mach-O whose every
// library is the system's (/usr/lib, /System/Library), with no run path
// (selfContained). A plugin that needs more is not run from Homebrew's
// directories: install it elsewhere.

// Accept decides whether a plugin found in Homebrew's admin-writable
// directories may run: nil when real, with content sum, is what the user
// stated. Its error says what is missing.
type Accept func(real, sum string) error

// relaxed is the place check's record of the Homebrew exception: nil means
// the strict checks only.
type relaxed struct {
	used []string // the directories that were writable by group admin
}

// HomebrewError: the plugin is in Homebrew's admin-writable directories and
// its content was not accepted.
type HomebrewError struct {
	Path, Real, SHA256 string
	Dirs               []string // writable by group admin
	Reason             error    // from Accept
}

func (e *HomebrewError) Error() string {
	return fmt.Sprintf("%s is in Homebrew's directories that the admin group can write to (%v): it runs only %v", e.Path, e.Dirs, e.Reason)
}

func (e *HomebrewError) Unwrap() error { return ErrPlugin }

// errNotAccepted is Discover's reason: it knows no configuration.
var errNotAccepted = errors.New("for a feather that pins its sha256 (or a project trust that records it)")
