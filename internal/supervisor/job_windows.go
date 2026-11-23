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
	procQueryInformationJob      = kernel32.NewProc("QueryInformationJobObject")
	procQueryFullProcessImage    = kernel32.NewProc("QueryFullProcessImageNameW")
)

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x2000
	processTerminate                  = 0x0001
	processSetQuota                   = 0x0100
	jobObjectBasicProcessIDList       = 3
	processQueryLimitedInformation    = 0x1000
	errorMoreData                     = syscall.Errno(234)
	// maxJobPIDs is how many process ids one look into a job takes in. The
	// editor's job holds the editor's helpers: a few dozen at most.
	maxJobPIDs = 1024
)

// jobProcessIDList is JOBOBJECT_BASIC_PROCESS_ID_LIST with room for
// maxJobPIDs ids: two DWORDs, then ULONG_PTRs, which Go lays out at the same
// offsets as C on 386 and amd64.
type jobProcessIDList struct {
	NumberOfAssignedProcesses uint32
	NumberOfProcessIdsInList  uint32
	ProcessIDList             [maxJobPIDs]uintptr
}

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

// crashReporterRunning reports whether a crash reporter
// (isCrashReporterImage) is among the processes still in the job. The list
// is the job's, so a reporter of another editor never counts. A process that
// ends between the list and the look at its image is skipped; it is no
// longer running.
func (j *Job) crashReporterRunning() (bool, error) {
	if j == nil || j.handle == 0 {
		return false, nil
	}
	var list jobProcessIDList
	r, _, err := procQueryInformationJob.Call(
		uintptr(j.handle),
		uintptr(jobObjectBasicProcessIDList),
		uintptr(unsafe.Pointer(&list)),
		unsafe.Sizeof(list),
		0,
	)
	// ERROR_MORE_DATA still fills in as many ids as fit.
	if r == 0 && err != errorMoreData {
		return false, fmt.Errorf("QueryInformationJobObject: %w", err)
	}
	n := int(list.NumberOfProcessIdsInList)
	if n > maxJobPIDs {
		n = maxJobPIDs
	}
	for _, pid := range list.ProcessIDList[:n] {
		if path, ok := processImage(pid); ok && isCrashReporterImage(path) {
			return true, nil
		}
	}
	return false, nil
}

// processImage returns the full image path of process pid, false when the
// process cannot be opened or asked (it ended meanwhile).
func processImage(pid uintptr) (string, bool) {
	r, _, _ := procOpenProcess.Call(processQueryLimitedInformation, 0, pid)
	if r == 0 {
		return "", false
	}
	ph := syscall.Handle(r)
	defer syscall.CloseHandle(ph)
	var buf [1024]uint16
	size := uint32(len(buf))
	r2, _, _ := procQueryFullProcessImage.Call(uintptr(ph), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r2 == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf[:size]), true
}
