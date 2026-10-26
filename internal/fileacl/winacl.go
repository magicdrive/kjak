package fileacl

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The binary ACL and SID formats of Windows (winnt.h), parsed here on every
// OS so that they can be tested and fuzzed anywhere.

// Windows access rights.
const (
	winFileReadData        = 0x00000001
	winFileWriteData       = 0x00000002
	winFileAppendData      = 0x00000004
	winFileReadEA          = 0x00000008
	winFileWriteEA         = 0x00000010
	winFileExecute         = 0x00000020
	winFileDeleteChild     = 0x00000040
	winFileReadAttributes  = 0x00000080
	winFileWriteAttributes = 0x00000100
	winDelete              = 0x00010000
	winReadControl         = 0x00020000
	winWriteDAC            = 0x00040000
	winWriteOwner          = 0x00080000
	winSynchronize         = 0x00100000
	winAccessSystemSec     = 0x01000000
	winMaximumAllowed      = 0x02000000
	winGenericAll          = 0x10000000
	winGenericExecute      = 0x20000000
	winGenericWrite        = 0x40000000
	winGenericRead         = 0x80000000
)

// winRefused are the rights an untrusted principal must not hold.
func winRefused(a Access) uint32 {
	switch a {
	case Private:
		// everything but reading the attributes and the security
		// descriptor (and waiting on the handle)
		return ^uint32(winReadControl | winSynchronize | winFileReadAttributes)
	case NoWrite:
		return winFileWriteData | winFileAppendData | winFileWriteEA | winFileDeleteChild |
			winFileWriteAttributes | winDelete | winWriteDAC | winWriteOwner |
			winAccessSystemSec | winMaximumAllowed | winGenericAll | winGenericWrite
	}
	// NoReplace: adding a file or a subdirectory is allowed (as a sticky
	// directory); removing or renaming one, or changing the ACL, is not
	return winFileDeleteChild | winDelete | winWriteDAC | winWriteOwner |
		winAccessSystemSec | winMaximumAllowed | winGenericAll
}

// ACE types and flags.
const (
	winAccessAllowed               = 0x00
	winAccessDenied                = 0x01
	winSystemAudit                 = 0x02
	winSystemAlarm                 = 0x03
	winAccessAllowedCompound       = 0x04
	winAccessAllowedObject         = 0x05
	winAccessDeniedObject          = 0x06
	winAccessAllowedCallback       = 0x09
	winAccessDeniedCallback        = 0x0a
	winAccessAllowedCallbackObject = 0x0b
	winAccessDeniedCallbackObject  = 0x0c

	winInheritOnlyACE = 0x08
	winInheritedACE   = 0x10
)

// WinACE is one entry of a DACL.
type WinACE struct {
	Type  byte
	Flags byte
	Mask  uint32
	SID   []byte // the binary SID (nil for a type without one at a known place)
}

// ParseWinACL parses a binary ACL (the ACL header and its ACEs). Anything
// out of bounds or inconsistent is an error.
func ParseWinACL(b []byte) ([]WinACE, error) {
	if len(b) < 8 {
		return nil, errors.New("ACL shorter than its header")
	}
	rev := b[0]
	if rev != 2 && rev != 4 {
		return nil, fmt.Errorf("ACL revision %d", rev)
	}
	size := int(binary.LittleEndian.Uint16(b[2:4]))
	count := int(binary.LittleEndian.Uint16(b[4:6]))
	if size < 8 || size > len(b) {
		return nil, fmt.Errorf("ACL size %d out of bounds (%d bytes)", size, len(b))
	}
	b = b[:size]
	aces := make([]WinACE, 0, min(count, size/8))
	off := 8
	for i := 0; i < count; i++ {
		if off+4 > len(b) {
			return nil, fmt.Errorf("ACE %d beyond the ACL", i)
		}
		typ, flags := b[off], b[off+1]
		aceSize := int(binary.LittleEndian.Uint16(b[off+2 : off+4]))
		if aceSize < 4 || aceSize%4 != 0 || off+aceSize > len(b) {
			return nil, fmt.Errorf("ACE %d: size %d out of bounds", i, aceSize)
		}
		body := b[off+4 : off+aceSize]
		ace := WinACE{Type: typ, Flags: flags}
		switch typ {
		case winAccessAllowed, winAccessDenied, winSystemAudit, winSystemAlarm,
			winAccessAllowedCallback, winAccessDeniedCallback:
			if len(body) < 4 {
				return nil, fmt.Errorf("ACE %d: no access mask", i)
			}
			ace.Mask = binary.LittleEndian.Uint32(body)
			n, err := sidLen(body[4:])
			if err != nil {
				return nil, fmt.Errorf("ACE %d: %v", i, err)
			}
			ace.SID = body[4 : 4+n]
		default:
			// object and compound ACEs: the mask is first, the SID is
			// after optional GUIDs; the SID is not needed (an allow
			// entry of these types is refused whoever it names)
			if len(body) >= 4 {
				ace.Mask = binary.LittleEndian.Uint32(body)
			}
		}
		aces = append(aces, ace)
		off += aceSize
	}
	return aces, nil
}

