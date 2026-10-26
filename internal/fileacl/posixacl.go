package fileacl

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The POSIX ACL of Linux as the system.posix_acl_access extended attribute
// holds it (linux/posix_acl_xattr.h): a little-endian version 2 header and
// entries of (tag u16, perm u16, id u32). Parsed on every OS so that it can
// be tested and fuzzed anywhere.

const (
	posixACLVersion  = 2
	posixACLUserObj  = 0x01
	posixACLUser     = 0x02
	posixACLGroupObj = 0x04
	posixACLGroup    = 0x08
	posixACLMask     = 0x10
	posixACLOther    = 0x20
	posixACLRead     = 4
	posixACLWrite    = 2
	posixACLExecute  = 1
	maxPOSIXEntries  = 1 << 14
)

// POSIXACE is one entry of a POSIX ACL.
type POSIXACE struct {
	Tag  uint16
	Perm uint16
	ID   uint32
}

// ParsePOSIXACL parses the value of system.posix_acl_access.
func ParsePOSIXACL(b []byte) ([]POSIXACE, error) {
	if len(b) < 4 {
		return nil, errors.New("POSIX ACL shorter than its header")
	}
	if v := binary.LittleEndian.Uint32(b); v != posixACLVersion {
		return nil, fmt.Errorf("POSIX ACL version %d", v)
	}
	b = b[4:]
	if len(b)%8 != 0 || len(b)/8 > maxPOSIXEntries {
		return nil, fmt.Errorf("POSIX ACL of %d bytes", len(b))
	}
	aces := make([]POSIXACE, len(b)/8)
	for i := range aces {
		e := b[i*8:]
		aces[i] = POSIXACE{Tag: binary.LittleEndian.Uint16(e), Perm: binary.LittleEndian.Uint16(e[2:]), ID: binary.LittleEndian.Uint32(e[4:])}
	}
	return aces, nil
}

// POSIXPolicy is what a POSIX ACL is checked against.
type POSIXPolicy struct {
	Access Access
	// UID is the effective user; RootTrusted: uid 0 may change it too (a
	// plugin as the mode check: root or the user).
	UID         uint32
	RootTrusted bool
	// Sticky: the directory has the sticky bit (NoReplace: write access
	// for others is then allowed, as for the mode bits).
	Sticky bool
}

// CheckPOSIXACL: the entries that are not the owner's (a named user other
// than the user, the owning group, a named group, others) must not have,
// once limited by the mask (where the ACL has one), a refused permission:
// any for Private, write for NoWrite, write unless sticky for NoReplace.
// An unknown tag is refused.
func CheckPOSIXACL(aces []POSIXACE, p POSIXPolicy) error {
	mask := uint16(posixACLRead | posixACLWrite | posixACLExecute)
	for _, e := range aces {
		if e.Tag == posixACLMask {
			mask = e.Perm
		}
	}
	var refused uint16
	switch p.Access {
	case Private:
		refused = posixACLRead | posixACLWrite | posixACLExecute
	case NoWrite:
		refused = posixACLWrite
	default:
		if !p.Sticky {
			refused = posixACLWrite
		}
	}
	for i, e := range aces {
		var who string
		perm := e.Perm
		switch e.Tag {
		case posixACLUserObj, posixACLMask:
			continue
		case posixACLUser:
			if e.ID == p.UID || (p.RootTrusted && e.ID == 0) {
				continue
			}
			who, perm = fmt.Sprintf("user:%d", e.ID), perm&mask
		case posixACLGroupObj:
			who, perm = "the owning group", perm&mask
		case posixACLGroup:
			who, perm = fmt.Sprintf("group:%d", e.ID), perm&mask
		case posixACLOther:
			who = "others"
		default:
			return fmt.Errorf("%w: POSIX ACL entry %d has the tag 0x%x, which kjak does not know", ErrNotPrivate, i, e.Tag)
		}
		if perm&refused != 0 {
			return fmt.Errorf("%w: the POSIX ACL lets %s %s it (permissions %s); see `getfacl`, remove it with: setfacl -b <file>",
				ErrNotPrivate, who, p.Access, rwx(perm))
		}
	}
	return nil
}

func rwx(p uint16) string {
	b := []byte("---")
	if p&posixACLRead != 0 {
		b[0] = 'r'
	}
	if p&posixACLWrite != 0 {
		b[1] = 'w'
	}
	if p&posixACLExecute != 0 {
		b[2] = 'x'
	}
	return string(b)
}
