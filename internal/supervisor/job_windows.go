//go:build windows

// Windows-Job-Objects sorgen dafür, dass beim Schließen des Job-Handles der
// komplette Prozessbaum vom Betriebssystem getötet wird (KILL_ON_JOB_CLOSE).
// Dadurch hinterlässt ein beendeter, neugestarteter oder abgestürzter Launcher
// keine Waisenprozesse (ShaderCompileWorker, CrashReportClient, …).
//
// Wir rufen die Win32-API direkt über syscall.NewLazyDLL auf — keine externe
// Abhängigkeit, damit der Cross-Compile von Linux aus offline funktioniert.
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

// Job kapselt ein Windows-Job-Object mit KILL_ON_JOB_CLOSE.
type Job struct {
	handle syscall.Handle
}

// NewJob erzeugt ein Job-Object, dessen Prozesse beim Schließen des Handles
// (auch bei Absturz des Launchers) automatisch beendet werden.
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

// Assign hängt einen Prozess (per PID) an das Job-Object. Vom Prozess danach
// gestartete Kindprozesse erben die Job-Mitgliedschaft.
func (j *Job) Assign(pid int) error {
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

// Close schließt das Job-Handle. Wegen KILL_ON_JOB_CLOSE beendet das alle noch
// laufenden Prozesse des Jobs samt deren Kinder.
func (j *Job) Close() error {
	if j == nil || j.handle == 0 {
		return nil
	}
	err := syscall.CloseHandle(j.handle)
	j.handle = 0
	return err
}
