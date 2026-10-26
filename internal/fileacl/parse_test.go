package fileacl

import (
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// sid encodes "S-1-a-b-c" in the binary form.
func sid(t testing.TB, s string) []byte {
	t.Helper()
	parts := strings.Split(s, "-")
	auth, err := strconv.ParseUint(parts[2], 10, 48)
	if err != nil {
		t.Fatal(err)
	}
	b := []byte{1, byte(len(parts) - 3), 0, 0, 0, 0, 0, 0}
	for i := 0; i < 6; i++ {
		b[7-i] = byte(auth >> (8 * i))
	}
	for _, p := range parts[3:] {
		v, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		b = binary.LittleEndian.AppendUint32(b, uint32(v))
	}
	return b
}

type testACE struct {
	typ, flags byte
	mask       uint32
	sid        string
}

// winACL encodes a revision 2 ACL.
func winACL(t testing.TB, aces ...testACE) []byte {
	var body []byte
	for _, a := range aces {
		s := sid(t, a.sid)
		size := 8 + len(s)
		body = append(body, a.typ, a.flags)
		body = binary.LittleEndian.AppendUint16(body, uint16(size))
		body = binary.LittleEndian.AppendUint32(body, a.mask)
		body = append(body, s...)
	}
	h := []byte{2, 0}
	h = binary.LittleEndian.AppendUint16(h, uint16(8+len(body)))
	h = binary.LittleEndian.AppendUint16(h, uint16(len(aces)))
	h = append(h, 0, 0)
	return append(h, body...)
}

const (
	me     = "S-1-5-21-1-2-3-1001"
	other  = "S-1-5-21-1-2-3-1002"
	world  = "S-1-1-0"
	users  = "S-1-5-32-545"
	authn  = "S-1-5-11"
	system = "S-1-5-18"
	admins = "S-1-5-32-544"
)

func checkWin(t *testing.T, a Access, aces ...testACE) error {
	t.Helper()
	parsed, err := ParseWinACL(winACL(t, aces...))
	if err != nil {
		t.Fatal(err)
	}
	trusted := map[string]bool{me: true, system: true, admins: true}
	return CheckWinACL(parsed, WinACLPolicy{Access: a, Trusted: func(s []byte) bool { return trusted[SIDString(s)] }})
}

func TestSIDString(t *testing.T) {
	for _, s := range []string{world, me, "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464", "S-1-15-2-1"} {
		if got := SIDString(sid(t, s)); got != s {
			t.Errorf("%s: %s", s, got)
		}
	}
	if SIDString([]byte{1, 5, 0}) != "(invalid SID)" {
		t.Error("a short SID")
	}
}

// The DACL policy: only the trusted principals may hold a refused right;
// deny ACEs are not credited; inherit-only ACEs do not apply; inherited
// ACEs count as explicit ones.
func TestCritical_WinACLPolicy(t *testing.T) {
	full := uint32(0x1f01ff)
	read := uint32(0x120089) // FILE_GENERIC_READ
	ok := func(a Access, aces ...testACE) {
		t.Helper()
		if err := checkWin(t, a, aces...); err != nil {
			t.Errorf("refused: %v", err)
		}
	}
	bad := func(want string, a Access, aces ...testACE) {
		t.Helper()
		err := checkWin(t, a, aces...)
		if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
	private := []testACE{{0, 0, full, me}, {0, winInheritedACE, full, system}, {0, 0, full, admins}}
	ok(Private, private...)
	ok(Private) // an empty DACL: nobody has access
	for _, who := range []string{world, users, authn, other, "S-1-3-0"} {
		bad(who, Private, append(private, testACE{0, 0, read, who})...)
		bad(who, Private, append(private, testACE{0, 0, winGenericRead, who})...)
		bad(who, Private, append(private, testACE{0, 0, winFileReadData, who})...)
		bad(who, Private, append(private, testACE{0, 0, winGenericAll, who})...)
		bad(who, Private, append(private, testACE{0, 0, winMaximumAllowed, who})...)
	}
	bad("Everyone", Private, append(private, testACE{0, 0, read, world})...)
	bad("inherited", Private, append(private, testACE{0, winInheritedACE, read, world})...)
	// a deny ACE in front of the grant would deny it: not credited
	bad("Everyone", Private, append([]testACE{{1, 0, full, world}}, append(private, testACE{0, 0, read, world})...)...)
	// a deny ACE alone grants nothing
	ok(Private, append(private, testACE{1, 0, full, world})...)
	// reading the attributes and the security descriptor is no access
	ok(Private, append(private, testACE{0, 0, winReadControl | winSynchronize | winFileReadAttributes, world})...)
	// inherit-only: for the children of a directory
	ok(Private, append(private, testACE{0, winInheritOnlyACE, full, world})...)
	// object, callback and compound allow ACEs are not evaluated
	for _, typ := range []byte{winAccessAllowedObject, winAccessAllowedCallback, winAccessAllowedCallbackObject, winAccessAllowedCompound} {
		bad("does not evaluate", Private, append(private, testACE{typ, 0, read, me})...)
	}
	for _, typ := range []byte{winSystemAudit, winSystemAlarm, 0x11, 0x7f} {
		bad("does not know", Private, append(private, testACE{typ, 0, read, me})...)
	}
	// a plugin: others may read and run it, not change it
	ok(NoWrite, append(private, testACE{0, 0, read | winFileExecute, users})...)
	for _, m := range []uint32{winFileWriteData, winFileAppendData, winDelete, winWriteDAC, winWriteOwner, winGenericWrite, winGenericAll, winFileDeleteChild, 0x1301bf} {
		bad("can change", NoWrite, append(private, testACE{0, 0, m, users})...)
	}
	// a directory above it: others may add entries (C:\), not replace them
	ok(NoReplace, append(private, testACE{0, 0, winFileAppendData | winFileWriteData | read, authn})...)
	for _, m := range []uint32{winFileDeleteChild, winDelete, winWriteDAC, winWriteOwner, winGenericAll} {
		bad("remove or replace", NoReplace, append(private, testACE{0, 0, m, authn})...)
	}
}

func TestParseWinACLBounds(t *testing.T) {
	good := winACL(t, testACE{0, 0, 1, world})
	if _, err := ParseWinACL(good); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string][]byte{
		"empty":      nil,
		"revision":   append([]byte{9}, good[1:]...),
		"size":       append(append([]byte(nil), good[:2]...), append([]byte{0xff, 0}, good[4:]...)...),
		"count":      append(append([]byte(nil), good[:4]...), append([]byte{5, 0}, good[6:]...)...),
		"truncated":  good[:len(good)-1],
		"ace size 0": append(append([]byte(nil), good[:10]...), append([]byte{0, 0}, good[12:]...)...),
		"sid count":  append(append([]byte(nil), good[:17]...), append([]byte{15}, good[18:]...)...),
	} {
		if _, err := ParseWinACL(b); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// filesec encodes the getattrlist buffer of a kauth_filesec with aces.
func filesec(aces ...DarwinACE) []byte {
	fs := binary.LittleEndian.AppendUint32(nil, kauthFilesecMagic)
	fs = append(fs, make([]byte, 32)...)
	fs = binary.LittleEndian.AppendUint32(fs, uint32(len(aces)))
	fs = binary.LittleEndian.AppendUint32(fs, 0)
	for _, a := range aces {
		fs = append(fs, a.Applicable[:]...)
		fs = binary.LittleEndian.AppendUint32(fs, a.Flags)
		fs = binary.LittleEndian.AppendUint32(fs, a.Rights)
	}
	buf := binary.LittleEndian.AppendUint32(nil, uint32(12+len(fs)))
	buf = binary.LittleEndian.AppendUint32(buf, 8) // offset from the attrreference
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(fs)))
	return append(buf, fs...)
}

var everyoneGUID = [16]byte{0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef, 0, 0, 0, 12}

func checkDarwin(t *testing.T, buf []byte, a Access) error {
	t.Helper()
	fs, err := ParseAttrlistFilesec(buf)
	if err != nil {
		t.Fatal(err)
	}
	aces, err := ParseFilesec(fs)
	if err != nil {
		t.Fatal(err)
	}
	return CheckDarwinACL(aces, a)
}

// The macOS policy: an allow entry that grants a refused right is refused
// whoever it names (fail closed); deny entries and only-inherit ones do not
// apply.
func TestCritical_DarwinACLPolicy(t *testing.T) {
	noACL := []byte{12, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0}
	if err := checkDarwin(t, noACL, Private); err != nil {
		t.Fatal(err)
	}
	// the buffer of `chmod +a "everyone allow read"`, as macOS returns it
	if err := checkDarwin(t, filesec(DarwinACE{everyoneGUID, kauthACEPermit, kauthReadData}), Private); !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "group:everyone") {
		t.Fatalf("everyone allow read: %v", err)
	}
	me := [16]byte{0xff, 0xff, 0xee, 0xee, 0xdd, 0xdd, 0xcc, 0xcc, 0xbb, 0xbb, 0xaa, 0xaa, 0, 0, 0x01, 0xf5}
	if err := checkDarwin(t, filesec(DarwinACE{me, kauthACEPermit | kauthACEInherited, kauthReadData}), Private); err == nil || !strings.Contains(err.Error(), "uid 501") || !strings.Contains(err.Error(), "inherited") {
		t.Fatalf("an inherited entry: %v", err)
	}
	for _, ok := range []DarwinACE{
		{everyoneGUID, kauthACEDeny, 0xffffffff},
		{everyoneGUID, kauthACEPermit | kauthACEOnlyInherit, kauthReadData},
		{everyoneGUID, kauthACEPermit, kauthReadAttributes | kauthReadSecurity | kauthReadExtAttrs},
		{everyoneGUID, kauthACEAudit, kauthReadData},
	} {
		if err := checkDarwin(t, filesec(ok), Private); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
	if err := checkDarwin(t, filesec(DarwinACE{everyoneGUID, 7, 0}), Private); err == nil {
		t.Error("an unknown kind was accepted")
	}
	// a plugin and its directories: reading and running are fine
	if err := checkDarwin(t, filesec(DarwinACE{everyoneGUID, kauthACEPermit, kauthReadData | kauthExecute | kauthGenericRead | kauthGenericExecute}), NoWrite); err != nil {
		t.Error(err)
	}
	for _, r := range []uint32{kauthWriteData, kauthAppendData, kauthDelete, kauthDeleteChild, kauthWriteSecurity, kauthChangeOwner, kauthGenericAll, kauthGenericWrite} {
		if err := checkDarwin(t, filesec(DarwinACE{everyoneGUID, kauthACEPermit, r}), NoWrite); err == nil {
			t.Errorf("rights 0x%x on a plugin", r)
		}
	}
	if err := checkDarwin(t, filesec(DarwinACE{everyoneGUID, kauthACEPermit, kauthWriteData | kauthAppendData}), NoReplace); err != nil {
		t.Error(err)
	}
	if err := checkDarwin(t, filesec(DarwinACE{everyoneGUID, kauthACEPermit, kauthDeleteChild}), NoReplace); err == nil {
		t.Error("delete_child above a plugin")
	}
}

func TestParseFilesecBounds(t *testing.T) {
	good := filesec(DarwinACE{everyoneGUID, kauthACEPermit, kauthReadData})
	for name, b := range map[string][]byte{
		"short":    good[:8],
		"total":    append(binary.LittleEndian.AppendUint32(nil, 4096), good[4:]...),
		"offset":   append(append(append([]byte(nil), good[:4]...), 0xf0, 0xff, 0xff, 0xff), good[8:]...),
		"overflow": append(append(append([]byte(nil), good[:8]...), 0xff, 0, 0, 0), good[12:]...),
	} {
		if _, err := ParseAttrlistFilesec(b); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	fs, err := ParseAttrlistFilesec(good)
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]byte(nil), fs...)
	bad[0] = 0
	if _, err := ParseFilesec(bad); err == nil {
		t.Error("a bad magic")
	}
	bad = append([]byte(nil), fs...)
	binary.LittleEndian.PutUint32(bad[36:], 2)
	if _, err := ParseFilesec(bad); err == nil {
		t.Error("a count beyond the buffer")
	}
	binary.LittleEndian.PutUint32(bad[36:], kauthFilesecNoACL)
	if aces, err := ParseFilesec(bad); err != nil || len(aces) != 0 {
		t.Errorf("no ACL: %v %v", aces, err)
	}
}