// sidLen is the length of the binary SID at the start of b.
func sidLen(b []byte) (int, error) {
	if len(b) < 8 {
		return 0, errors.New("SID shorter than its header")
	}
	if b[0] != 1 {
		return 0, fmt.Errorf("SID revision %d", b[0])
	}
	n := 8 + 4*int(b[1])
	if b[1] > 15 || n > len(b) {
		return 0, fmt.Errorf("SID with %d sub-authorities out of bounds", b[1])
	}
	return n, nil
}

// SIDString renders a binary SID as S-1-....
func SIDString(sid []byte) string {
	n, err := sidLen(sid)
	if err != nil {
		return "(invalid SID)"
	}
	var auth uint64
	for _, c := range sid[2:8] {
		auth = auth<<8 | uint64(c)
	}
	var sb strings.Builder
	sb.WriteString("S-1-")
	sb.WriteString(strconv.FormatUint(auth, 10))
	for off := 8; off < n; off += 4 {
		sb.WriteByte('-')
		sb.WriteString(strconv.FormatUint(uint64(binary.LittleEndian.Uint32(sid[off:])), 10))
	}
	return sb.String()
}

// wellKnownSIDs name the SIDs a refused ACE usually has.
var wellKnownSIDs = map[string]string{
	"S-1-1-0":      "Everyone",
	"S-1-3-0":      "CREATOR OWNER",
	"S-1-3-1":      "CREATOR GROUP",
	"S-1-5-2":      "NETWORK",
	"S-1-5-4":      "INTERACTIVE",
	"S-1-5-7":      "ANONYMOUS LOGON",
	"S-1-5-11":     "Authenticated Users",
	"S-1-5-32-545": "Users",
	"S-1-5-32-546": "Guests",
	"S-1-15-2-1":   "ALL APPLICATION PACKAGES",
}

// WinACLPolicy is what a DACL is checked against.
type WinACLPolicy struct {
	Access Access
	// Trusted reports whether a principal (a binary SID) may hold any
	// right.
	Trusted func(sid []byte) bool
	// Name names a principal for the error (optional).
	Name func(sid []byte) string
}

// CheckWinACL checks the ACEs of a DACL: no allow ACE that applies to the
// object (not inherit-only) may give an untrusted principal a refused
// right; an allow ACE of a type kjak does not read (object, callback,
// compound) is refused, whatever it names; audit and alarm entries do not
// belong in a DACL and are refused. Deny ACEs only take rights away and are
// skipped (they are not credited either). Inherited ACEs are checked as
// the explicit ones: what matters is what the ACL grants now.
func CheckWinACL(aces []WinACE, p WinACLPolicy) error {
	refused := winRefused(p.Access)
	for i, ace := range aces {
		if ace.Flags&winInheritOnlyACE != 0 {
			continue // applies to the children of a directory only
		}
		switch ace.Type {
		case winAccessDenied, winAccessDeniedObject, winAccessDeniedCallback, winAccessDeniedCallbackObject:
			continue
		case winAccessAllowed:
			if ace.Mask&refused == 0 || (p.Trusted != nil && p.Trusted(ace.SID)) {
				continue
			}
			return fmt.Errorf("%w: %s can %s it (%s ACE %d, rights 0x%08x); run: icacls <file> /inheritance:r /grant:r \"%%USERNAME%%:F\"",
				ErrNotPrivate, winPrincipal(ace.SID, p.Name), p.Access, inherited(ace.Flags), i, ace.Mask)
		case winAccessAllowedObject, winAccessAllowedCallback, winAccessAllowedCallbackObject, winAccessAllowedCompound:
			if ace.Mask&refused == 0 {
				continue
			}
			return fmt.Errorf("%w: ACE %d is an allow ACE of type %d, which kjak does not evaluate", ErrNotPrivate, i, ace.Type)
		default:
			return fmt.Errorf("%w: ACE %d has the type %d, which kjak does not know in a DACL", ErrNotPrivate, i, ace.Type)
		}
	}
	return nil
}

func inherited(flags byte) string {
	if flags&winInheritedACE != 0 {
		return "inherited"
	}
	return "explicit"
}

func winPrincipal(sid []byte, name func([]byte) string) string {
	s := SIDString(sid)
	if n, ok := wellKnownSIDs[s]; ok {
		return n + " (" + s + ")"
	}
	if name != nil {
		if n := name(sid); n != "" && n != s {
			return n + " (" + s + ")"
		}
	}
	return s
}
