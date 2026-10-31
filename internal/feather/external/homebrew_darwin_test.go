//go:build darwin

package external

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestHomebrewKeg(t *testing.T) {
	for real, want := range map[string]bool{
		"/opt/homebrew/Cellar/kjak-notify-macos/0.1.0/bin/kjak-feather-notify-macos": true,
		"/usr/local/Cellar/f/1.0_1/bin/kjak-feather-x":                               true,
		"/opt/homebrew/Cellar/f/1.0/libexec/kjak-feather-x":                          false, // not bin
		"/opt/homebrew/Cellar/f/1.0/bin/sub/kjak-feather-x":                          false,
		"/opt/homebrew/Cellar/f/bin/kjak-feather-x":                                  false, // no version
		"/opt/homebrew/Cellar/f/../g/1.0/bin/kjak-feather-x":                         false,
		"/opt/homebrew/Cellar/f//1.0/bin/kjak-feather-x":                             false,
		"/opt/homebrew/Cellar/./1.0/bin/kjak-feather-x":                              false,
		"/opt/homebrewX/Cellar/f/1.0/bin/kjak-feather-x":                             false,
		"/OPT/homebrew/Cellar/f/1.0/bin/kjak-feather-x":                              false, // other spelling
		"/home/linuxbrew/.linuxbrew/Cellar/f/1.0/bin/kjak-feather-x":                 false,
		"/opt/homebrew/bin/kjak-feather-x":                                           false,
		"/Users/me/.local/bin/kjak-feather-x":                                        false,
	} {
		if _, got := homebrewKeg(real); got != want {
			t.Errorf("%s: %v, want %v", real, got, want)
		}
	}
}

func TestHomebrewDirMode(t *testing.T) {
	me := os.Geteuid()
	st := func(uid, gid uint32) *syscall.Stat_t { return &syscall.Stat_t{Uid: uid, Gid: gid} }
	dir := os.ModeDir
	for _, c := range []struct {
		mode os.FileMode
		sys  any
		want bool
	}{
		{dir | 0o775, st(uint32(me), 80), true},
		{dir | 0o755, st(uint32(me), 80), true},
		{dir | 0o777, st(uint32(me), 80), false},   // others
		{dir | 0o775, st(uint32(me), 20), false},   // staff
		{dir | 0o775, st(uint32(me+1), 80), false}, // another user
		{dir | 0o775, st(0, 80), false},            // root: not Homebrew's
		{dir | os.ModeSetgid | 0o775, st(uint32(me), 80), false},
		{os.ModeSymlink | 0o775, st(uint32(me), 80), false},
		{0o775, st(uint32(me), 80), false}, // a file
		{dir | 0o775, nil, false},
	} {
		if got := homebrewDirMode(c.mode, c.sys, me); got != c.want {
			t.Errorf("%v %+v: %v, want %v", c.mode, c.sys, got, c.want)
		}
	}
	// kjak run as root: a root-owned group-writable directory is still not
	// Homebrew's
	if homebrewDirMode(dir|0o775, st(0, 80), 0) {
		t.Error("a root-owned directory was excepted for root")
	}
}

// brewTree is a Homebrew prefix as the installer and brew make it: the
// prefix 0755; bin, Cellar and opt 0775 of group admin; a keg
// Cellar/kjak-test/1.0/bin/kjak-feather-test (0555); bin/kjak-feather-test
// and opt/kjak-test links into it.
type brewTree struct {
	prefix, bin, cellar, opt, keg, kegBin, exe, link, optLink string
}

func chgrpAdmin(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.Lchown(p, -1, 80); err != nil {
			// CI's macOS runners are admin: there, a skip would hide the tests
			if os.Getenv("CI") != "" {
				t.Fatalf("cannot give %s to group admin: %v", p, err)
			}
			t.Skipf("cannot give %s to group admin (not a member?): %v", p, err)
		}
	}
}

