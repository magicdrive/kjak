//go:build darwin

package external

import (
	"debug/macho"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Mach-O load commands that name a library, or change where libraries are
// found (debug/macho parses only LC_LOAD_DYLIB as *macho.Dylib).
const (
	lcLoadDylib       = 0xc
	lcLoadWeakDylib   = 0x80000018
	lcReexportDylib   = 0x8000001f
	lcLazyLoadDylib   = 0x20
	lcLoadUpwardDylib = 0x80000023
	lcRpath           = 0x8000001c
	lcDyldEnvironment = 0x27
)

// selfContained: r is a Mach-O (thin or universal) whose every library is
// the system's (/usr/lib/, /System/Library/), with no run path and no dyld
// environment: nothing it loads can be replaced from Homebrew's prefix. r
// is the opened, hashed file, read again at its offsets.
func selfContained(r io.ReaderAt) error {
	var files []*macho.File
	if fat, err := macho.NewFatFile(r); err == nil {
		for _, a := range fat.Arches {
			files = append(files, a.File)
		}
	} else if f, err := macho.NewFile(r); err == nil {
		files = append(files, f)
	} else {
		return errors.New("is not a Mach-O executable (a script's interpreter could be replaced by another admin user)")
	}
	for _, f := range files {
		if f.Type != macho.TypeExec {
			return errors.New("is not a Mach-O executable")
		}
		for _, l := range f.Loads {
			raw := l.Raw()
			if len(raw) < 8 {
				return errors.New("has a malformed load command")
			}
			switch cmd := f.ByteOrder.Uint32(raw); cmd {
			case lcRpath:
				return errors.New("has a run path (LC_RPATH): its libraries could come from elsewhere")
			case lcDyldEnvironment:
				return errors.New("sets a dyld environment (LC_DYLD_ENVIRONMENT)")
			case lcLoadDylib, lcLoadWeakDylib, lcReexportDylib, lcLazyLoadDylib, lcLoadUpwardDylib:
				name, err := dylibName(raw, f.ByteOrder)
				if err != nil {
					return err
				}
				if !strings.HasPrefix(name, "/usr/lib/") && !strings.HasPrefix(name, "/System/Library/") {
					return fmt.Errorf("loads %s, which another admin user could replace: only a program that loads system libraries alone runs from there", name)
				}
			}
		}
	}
	return nil
}

// dylibName is the path of a dylib load command (its name offset at 8).
func dylibName(raw []byte, bo binary.ByteOrder) (string, error) {
	if len(raw) < 12 {
		return "", errors.New("has a malformed library load command")
	}
	off := bo.Uint32(raw[8:])
	if off < 12 || int(off) >= len(raw) {
		return "", errors.New("has a malformed library load command")
	}
	name := raw[off:]
	if i := strings.IndexByte(string(name), 0); i >= 0 {
		name = name[:i]
	}
	if len(name) == 0 || strings.Contains(string(name), "..") {
		return "", fmt.Errorf("loads a library %q", name)
	}
	return string(name), nil
}
