//go:build windows

package configure

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/magicdrive/kjak/internal/fileacl"
)

// openSecrets opens a secrets file for reading with CreateFile:
//   - with noFollow, FILE_FLAG_OPEN_REPARSE_POINT: a symbolic link, a
//     junction or another reparse point at path is opened itself (not
//     followed) and refused by checkPrivateFile, on the handle: no
//     check-then-use race;
//   - share mode FILE_SHARE_READ only: while kjak reads it, nobody can
//     write to the file, delete it or rename another file over it (the
//     content read is the content checked);
//   - SECURITY_SQOS_PRESENT|SECURITY_IDENTIFICATION: a named pipe given
//     as the path cannot impersonate the user (it is refused anyway, not
//     being a disk file).
func openSecrets(path string, noFollow bool) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.SECURITY_SQOS_PRESENT | windows.SECURITY_IDENTIFICATION)
	if noFollow {
		flags |= windows.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// checkPrivateFile checks the opened file, on its handle:
//   - a disk file, not a directory, not a reparse point (a link, with
//     noFollow; without it, links were followed to the file checked);
//   - with single (the project file), one name only (a hard link could be
//     the user's personal file under the project's name);
//   - its owner and its DACL (fileacl.CheckFile): owned by the user, and no
//     ACE (inherited or explicit) that gives anyone but the user, SYSTEM
//     and Administrators any access to the content; a null DACL, or an ACE
//     kjak cannot evaluate, is refused (fail closed).
func checkPrivateFile(f *os.File, path string, st os.FileInfo, single bool) error {
	h := windows.Handle(f.Fd())
	if t, err := windows.GetFileType(h); err != nil || t != windows.FILE_TYPE_DISK {
		return fmt.Errorf("%w: it is not a regular file", ErrInsecureSecrets)
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		return fmt.Errorf("%w: its attributes cannot be read: %v", ErrInsecureSecrets, err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return fmt.Errorf("%w: it is a symbolic link or a reparse point, which is refused here", ErrInsecureSecrets)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || !st.Mode().IsRegular() {
		return fmt.Errorf("%w: it is not a regular file", ErrInsecureSecrets)
	}
	if single && info.NumberOfLinks > 1 {
		return fmt.Errorf("%w: it has %d names (hard links), which is refused here (it could be your own secrets file under the project's name)", ErrInsecureSecrets, info.NumberOfLinks)
	}
	if err := fileacl.CheckFile(f, fileacl.Private); err != nil {
		return fmt.Errorf("%w: %w", ErrInsecureSecrets, err)
	}
	return nil
}

// createPrivate creates path exclusively (CREATE_NEW: never an existing
// file or link) with a protected DACL that gives access to the user, SYSTEM
// and Administrators only, whatever the directory would pass on.
func createPrivate(path string) (*os.File, error) {
	sddl, err := fileacl.PrivateSDDL()
	if err != nil {
		return nil, err
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return nil, err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// SecretsChecksPermissions: the owner and the DACL of a secrets file are
// checked.
const SecretsChecksPermissions = true