func newBrewTree(t *testing.T) brewTree {
	t.Helper()
	prefix := filepath.Join(tempDir(t), "homebrew")
	b := brewTree{prefix: prefix, bin: prefix + "/bin", cellar: prefix + "/Cellar", opt: prefix + "/opt"}
	b.keg = b.cellar + "/kjak-test/1.0"
	b.kegBin = b.keg + "/bin"
	b.exe = install(t, b.kegBin, "test")
	b.link = b.bin + "/" + filepath.Base(b.exe)
	b.optLink = b.opt + "/kjak-test"
	for _, d := range []string{b.bin, b.opt} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("../Cellar/kjak-test/1.0/bin/"+filepath.Base(b.exe), b.link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Cellar/kjak-test/1.0", b.optLink); err != nil {
		t.Fatal(err)
	}
	chgrpAdmin(t, prefix, b.bin, b.cellar, b.opt, b.cellar+"/kjak-test", b.keg, b.kegBin, b.exe, b.link, b.optLink)
	for p, m := range map[string]os.FileMode{prefix: 0o755, b.bin: 0o775, b.cellar: 0o775, b.opt: 0o775, b.cellar + "/kjak-test": 0o755, b.keg: 0o755, b.kegBin: 0o755, b.exe: 0o555} {
		if err := os.Chmod(p, m); err != nil {
			t.Fatal(err)
		}
	}
	old := homebrewPrefixes
	homebrewPrefixes = []string{prefix}
	t.Cleanup(func() { homebrewPrefixes = old })
	return b
}

func acceptAll(real, sum string) error { return nil }

var errNo = errors.New("no")

func acceptNone(real, sum string) error { return errNo }

func wantRefused(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil || !errors.Is(err, ErrPlugin) || !strings.Contains(err.Error(), want) {
		t.Fatalf("want a refusal with %q, got %v", want, err)
	}
}

// The standard layout: refused by the strict checks (as kjak v1.1.0),
// accepted only through ResolveAccept with the content accepted, by the
// link in bin and by the opt path.
func TestCritical_HomebrewLayout(t *testing.T) {
	b := newBrewTree(t)
	_, err := Resolve("test", "", b.bin, runtime.GOOS)
	wantRefused(t, err, "its directory "+b.bin+" is writable by group or others")
	_, err = ResolveAccept("test", "", b.bin, runtime.GOOS, acceptNone)
	var he *HomebrewError
	if !errors.As(err, &he) || !errors.Is(err, ErrPlugin) || he.Real != b.exe || len(he.SHA256) != 64 || !errors.Is(he.Reason, errNo) {
		t.Fatalf("want a HomebrewError, got %v", err)
	}
	var gotReal, gotSum string
	exe, err := ResolveAccept("test", "", b.bin, runtime.GOOS, func(real, sum string) error { gotReal, gotSum = real, sum; return nil })
	if err != nil || !exe.Homebrew || exe.Real != b.exe || gotReal != b.exe || gotSum != exe.SHA256 {
		t.Fatalf("standard layout: %+v %v", exe, err)
	}
	if err := exe.Verify(false); err != nil {
		t.Fatalf("verify: %v", err)
	}
	// the opt path, pinned (path =)
	opt := b.optLink + "/bin/" + filepath.Base(b.exe)
	if exe, err := ResolveAccept("test", opt, "", runtime.GOOS, acceptAll); err != nil || !exe.Homebrew || exe.Real != b.exe {
		t.Fatalf("opt path: %+v %v", exe, err)
	}
	// Discover: never ok, the SHA-256 to pin
	found, _ := Discover(b.bin, runtime.GOOS)
	if len(found) != 1 || !found[0].Homebrew || found[0].SHA256 != exe.SHA256 || !strings.Contains(found[0].Problem, "pins its sha256") {
		t.Fatalf("discover: %+v", found)
	}
}

// A plugin outside Homebrew is checked exactly as before, whatever accept
// says.
func TestCritical_HomebrewExceptionNotElsewhere(t *testing.T) {
	root := tempDir(t)
	dir := filepath.Join(root, "bin")
	install(t, dir, "test")
	chgrpAdmin(t, dir)
	if err := os.Chmod(dir, 0o775); err != nil {
		t.Fatal(err)
	}
	old := homebrewPrefixes
	homebrewPrefixes = []string{root}
	t.Cleanup(func() { homebrewPrefixes = old })
	// root/bin is a prefix's bin, but the file is not in a keg
	_, err := ResolveAccept("test", "", dir, runtime.GOOS, acceptAll)
	wantRefused(t, err, "its directory "+dir+" is writable by group or others")
	if errors.As(err, new(*HomebrewError)) {
		t.Fatal("a plugin outside a keg got the Homebrew exception")
	}
}

