//go:build windows

package fileacl

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Model is what is checked on this OS (kjak config path).
const Model = "owner, DACL"

// The principals trusted on Windows. Administrators and SYSTEM can read
// every file anyway (backup and take-ownership privileges): trusting them
// exposes nothing more. TrustedInstaller owns the system's programs; it is
// trusted for plugins only. "OWNER RIGHTS" names the owner, which is
// checked on its own.
const (
	sidSystem           = "S-1-5-18"
	sidAdministrators   = "S-1-5-32-544"
	sidOwnerRights      = "S-1-3-4"
	sidTrustedInstaller = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
)

type identity struct {
	user         string // the SID of the user running kjak
	defaultOwner string // the owner the token gives the files it creates
	err          error
}

var current = sync.OnceValue(func() identity {
	t := windows.GetCurrentProcessToken()
	u, err := t.GetTokenUser()
	if err != nil {
		return identity{err: err}
	}
	id := identity{user: u.User.Sid.String(), defaultOwner: u.User.Sid.String()}
	// TokenOwner: Administrators for an elevated administrator when the
	// policy "default owner for objects created by members of the
	// Administrators group" says so (the default on Windows Server)
	var n uint32
	_ = windows.GetTokenInformation(t, windows.TokenOwner, nil, 0, &n)
	if n > 0 {
		buf := make([]byte, n)
		if err := windows.GetTokenInformation(t, windows.TokenOwner, &buf[0], n, &n); err == nil {
			owner := (*struct{ Owner *windows.SID })(unsafe.Pointer(&buf[0])).Owner
			if owner != nil && owner.IsValid() {
				id.defaultOwner = owner.String()
			}
		}
	}
	return id
})

// trusted is the set of SIDs that may hold any right for a.
func trusted(id identity, a Access) map[string]bool {
	t := map[string]bool{id.user: true, sidSystem: true, sidAdministrators: true, sidOwnerRights: true}
	if a != Private {
		t[sidTrustedInstaller] = true
	}
	return t
}

// ownerAllowed: a secrets file must be owned by the user (or by the
// token's default owner: an elevated administrator's files are owned by
// Administrators); a plugin or a directory by the user, SYSTEM,
// Administrators or TrustedInstaller.
func ownerAllowed(id identity, a Access, owner string) bool {
	if a == Private {
		return owner == id.user || (owner == id.defaultOwner && owner == sidAdministrators)
	}
	return owner == id.user || owner == sidSystem || owner == sidAdministrators || owner == sidTrustedInstaller
}

// CheckFile checks the owner and the DACL of the opened file f, read on
// its handle (not its path: no check-then-use race).
func CheckFile(f *os.File, a Access) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%w: its security descriptor cannot be read: %v", ErrNotPrivate, err)
	}
	return checkSD(sd, a, "")
}

// CheckPath checks the owner and the DACL of path (a directory; links
// followed).
func CheckPath(path string, a Access) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("%w: the security descriptor of %s cannot be read: %v", ErrNotPrivate, path, err)
	}
	return checkSD(sd, a, path+": ")
}

func checkSD(sd *windows.SECURITY_DESCRIPTOR, a Access, where string) error {
	id := current()
	if id.err != nil {
		return fmt.Errorf("%w: the user running kjak is not known: %v", ErrNotPrivate, id.err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil || !owner.IsValid() {
		return fmt.Errorf("%w: %sits owner cannot be read", ErrNotPrivate, where)
	}
	if o := owner.String(); !ownerAllowed(id, a, o) {
		return fmt.Errorf("%w: %sit is owned by %s, not by the user running kjak (%s)", ErrNotPrivate, where, accountName(o), id.user)
	}
	dacl, _, err := sd.DACL()
	if errors.Is(err, windows.ERROR_OBJECT_NOT_FOUND) || (err == nil && dacl == nil) {
		// no DACL, or a null DACL: everyone has full access
		return fmt.Errorf("%w: %sit has no DACL (everyone has full access)", ErrNotPrivate, where)
	}
	if err != nil {
		return fmt.Errorf("%w: %sits DACL cannot be read: %v", ErrNotPrivate, where, err)
	}
	// the ACL header: revision, sbz1, size (uint16)
	hdr := unsafe.Slice((*byte)(unsafe.Pointer(dacl)), 8)
	size := int(hdr[2]) | int(hdr[3])<<8
	aces, err := ParseWinACL(unsafe.Slice((*byte)(unsafe.Pointer(dacl)), size))
	if err != nil {
		return fmt.Errorf("%w: %sits DACL: %v", ErrNotPrivate, where, err)
	}
	t := trusted(id, a)
	err = CheckWinACL(aces, WinACLPolicy{
		Access:  a,
		Trusted: func(sid []byte) bool { return t[SIDString(sid)] },
		Name: func(sid []byte) string {
			return accountName(SIDString(sid))
		},
	})
	if err != nil && where != "" {
		return fmt.Errorf("%s%w", where, err)
	}
	return err
}

// accountName is DOMAIN\name of a SID, or the SID.
func accountName(s string) string {
	sid, err := windows.StringToSid(s)
	if err != nil {
		return s
	}
	name, domain, _, err := sid.LookupAccount("")
	if err != nil || name == "" {
		return s
	}
	if domain != "" {
		return domain + `\` + name
	}
	return name
}

// PrivateSDDL is the security descriptor of a new private file: owned by
// the user, a protected DACL (nothing inherited) that gives full access to
// the user, SYSTEM and Administrators.
func PrivateSDDL() (string, error) {
	id := current()
	if id.err != nil {
		return "", id.err
	}
	return fmt.Sprintf("O:%sD:P(A;;FA;;;%s)(A;;FA;;;SY)(A;;FA;;;BA)", id.user, id.user), nil
}
