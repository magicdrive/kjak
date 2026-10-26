package fileacl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// The extended security of macOS (sys/kauth.h) as getattrlist returns it
// for ATTR_CMN_EXTENDED_SECURITY: a kauth_filesec in the host byte order
// (little-endian on every Mac kjak is built for). Parsed on every OS so
// that it can be tested and fuzzed anywhere.

// kauth rights.
const (
	kauthReadData         = 1 << 1
	kauthWriteData        = 1 << 2 // also ADD_FILE
	kauthExecute          = 1 << 3
	kauthDelete           = 1 << 4
	kauthAppendData       = 1 << 5 // also ADD_SUBDIRECTORY
	kauthDeleteChild      = 1 << 6
	kauthReadAttributes   = 1 << 7
	kauthWriteAttributes  = 1 << 8
	kauthReadExtAttrs     = 1 << 9
	kauthWriteExtAttrs    = 1 << 10
	kauthReadSecurity     = 1 << 11
	kauthWriteSecurity    = 1 << 12
	kauthChangeOwner      = 1 << 13
	kauthSynchronize      = 1 << 20
	kauthGenericAll       = 1 << 21
	kauthGenericExecute   = 1 << 22
	kauthGenericWrite     = 1 << 23
	kauthGenericRead      = 1 << 24
	kauthACEKindMask      = 0xf
	kauthACEPermit        = 1
	kauthACEDeny          = 2
	kauthACEAudit         = 3
	kauthACEAlarm         = 4
	kauthACEInherited     = 1 << 4
	kauthACEOnlyInherit   = 1 << 8
	kauthFilesecMagic     = 0x012cc16d
	kauthFilesecNoACL     = 0xffffffff
	kauthFilesecHeader    = 4 + 16 + 16 + 4 + 4 // magic, owner, group, count, flags
	kauthACESize          = 16 + 4 + 4
	maxDarwinACLEntries   = 128 // ACL_MAX_ENTRIES
	attrlistHeaderAndAttr = 4 + 8
)

func kauthRefused(a Access) uint32 {
	switch a {
	case Private:
		return ^uint32(kauthReadAttributes | kauthReadExtAttrs | kauthReadSecurity | kauthSynchronize)
	case NoWrite:
		return kauthWriteData | kauthAppendData | kauthDelete | kauthDeleteChild |
			kauthWriteAttributes | kauthWriteExtAttrs | kauthWriteSecurity | kauthChangeOwner |
			kauthGenericAll | kauthGenericWrite
	}
	return kauthDelete | kauthDeleteChild | kauthWriteSecurity | kauthChangeOwner |
		kauthGenericAll | kauthGenericWrite
}

// DarwinACE is one entry of a macOS ACL.
type DarwinACE struct {
	Applicable [16]byte // the GUID of the user or group
	Flags      uint32
	Rights     uint32
}

// ParseAttrlistFilesec takes the kauth_filesec out of the buffer of a
// getattrlist call for ATTR_CMN_EXTENDED_SECURITY alone: the length of the
// returned attributes, then an attrreference (offset from itself, length).
// A file without an ACL has a zero length: nil.
func ParseAttrlistFilesec(buf []byte) ([]byte, error) {
	if len(buf) < attrlistHeaderAndAttr {
		return nil, errors.New("getattrlist: short buffer")
	}
	total := binary.LittleEndian.Uint32(buf)
	if total < attrlistHeaderAndAttr || int(total) > len(buf) {
		return nil, fmt.Errorf("getattrlist: length %d out of bounds", total)
	}
	buf = buf[:total]
	off := int64(int32(binary.LittleEndian.Uint32(buf[4:])))
	n := int64(binary.LittleEndian.Uint32(buf[8:]))
	if n == 0 {
		return nil, nil
	}
	start := 4 + off
	if off < 0 || start+n > int64(len(buf)) {
		return nil, fmt.Errorf("getattrlist: extended security at %d+%d out of bounds", start, n)
	}
	return buf[start : start+n], nil
}