// Replacements and changes another member of admin (or a mistake) could
// make: each one is refused, at resolution or before the run.
func TestCritical_HomebrewTampering(t *testing.T) {
	// want: the refusal through PATH (bin); optWant: through the pinned opt
	// path ("same": want; "": accepted there, the change is in bin only)
	type tc struct {
		name, want, optWant string
		change              func(t *testing.T, b brewTree)
	}
	cases := []tc{
		{"Cellar writable by others", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.cellar, 0o777) }},
		{"bin writable by others", "writable by group or others", "", func(t *testing.T, b brewTree) { mustChmod(t, b.bin, 0o777) }},
		{"Cellar of group staff", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChgrp(t, b.cellar, 20) }},
		{"keg bin writable by group", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.kegBin, 0o775) }},
		{"keg version writable by group", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.keg, 0o775) }},
		{"formula directory writable by group", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.cellar+"/kjak-test", 0o775) }},
		{"prefix writable by group", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.prefix, 0o775) }},
		{"plugin writable by group", "writable by group", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.exe, 0o575) }},
		{"Cellar setgid", "writable by group or others", "same", func(t *testing.T, b brewTree) { mustChmod(t, b.cellar, 0o775|os.ModeSetgid) }},
		{"Cellar ACL delete_child", "above it", "same", func(t *testing.T, b brewTree) { chmodACL(t, "+a", "everyone allow delete_child", b.cellar) }},
		{"bin ACL add_file", "its directory", "", func(t *testing.T, b brewTree) { chmodACL(t, "+a", "everyone allow add_file", b.bin) }},
		{"Cellar a link to another directory", "writable by group or others", "same", func(t *testing.T, b brewTree) {
			moved := b.prefix + "/Cellar.real"
			if err := os.Rename(b.cellar, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("Cellar.real", b.cellar); err != nil {
				t.Fatal(err)
			}
			mustChmod(t, b.prefix, 0o755)
		}},
		{"bin a link to another directory", "writable by group or others", "", func(t *testing.T, b brewTree) {
			moved := b.prefix + "/bin.real"
			if err := os.Rename(b.bin, moved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("bin.real", b.bin); err != nil {
				t.Fatal(err)
			}
		}},
		{"link in bin to a file outside a keg", "writable by group or others", "", func(t *testing.T, b brewTree) {
			other := install(t, filepath.Join(b.prefix, "..", "elsewhere"), "test")
			mustRelink(t, b.link, other)
		}},
		{"a script (its interpreter could be replaced)", "is not a Mach-O executable", "same", func(t *testing.T, b brewTree) {
			replaceExe(t, b, []byte("#!/bin/sh\nexit 0\n"))
		}},
		{"a Mach-O loading a library outside the system's", "loads /tmp/lib/libSystem.B.dylib", "same", func(t *testing.T, b brewTree) {
			data := mustRead(t, b.exe)
			old := []byte("/usr/lib/libSystem.B.dylib")
			i := bytes.Index(data, old)
			if i < 0 {
				t.Skip("the test plugin does not load libSystem by that name")
			}
			copy(data[i:], "/tmp/lib/libSystem.B.dylib")
			replaceExe(t, b, data)
		}},
		{"a Mach-O naming another dynamic linker", "names the dynamic linker /tmp/lib/dyld", "same", func(t *testing.T, b brewTree) {
			data := mustRead(t, b.exe)
			i := bytes.Index(data, []byte("/usr/lib/dyld\x00"))
			if i < 0 {
				t.Skip("the test plugin names no dynamic linker")
			}
			copy(data[i:], "/tmp/lib/dyld")
			replaceExe(t, b, data)
		}},
		{"a keg file of another name, pinned through opt", "writable by group or others", "is not named kjak-feather-test", func(t *testing.T, b brewTree) {
			other := b.kegBin + "/other-program"
			if err := os.Rename(b.exe, other); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("other-program", b.exe); err != nil {
				t.Fatal(err)
			}
			chgrpAdmin(t, b.exe)
		}},
		{"prefix not standard", "writable by group or others", "same", func(t *testing.T, b brewTree) { homebrewPrefixes = []string{b.prefix + "X"} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := newBrewTree(t)
			c.change(t, b)
			_, err := ResolveAccept("test", "", b.bin, runtime.GOOS, acceptAll)
			wantRefused(t, err, c.want)
			// through the pinned opt path
			_, err = ResolveAccept("test", b.optLink+"/bin/"+filepath.Base(b.exe), "", runtime.GOOS, acceptAll)
			switch c.optWant {
			case "":
				if err != nil {
					t.Fatalf("opt path, the change is in bin only: %v", err)
				}
			case "same":
				wantRefused(t, err, c.want)
			default:
				wantRefused(t, err, c.optWant)
			}
		})
	}
}