func posixACL(entries ...POSIXACE) []byte {
	b := binary.LittleEndian.AppendUint32(nil, posixACLVersion)
	for _, e := range entries {
		b = binary.LittleEndian.AppendUint16(b, e.Tag)
		b = binary.LittleEndian.AppendUint16(b, e.Perm)
		b = binary.LittleEndian.AppendUint32(b, e.ID)
	}
	return b
}

// The POSIX ACL policy: what the entries grant once limited by the mask.
func TestCritical_POSIXACLPolicy(t *testing.T) {
	const uid = 1000
	check := func(p POSIXPolicy, entries ...POSIXACE) error {
		t.Helper()
		aces, err := ParsePOSIXACL(posixACL(entries...))
		if err != nil {
			t.Fatal(err)
		}
		p.UID = uid
		return CheckPOSIXACL(aces, p)
	}
	base := []POSIXACE{{posixACLUserObj, 6, 0}, {posixACLGroupObj, 0, 0}, {posixACLOther, 0, 0}}
	if err := check(POSIXPolicy{Access: Private}, base...); err != nil {
		t.Fatal(err)
	}
	// setfacl -m u:1001:r (the mask becomes r--)
	err := check(POSIXPolicy{Access: Private}, append(base, POSIXACE{posixACLUser, 4, 1001}, POSIXACE{posixACLMask, 4, 0})...)
	if !errors.Is(err, ErrNotPrivate) || !strings.Contains(err.Error(), "user:1001") {
		t.Fatalf("a named user: %v", err)
	}
	// ... then chmod 600: the mask is ---, the entry grants nothing
	if err := check(POSIXPolicy{Access: Private}, append(base, POSIXACE{posixACLUser, 4, 1001}, POSIXACE{posixACLMask, 0, 0})...); err != nil {
		t.Fatalf("a masked entry: %v", err)
	}
	if err := check(POSIXPolicy{Access: Private}, append(base, POSIXACE{posixACLGroup, 4, 20}, POSIXACE{posixACLMask, 4, 0})...); err == nil || !strings.Contains(err.Error(), "group:20") {
		t.Fatalf("a named group: %v", err)
	}
	// the user's own entry is no exposure
	if err := check(POSIXPolicy{Access: Private}, append(base, POSIXACE{posixACLUser, 6, uid}, POSIXACE{posixACLMask, 6, 0})...); err != nil {
		t.Fatal(err)
	}
	if err := check(POSIXPolicy{Access: Private}, POSIXACE{posixACLUserObj, 6, 0}, POSIXACE{posixACLOther, 4, 0}); err == nil {
		t.Fatal("others can read")
	}
	if err := check(POSIXPolicy{Access: Private}, append(base, POSIXACE{0x40, 0, 0})...); err == nil {
		t.Fatal("an unknown tag")
	}
	// a plugin: others may read, not write; root may
	if err := check(POSIXPolicy{Access: NoWrite, RootTrusted: true}, append(base, POSIXACE{posixACLUser, 7, 0}, POSIXACE{posixACLGroup, 5, 20}, POSIXACE{posixACLMask, 7, 0})...); err != nil {
		t.Fatal(err)
	}
	if err := check(POSIXPolicy{Access: NoWrite, RootTrusted: true}, append(base, POSIXACE{posixACLGroup, 2, 20}, POSIXACE{posixACLMask, 7, 0})...); err == nil {
		t.Fatal("a group can write to a plugin")
	}
	if err := check(POSIXPolicy{Access: NoReplace, Sticky: true}, append(base, POSIXACE{posixACLGroup, 7, 20}, POSIXACE{posixACLMask, 7, 0})...); err != nil {
		t.Fatalf("a sticky directory above: %v", err)
	}
	if err := check(POSIXPolicy{Access: NoReplace}, append(base, POSIXACE{posixACLGroup, 7, 20}, POSIXACE{posixACLMask, 7, 0})...); err == nil {
		t.Fatal("a writable directory above")
	}
	for _, b := range [][]byte{nil, {1, 0, 0, 0}, append(posixACL(base...), 1)} {
		if _, err := ParsePOSIXACL(b); err == nil {
			t.Errorf("%x: no error", b)
		}
	}
}
