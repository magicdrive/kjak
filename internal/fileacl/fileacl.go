// Package fileacl checks the access control lists of files and
// directories: who, besides the user, the ACL lets read or change them.
//
// Each OS has its own model, and this package does not pretend otherwise
// (docs/security.md has the table):
//
//   - Linux: the POSIX ACL (the system.posix_acl_access extended
//     attribute) next to the mode bits the callers check. Its named
//     entries are limited by the mask, which is the group bits of the mode,
//     so the mode check already bounds them; the ACL is read anyway, and an
//     entry that grants access is refused (a file system that does not keep
//     the two in step). NFSv4 ACLs are not read.
//   - macOS: the extended security (ACL) of the file, read with
//     fgetattrlist(ATTR_CMN_EXTENDED_SECURITY) without cgo. An entry names
//     its principal by a GUID, which only the directory services
//     (membership API, cgo) map to a user or a group: every allow entry
//     that grants a refused right is refused, whoever it names, the user
//     included (fail closed). Deny entries only take rights away and are
//     ignored.
//   - Windows: the owner and the DACL of the file, read on the opened
//     handle (GetSecurityInfo). Only the user, SYSTEM and Administrators
//     (and, for plugins, TrustedInstaller) may hold a refused right; a
//     null DACL, an ACE type kjak does not know, or an ACE of an unknown
//     principal is refused. Deny ACEs are ignored (they are not credited).
//
// The checks are made against what the ACL grants, never against what a
// deny entry might take away again: a file that would be safe only thanks
// to a deny entry is refused.
package fileacl

import (
	"errors"
)

// ErrNotPrivate: the ACL lets someone other than the user (and the
// principals trusted on the OS) read or change the file.
var ErrNotPrivate = errors.New("the access control list grants access to others")

// Access is what the ACL must not grant to anyone else.
type Access int

const (
	// Private: no access at all to the content, and no change (a secrets
	// file). Reading the attributes and the ACL itself is allowed.
	Private Access = iota
	// NoWrite: no change of the file (or of what a directory holds), of
	// its ACL or of its owner (a plugin and the directories that hold it).
	NoWrite
	// NoReplace: no removal or replacement of what the directory holds,
	// no change of its ACL or owner; adding new entries is allowed (a
	// directory above a plugin: as the sticky bit).
	NoReplace
)

func (a Access) String() string {
	switch a {
	case Private:
		return "read or change"
	case NoWrite:
		return "change"
	case NoReplace:
		return "remove or replace entries"
	}
	return "access"
}
