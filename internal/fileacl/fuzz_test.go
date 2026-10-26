package fileacl

import (
	"testing"
)

// The parsers read what the OS returns; they must reject, never panic on,
// whatever bytes they get, and the checks must run on whatever they parse.

func FuzzParseWinACL(f *testing.F) {
	f.Add(winACL(f, testACE{0, 0, 0x1f01ff, me}, testACE{1, 0x10, 1, world}, testACE{0, 0x08, 1, users}))
	f.Add([]byte{2, 0, 8, 0, 0, 0, 0, 0})
	f.Add([]byte{4, 0, 16, 0, 1, 0, 0, 0, 5, 0, 8, 0, 1, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		aces, err := ParseWinACL(b)
		if err != nil {
			return
		}
		for _, a := range []Access{Private, NoWrite, NoReplace} {
			_ = CheckWinACL(aces, WinACLPolicy{Access: a, Trusted: func(s []byte) bool { return SIDString(s) == me }})
		}
		for _, ace := range aces {
			_ = SIDString(ace.SID)
		}
	})
}

func FuzzParseFilesec(f *testing.F) {
	f.Add(filesec(DarwinACE{everyoneGUID, kauthACEPermit, kauthReadData}))
	f.Add(filesec())
	f.Add([]byte{12, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		fs, err := ParseAttrlistFilesec(b)
		if err != nil {
			return
		}
		aces, err := ParseFilesec(fs)
		if err != nil {
			return
		}
		for _, a := range []Access{Private, NoWrite, NoReplace} {
			_ = CheckDarwinACL(aces, a)
		}
	})
}

func FuzzParsePOSIXACL(f *testing.F) {
	f.Add(posixACL(POSIXACE{posixACLUserObj, 6, 0}, POSIXACE{posixACLUser, 4, 1001}, POSIXACE{posixACLMask, 4, 0}, POSIXACE{posixACLOther, 0, 0}))
	f.Add([]byte{2, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		aces, err := ParsePOSIXACL(b)
		if err != nil {
			return
		}
		for _, a := range []Access{Private, NoWrite, NoReplace} {
			_ = CheckPOSIXACL(aces, POSIXPolicy{Access: a, UID: 1000, RootTrusted: true})
		}
	})
}