// After a successful resolution, a change before the run is refused by
// Verify, even when the caller would not hash again.
func TestCritical_HomebrewVerify(t *testing.T) {
	for name, change := range map[string]func(*testing.T, brewTree){
		"keg replaced (another formula version)": func(t *testing.T, b brewTree) {
			other := install(t, b.cellar+"/kjak-test/2.0/bin", "test")
			if err := os.WriteFile(other, append(mustRead(t, other), 0), 0o755); err != nil {
				t.Fatal(err)
			}
			mustRelink(t, b.link, other)
		},
		"content changed, same file": func(t *testing.T, b brewTree) {
			mustChmod(t, b.exe, 0o755)
			f, err := os.OpenFile(b.exe, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.Write([]byte{0})
			_ = f.Close()
			mustChmod(t, b.exe, 0o555)
		},
		"Cellar made writable by others": func(t *testing.T, b brewTree) { mustChmod(t, b.cellar, 0o777) },
		// only the hash tells: same file, size and modification time
		"content changed, same size and time": func(t *testing.T, b brewTree) {
			st, err := os.Stat(b.exe)
			if err != nil {
				t.Fatal(err)
			}
			data := mustRead(t, b.exe)
			data[len(data)-1] ^= 0xff
			mustChmod(t, b.exe, 0o755)
			f, err := os.OpenFile(b.exe, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt(data[len(data)-1:], int64(len(data)-1)); err != nil {
				t.Fatal(err)
			}
			_ = f.Close()
			mustChmod(t, b.exe, 0o555)
			if err := os.Chtimes(b.exe, st.ModTime(), st.ModTime()); err != nil {
				t.Fatal(err)
			}
		},
		// only the link tells: an identical copy in another keg
		"link moved to an identical copy": func(t *testing.T, b brewTree) {
			copyDir := b.cellar + "/kjak-test/2.0/bin"
			if err := os.MkdirAll(copyDir, 0o755); err != nil {
				t.Fatal(err)
			}
			cp := copyDir + "/" + filepath.Base(b.exe)
			if err := os.WriteFile(cp, mustRead(t, b.exe), 0o555); err != nil {
				t.Fatal(err)
			}
			mustRelink(t, b.link, cp)
		},
		"keg directory swapped": func(t *testing.T, b brewTree) {
			if err := os.Rename(b.keg, b.keg+".old"); err != nil {
				t.Fatal(err)
			}
			install(t, b.kegBin, "test")
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBrewTree(t)
			exe, err := ResolveAccept("test", "", b.bin, runtime.GOOS, acceptAll)
			if err != nil {
				t.Fatal(err)
			}
			change(t, b)
			if err := exe.Verify(false); err == nil || !errors.Is(err, ErrPlugin) {
				t.Fatalf("verify after %s: %v", name, err)
			}
		})
	}
}

// replaceExe writes data as the keg's plugin (0555, group admin).
func replaceExe(t *testing.T, b brewTree, data []byte) {
	t.Helper()
	mustChmod(t, b.exe, 0o755)
	if err := os.WriteFile(b.exe, data, 0o755); err != nil {
		t.Fatal(err)
	}
	mustChmod(t, b.exe, 0o555)
}

func mustChmod(t *testing.T, p string, m os.FileMode) {
	t.Helper()
	if err := os.Chmod(p, m); err != nil {
		t.Fatal(err)
	}
}

func mustChgrp(t *testing.T, p string, gid int) {
	t.Helper()
	if err := os.Lchown(p, -1, gid); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustRelink(t *testing.T, link, target string) {
	t.Helper()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("/usr/sbin/chown", "-h", ":admin", link).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}
