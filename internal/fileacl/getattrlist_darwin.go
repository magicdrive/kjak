//go:build darwin

package fileacl

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// getattrlist(2) and fgetattrlist(2) through libSystem, as golang.org/x/sys
// calls the functions it wraps (it wraps setattrlist, not these): a
// trampoline to the libSystem function (getattrlist_darwin_*.s) called by
// the runtime's syscall.syscall6. No cgo, and no raw system call (Apple
// supports libSystem, not the system call numbers).

//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

func fgetattrlist(fd uintptr, attrs *unix.Attrlist, buf []byte) error {
	_, _, e := syscall_syscall6(libc_fgetattrlist_trampoline_addr, fd,
		uintptr(unsafe.Pointer(attrs)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	if e != 0 {
		return e
	}
	return nil
}

func getattrlist(path string, attrs *unix.Attrlist, buf []byte) error {
	p, err := unix.BytePtrFromString(path)
	if err != nil {
		return err
	}
	_, _, e := syscall_syscall6(libc_getattrlist_trampoline_addr, uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(attrs)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	if e != 0 {
		return e
	}
	return nil
}

var libc_fgetattrlist_trampoline_addr uintptr

//go:cgo_import_dynamic libc_fgetattrlist fgetattrlist "/usr/lib/libSystem.B.dylib"

var libc_getattrlist_trampoline_addr uintptr

//go:cgo_import_dynamic libc_getattrlist getattrlist "/usr/lib/libSystem.B.dylib"