// ParseFilesec parses a kauth_filesec. A filesec without an ACL
// (KAUTH_FILESEC_NOACL) has no entries.
func ParseFilesec(b []byte) ([]DarwinACE, error) {
	if len(b) == 0 {
		return nil, nil
	}
	if len(b) < kauthFilesecHeader {
		return nil, errors.New("extended security shorter than its header")
	}
	if m := binary.LittleEndian.Uint32(b); m != kauthFilesecMagic {
		return nil, fmt.Errorf("extended security magic 0x%08x", m)
	}
	count := binary.LittleEndian.Uint32(b[36:])
	if count == kauthFilesecNoACL {
		return nil, nil
	}
	if count > maxDarwinACLEntries || kauthFilesecHeader+int(count)*kauthACESize > len(b) {
		return nil, fmt.Errorf("ACL of %d entries out of bounds (%d bytes)", count, len(b))
	}
	aces := make([]DarwinACE, count)
	for i := range aces {
		e := b[kauthFilesecHeader+i*kauthACESize:]
		copy(aces[i].Applicable[:], e[:16])
		aces[i].Flags = binary.LittleEndian.Uint32(e[16:])
		aces[i].Rights = binary.LittleEndian.Uint32(e[20:])
	}
	return aces, nil
}

// CheckDarwinACL: no allow entry that applies to the object (not
// only-inherit) may grant a refused right, whoever it names (the GUIDs are
// not mapped without the directory services). Deny entries are skipped;
// audit and alarm entries (not implemented by macOS) grant nothing; an
// entry of another kind is refused.
func CheckDarwinACL(aces []DarwinACE, a Access) error {
	refused := uint32(kauthRefused(a))
	for i, ace := range aces {
		if ace.Flags&kauthACEOnlyInherit != 0 {
			continue
		}
		switch ace.Flags & kauthACEKindMask {
		case kauthACEDeny, kauthACEAudit, kauthACEAlarm:
			continue
		case kauthACEPermit:
			if ace.Rights&refused == 0 {
				continue
			}
			return fmt.Errorf("%w: an ACL entry lets %s %s it (%s entry %d, rights 0x%08x); see `ls -le`, remove it with: chmod -N <file>",
				ErrNotPrivate, darwinPrincipal(ace.Applicable), a, darwinInherited(ace.Flags), i, ace.Rights)
		default:
			return fmt.Errorf("%w: ACL entry %d has the kind %d, which kjak does not know", ErrNotPrivate, i, ace.Flags&kauthACEKindMask)
		}
	}
	return nil
}

func darwinInherited(flags uint32) string {
	if flags&kauthACEInherited != 0 {
		return "inherited"
	}
	return "explicit"
}

// darwinPrincipal names a GUID: the compatibility GUIDs of the membership
// API carry a uid (FFFFEEEE-DDDD-CCCC-BBBB-AAAAxxxxxxxx) or a gid
// (ABCDEFAB-CDEF-ABCD-EFAB-CDEFxxxxxxxx); others are printed as they are.
func darwinPrincipal(g [16]byte) string {
	user := [12]byte{0xff, 0xff, 0xee, 0xee, 0xdd, 0xdd, 0xcc, 0xcc, 0xbb, 0xbb, 0xaa, 0xaa}
	group := [12]byte{0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef}
	id := binary.BigEndian.Uint32(g[12:])
	switch {
	case [12]byte(g[:12]) == group && id == 12:
		return "group:everyone"
	case [12]byte(g[:12]) == group:
		return fmt.Sprintf("gid %d", id)
	case [12]byte(g[:12]) == user:
		return fmt.Sprintf("uid %d", id)
	}
	h := fmt.Sprintf("%X", g[:])
	return "GUID " + strings.Join([]string{h[0:8], h[8:12], h[12:16], h[16:20], h[20:32]}, "-")
}
