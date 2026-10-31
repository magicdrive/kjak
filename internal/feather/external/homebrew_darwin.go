//go:build darwin

package external

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// gidAdmin is macOS's admin group.
const gidAdmin = 80

// homebrewPrefixes are the standard Homebrew prefixes (Apple silicon,
// Intel). Another prefix gets no exception. Tests replace it.
var homebrewPrefixes = []string{"/opt/homebrew", "/usr/local"}

// homebrewKeg: real is <prefix>/Cellar/<formula>/<version>/bin/<file>,
// spelled exactly (clean, no empty, "." or ".." element), for one of the
// prefixes. It returns the prefix.
func homebrewKeg(real string) (string, bool) {
	if real != filepath.Clean(real) {
		return "", false
	}
	for _, p := range homebrewPrefixes {
		rest, ok := strings.CutPrefix(real, p+"/Cellar/")
		if !ok {
			continue
		}
		parts := strings.Split(rest, "/")
		if len(parts) != 4 || parts[2] != "bin" {
			return "", false
		}
		for _, s := range parts {
			if s == "" || s == "." || s == ".." {
				return "", false
			}
		}
		return p, true
	}
	return "", false
}

// homebrewGroupWrite reports whether dir, which the strict check refused
// because its group can write to it, is excepted for the plugin at path
// (where it was found) leading to real: one of the directories Homebrew
// makes writable by admin, for a plugin in a keg of the same prefix. holds:
// dir holds the plugin's name (only <prefix>/bin, and only for the name
// <prefix>/bin/<file>); otherwise dir is above it (<prefix>/Cellar or
// <prefix>/opt). dir itself must be a real directory (not a link), owned by
// the user running kjak (not root: Homebrew's own directories belong to the
// user who installed it), of group admin, not writable by others, without
// setuid or setgid. The ACL is checked by the caller as for any directory.
func homebrewGroupWrite(dir, path, real string, holds bool) bool {
	prefix, ok := homebrewKeg(real)
	if !ok {
		return false
	}
	if holds {
		if dir != prefix+"/bin" || path != prefix+"/bin/"+filepath.Base(real) {
			return false
		}
	} else if dir != prefix+"/Cellar" && dir != prefix+"/opt" {
		return false
	}
	st, err := os.Lstat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	return homebrewDirMode(st.Mode(), st.Sys(), os.Geteuid())
}

// homebrewDirMode: the owner, group and mode bits of an excepted directory.
func homebrewDirMode(mode os.FileMode, sys any, euid int) bool {
	s, ok := sys.(*syscall.Stat_t)
	if !ok || mode&os.ModeType != os.ModeDir {
		return false
	}
	return int(s.Uid) == euid && s.Uid != 0 && s.Gid == gidAdmin &&
		mode.Perm()&0o002 == 0 && mode&(os.ModeSetuid|os.ModeSetgid) == 0
}
