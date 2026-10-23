package subcmd

import (
	"os"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// fifoEndPolled: poll(2) and kqueue on macOS do not tell that the writer of
// a FIFO closed (select does, below FD_SETSIZE only), so a FIFO is read by
// readFIFO: in select, else looking again every fifoRecheck.
const fifoEndPolled = false

// selectFIFO waits in select(2) until the FIFO fd or canceled is readable.
// Unlike poll and kqueue, select tells that a writer closed, whether it
// wrote or not, until a read of 0 (which forgets it). ok is false where
// select cannot wait for them: a descriptor of FD_SETSIZE or more.
func selectFIFO(fd, canceled int) (isCanceled, ok bool, err error) {
	if fd >= unix.FD_SETSIZE || canceled >= unix.FD_SETSIZE {
		return false, false, nil
	}
	var r unix.FdSet
	r.Set(fd)
	r.Set(canceled)
	if _, err := unix.Select(max(fd, canceled)+1, &r, nil, nil, nil); err != nil {
		return false, true, err
	}
	return r.IsSet(canceled), true, nil
}

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
