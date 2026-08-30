//go:build windows

// Windows job objects make the operating system kill the entire process tree
// when the job handle is closed (KILL_ON_JOB_CLOSE). That way a launcher that
// exits, restarts or crashes leaves no orphaned processes behind
// (ShaderCompileWorker, CrashReportClient, …).
//
// We call the Win32 API directly via syscall.NewLazyDLL — no external
// dependency, so the cross-compile from Linux works offline.
package supervisor

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObject          = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
)

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x2000
	processTerminate                  = 0x0001
	processSetQuota                   = 0x0100
)

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectBasicLimitInfo struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobObjectExtendedLimitInfo struct {
	BasicLimitInformation jobObjectBasicLimitInfo
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// Job wraps a Windows job object with KILL_ON_JOB_CLOSE.
type Job struct {
	handle syscall.Handle
}

// NewJob creates a job object whose processes are terminated automatically when
// the handle is closed (including when the launcher crashes).
func NewJob() (*Job, error) {
	r, _, err := procCreateJobObject.Call(0, 0)
	if r == 0 {
		return nil, fmt.Errorf("CreateJobObject: %w", err)
	}
	h := syscall.Handle(r)

	var info jobObjectExtendedLimitInfo
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r2, _, err := procSetInformationJobObject.Call(
		uintptr(h),
		uintptr(jobObjectExtendedLimitInformation),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if r2 == 0 {
		_ = syscall.CloseHandle(h)
		return nil, fmt.Errorf("SetInformationJobObject: %w", err)
	}
	return &Job{handle: h}, nil
}

// Assign attaches a process (by PID) to the job object. Child processes it
// spawns afterwards inherit the job membership.
//
// A nil receiver is refused with an error instead of panicking: NewJob returns
// nil on failure, and a process running outside any job silently voids the
// KILL_ON_JOB_CLOSE no-zombie guarantee — the caller has to log that.
func (j *Job) Assign(pid int) error {
	if j == nil || j.handle == 0 {
		return fmt.Errorf("no job object for pid %d", pid)
	}
	r, _, err := procOpenProcess.Call(processSetQuota|processTerminate, 0, uintptr(pid))
	if r == 0 {
		return fmt.Errorf("OpenProcess(%d): %w", pid, err)
	}
	ph := syscall.Handle(r)
	defer syscall.CloseHandle(ph)

	r2, _, err := procAssignProcessToJobObject.Call(uintptr(j.handle), uintptr(ph))
	if r2 == 0 {
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	return nil
}

// Close closes the job handle. Because of KILL_ON_JOB_CLOSE this terminates all
// processes still running in the job, including their children.
func (j *Job) Close() error {
	if j == nil || j.handle == 0 {
		return nil
	}
	err := syscall.CloseHandle(j.handle)
	j.handle = 0
	return err
}
