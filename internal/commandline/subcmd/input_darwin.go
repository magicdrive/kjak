package subcmd

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// fifoNeedsWake: poll(2) and kqueue on macOS do not tell that the writer
// of a FIFO closed (select does, below FD_SETSIZE only), so a FIFO with a
// path is read with blocking reads that the end of the context wakes
// (readFIFO).
const fifoNeedsWake = true

// fifoPath is the path of the named FIFO f; an anonymous pipe has none (and
// poll tells its end).
func fifoPath(f *os.File) (string, bool) {
	buf := make([]byte, unix.PathMax) // on the heap: fcntl gets its address
	_, err := unix.FcntlInt(f.Fd(), unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buf[0]))))
	runtime.KeepAlive(buf)
	if err != nil {
		return "", false
	}
	return unix.ByteSliceToString(buf), true
}
