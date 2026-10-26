// Trampolines to the libSystem functions of getattrlist_darwin.go (the
// form golang.org/x/sys/unix uses).

//go:build darwin

#include "textflag.h"

TEXT libc_fgetattrlist_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_fgetattrlist(SB)
GLOBL	·libc_fgetattrlist_trampoline_addr(SB), RODATA, $8
DATA	·libc_fgetattrlist_trampoline_addr(SB)/8, $libc_fgetattrlist_trampoline<>(SB)

TEXT libc_getattrlist_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_getattrlist(SB)
GLOBL	·libc_getattrlist_trampoline_addr(SB), RODATA, $8
DATA	·libc_getattrlist_trampoline_addr(SB)/8, $libc_getattrlist_trampoline<>(SB)
