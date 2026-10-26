//go:build windows

package fileacl

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	sidWorld         = "S-1-1-0"
	sidUsers         = "S-1-5-32-545"
	sidAuthenticated = "S-1-5-11"
)

// icacls changes an ACL with the system's own tool.
func icacls(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("icacls", args...).CombinedOutput(); err != nil {
		t.Fatalf("icacls %v: %v %s", args, err, out)
	}
}

func newFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "secrets.toml")
	if err := os.WriteFile(p, []byte("a = \"b\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func checkOpened(t *testing.T, path string, a Access) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	return CheckFile(f, a)
}

// privateFile makes a file private as the docs say: no inheritance, full
// control for the user only.
func privateFile(t *testing.T, path string) {
	t.Helper()
	icacls(t, path, "/inheritance:r", "/grant:r", "*"+current().user+":F")
}

// Real DACLs, changed with icacls: Everyone, Users and Authenticated
// Users are refused whether the ACE is explicit or inherited; a deny ACE is
// not; SYSTEM and Administrators are trusted.
func TestCritical_WindowsFileDACL(t *testing.T) {
	if id := current(); id.err != nil {
		t.Fatal(id.err)
	} else {
		t.Logf("user %s, default owner %s", id.user, id.defaultOwner)
	}
	dir := t.TempDir()
	p := newFile(t, dir)
	if out, err := exec.Command("icacls", p).CombinedOutput(); err == nil {
		t.Logf("a new file in the temporary directory:\n%s", out)
	}
	privateFile(t, p)
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a private file: %v", err)
	}
	for sid, name := range map[string]string{sidWorld: "Everyone", sidUsers: "Users", sidAuthenticated: "Authenticated Users"} {
		icacls(t, p, "/grant", "*"+sid+":(R)")
		err := checkOpened(t, p, Private)
		if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), name) {
			t.Errorf("%s can read: %v", name, err)
		}
		icacls(t, p, "/remove:g", "*"+sid)
	}
	// SYSTEM and Administrators are trusted
	icacls(t, p, "/grant", "*"+sidSystem+":F", "/grant", "*"+sidAdministrators+":F")
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("SYSTEM and Administrators: %v", err)
	}
	// a deny ACE takes rights away: nothing to refuse (specific rights:
	// "(W)" would deny SYNCHRONIZE and READ_CONTROL, which every open
	// asks for, to the user too)
	icacls(t, p, "/deny", "*"+sidWorld+":(WD,AD)")
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a deny ACE: %v", err)
	}
	icacls(t, p, "/remove:d", "*"+sidWorld)
	// read and execute for Everyone: fine for a plugin, not for a secret
	icacls(t, p, "/grant", "*"+sidWorld+":(RX)")
	if err := checkOpened(t, p, NoWrite); err != nil {
		t.Fatalf("Everyone can read a plugin: %v", err)
	}
	icacls(t, p, "/grant", "*"+sidWorld+":(M)")
	if err := checkOpened(t, p, NoWrite); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Everyone can change a plugin: %v", err)
	}
	icacls(t, p, "/remove:g", "*"+sidWorld)

	// inherited: a directory that passes Everyone:(R) on to its files
	inh := filepath.Join(dir, "inherit")
	if err := os.Mkdir(inh, 0o700); err != nil {
		t.Fatal(err)
	}
	icacls(t, inh, "/grant", "*"+sidWorld+":(OI)(R)")
	q := newFile(t, inh)
	err := checkOpened(t, q, Private)
	if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "inherited") {
		t.Fatalf("an inherited ACE: %v", err)
	}
	// turning inheritance back on brings it back
	privateFile(t, q)
	if err := checkOpened(t, q, Private); err != nil {
		t.Fatalf("inheritance removed: %v", err)
	}
	icacls(t, q, "/inheritance:e")
	if err := checkOpened(t, q, Private); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("inheritance enabled again: %v", err)
	}

	// a directory: adding entries is not replacing them
	icacls(t, inh, "/grant", "*"+sidAuthenticated+":(AD,WD)")
	if err := CheckPath(inh, NoWrite); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Authenticated Users can add files: %v", err)
	}
	if err := CheckPath(inh, NoReplace); err != nil {
		t.Fatalf("adding files above a plugin: %v", err)
	}
	icacls(t, inh, "/grant", "*"+sidAuthenticated+":(DC)")
	if err := CheckPath(inh, NoReplace); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("Authenticated Users can delete entries: %v", err)
	}
}

// A null DACL gives everyone full access: refused.
func TestCritical_WindowsNullDACL(t *testing.T) {
	p := newFile(t, t.TempDir())
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	err := checkOpened(t, p, Private)
	if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "no DACL") {
		t.Fatalf("a null DACL: %v", err)
	}
	if err := CheckPath(p, NoReplace); !errors.Is(err, ErrNotPrivate) {
		t.Fatalf("a null DACL on a path: %v", err)
	}
}

// A file owned by someone else is refused, whatever its DACL.
func TestCritical_WindowsOwner(t *testing.T) {
	p := newFile(t, t.TempDir())
	privateFile(t, p)
	system, err := windows.StringToSid(sidSystem)
	if err != nil {
		t.Fatal(err)
	}
	if !enablePrivilege("SeRestorePrivilege") {
		t.Skip("SeRestorePrivilege is not held: the owner cannot be set to another principal")
	}
	if err := windows.SetNamedSecurityInfo(p, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION, system, nil, nil, nil); err != nil {
		t.Skip("the owner cannot be changed:", err)
	}
	err = checkOpened(t, p, Private)
	if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "owned by") {
		t.Fatalf("owned by SYSTEM: %v", err)
	}
	// a plugin may be SYSTEM's
	if err := checkOpened(t, p, NoWrite); err != nil {
		t.Fatalf("a plugin owned by SYSTEM: %v", err)
	}
}

func enablePrivilege(name string) bool {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok); err != nil {
		return false
	}
	defer func() { _ = tok.Close() }()
	var luid windows.LUID
	if err := windows.LookupPrivilegeValue(nil, windows.StringToUTF16Ptr(name), &luid); err != nil {
		return false
	}
	tp := windows.Tokenprivileges{PrivilegeCount: 1}
	tp.Privileges[0] = windows.LUIDAndAttributes{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED}
	err := windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil)
	return err == nil // ERROR_NOT_ALL_ASSIGNED is returned as an error
}

// PrivateSDDL makes a file that passes the check in a directory that
// would pass Everyone on.
func TestWindowsPrivateSDDL(t *testing.T) {
	dir := t.TempDir()
	icacls(t, dir, "/grant", "*"+sidWorld+":(OI)(CI)(F)")
	sddl, err := PrivateSDDL()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "new.toml")
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(p), windows.GENERIC_WRITE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = windows.CloseHandle(h)
	if err := checkOpened(t, p, Private); err != nil {
		t.Fatalf("a file made with PrivateSDDL: %v", err)
	}
}
