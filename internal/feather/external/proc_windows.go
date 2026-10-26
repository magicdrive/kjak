//go:build windows

package external

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/magicdrive/kjak/internal/fileacl"
)

// checkFile checks an executable: a regular .exe file, owned by the user,
// SYSTEM, Administrators or TrustedInstaller, that nobody else can change
// (its DACL: fileacl.NoWrite).
func checkFile(f *os.File, real string, st os.FileInfo) error {
	if !st.Mode().IsRegular() {
		return errors.New("not a regular file")
	}
	if !strings.EqualFold(real[max(0, len(real)-4):], ".exe") {
		return errors.New("not an .exe file")
	}
	return fileacl.CheckFile(f, fileacl.NoWrite)
}

func openNoBlock(path string) (*os.File, error) { return os.Open(path) }

// checkPlace checks where a plugin is, by the owners and the DACLs: the
// directory that holds the name and the directory of the file it leads to
// must not be changeable by anyone else (fileacl.NoWrite); in every
// directory above them, nobody else may remove or replace an entry or
// change the DACL (fileacl.NoReplace: creating a new entry is allowed, as
// the "create folders" right of Users on C:\).
func checkPlace(path, real string) error {
	for _, dir := range []string{filepath.Dir(path), filepath.Dir(real)} {
		if err := fileacl.CheckPath(dir, fileacl.NoWrite); err != nil {
			return fmt.Errorf("its directory %s: %v", dir, err)
		}
		for d := filepath.Dir(dir); ; d = filepath.Dir(d) {
			if err := fileacl.CheckPath(d, fileacl.NoReplace); err != nil {
				return fmt.Errorf("the directory %s above it: %v", d, err)
			}
			if d == filepath.Dir(d) {
				break
			}
		}
	}
	return nil
}

// containment is the Job Object of a plugin. The job has
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and no breakaway right: every process
// the plugin starts is in it, and terminating the job (a cancel, the end of
// the run) or closing its last handle (kjak exiting, even by a crash)
// kills them all.
//
// os/exec cannot create a process in a job (syscall.SysProcAttr has no
// PROC_THREAD_ATTRIBUTE_JOB_LIST), so the plugin is created suspended
// (CREATE_SUSPENDED), assigned to the job, and only then resumed: it runs
// no instruction, and so starts no child, outside the job. Its main thread
// is resumed through a thread snapshot (os/exec closes the thread handle
// CreateProcess returned). If the assignment fails, the suspended process
// is killed: a plugin never runs outside a job.
//
// Limits (docs/security.md): a process started on the plugin's behalf by
// another process (a service, WMI, the task scheduler, a COM server) is
// not in the job.
type containment struct {
	cmd *exec.Cmd
	mu  sync.Mutex
	job windows.Handle // 0 once closed
}

// newContainment creates the job and prepares cmd to start suspended.
func newContainment(cmd *exec.Cmd) (*containment, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	c := &containment{cmd: cmd, job: job}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	cmd.Cancel = c.kill
	return c, nil
}

// start starts the plugin suspended, puts it in the job and resumes it.
func (c *containment) start() error {
	if err := c.cmd.Start(); err != nil {
		c.close()
		return err
	}
	if err := c.assignAndResume(uint32(c.cmd.Process.Pid)); err != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
		c.close()
		return fmt.Errorf("the plugin could not be put in a job object: %w", err)
	}
	return nil
}

func (c *containment) assignAndResume(pid uint32) error {
	// os.Process holds a handle to the process until Wait: the pid
	// cannot be reused meanwhile
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return fmt.Errorf("OpenProcess: %w", err)
	}
	defer windows.CloseHandle(h)
	c.mu.Lock()
	job := c.job
	c.mu.Unlock()
	if job == 0 {
		return errors.New("the job was closed")
	}
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return resumeThreads(pid)
}

// resumeThreads resumes the threads of a process created suspended (its
// main thread: it has no other yet).
func resumeThreads(pid uint32) error {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", err)
	}
	defer windows.CloseHandle(snap)
	te := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := 0
	for err = windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID != pid {
			continue
		}
		th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
		if err != nil {
			return fmt.Errorf("OpenThread: %w", err)
		}
		_, rerr := windows.ResumeThread(th)
		_ = windows.CloseHandle(th)
		if rerr != nil {
			return fmt.Errorf("ResumeThread: %w", rerr)
		}
		resumed++
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("Thread32Next: %w", err)
	}
	if resumed == 0 {
		return errors.New("no thread of the plugin was found to resume")
	}
	return nil
}

// kill terminates every process of the job, and the plugin itself (in
// case the cancel came before it was assigned to the job).
func (c *containment) kill() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	if c.job != 0 {
		err = windows.TerminateJobObject(c.job, 1)
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	return err
}

// close terminates what is left in the job once the plugin ended and
// releases the job's handle (KILL_ON_JOB_CLOSE kills anything left too).
func (c *containment) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.job == 0 {
		return
	}
	_ = windows.TerminateJobObject(c.job, 1)
	_ = windows.CloseHandle(c.job)
	c.job = 0
}

// GroupKill: the whole Job Object of a plugin is killed.
const GroupKill = true
