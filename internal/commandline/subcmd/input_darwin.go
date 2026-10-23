package subcmd

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// fifoEndPolled: poll(2) and kqueue on macOS do not tell that the writer of
// a FIFO closed (select does, below FD_SETSIZE only), so a FIFO is read by
// readFIFO, which looks again every fifoRecheck.
const fifoEndPolled = false

// fifoPath is the path of the named FIFO f, to open it again; an anonymous
// pipe has none (and poll tells its end).
func fifoPath(f *os.File) (string, bool) {
	buf := make([]byte, unix.PathMax) // on the heap: fcntl gets its address
	_, err := unix.FcntlInt(f.Fd(), unix.F_GETPATH, int(uintptr(unsafe.Pointer(&buf[0]))))
	runtime.KeepAlive(buf)
	if err != nil {
		return "", false
	}
	return unix.ByteSliceToString(buf), true
}
