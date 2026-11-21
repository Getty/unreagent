# Document 2 — Windows process and console facts for a Go launcher

Research date: **21 November 2026**. Scope: desktop Win32; API availability is separate from the minimum Windows version supported by the launcher's chosen Go release. Go observations identify the inspected source, not an assumed unreagent dependency version. Snippets use `golang.org/x/sys/windows` v0.50.0 and ordinary Go `unsafe`; Win32 calls use no cgo. The eight snippets were **cross-compiled with Go 1.27.4, x/sys v0.50.0, `CGO_ENABLED=0`, `GOOS=windows`, `GOARCH=amd64` and `arm64`**; no Windows execution was available. [Go source][W-go-start] · [x/sys source][W-xs-call]

## 1. Creation, job assignment, and the execution race

**What the obvious assumption gets wrong:** `CreateProcess` followed by assignment does not prevent the child running first. Suspension closes that execution race but leaves an orphan interval if the creator dies before assignment; a creation-time job attribute removes the separate assignment interval. [Chen][W-chen]

| Fact | Source | Windows versions |
|---|---|---|
| An ordinary newly created child can execute before its creator assigns it to a job; creating a descendant during that interval precedes automatic job inheritance. Later assignment is not documented as retroactively collecting an existing process tree. | [Chen][W-chen]; [job inheritance][W-jobs] | All versions with these APIs. |
| `CREATE_SUSPENDED` suspends the initial thread; assignment must succeed before `ResumeThread` permits it to execute. Creator death before assignment can leave an unassigned suspended process. | [Creation flags][W-flags]; [Chen][W-chen] | Current API references list XP / Server 2003 as the supported minimum for `AssignProcessToJobObject` and `ResumeThread`. |
| `PROC_THREAD_ATTRIBUTE_JOB_LIST` accepts an ordered array of job handles. Assignment occurs before the initial thread runs; the order must satisfy nested-job rules. This is the documented fix for the separate create/assign execution window, not a published transaction specification for every interrupted kernel call. | [Attribute contract][W-attrs]; [Chen][W-chen] | **Windows 10+; Server 2016+**. |
| `syscall.SysProcAttr.CreationFlags` can carry `CREATE_SUSPENDED`, but `syscall.StartProcess` closes `PROCESS_INFORMATION.Thread` before returning. `os/exec.Cmd` therefore does not expose the original thread handle for `ResumeThread`. `SysProcAttr` has no general attribute-list/job-list field in the inspected source. | `src/syscall/exec_windows.go`: [`SysProcAttr`, `StartProcess`][W-go-start] | Go 1.27.4 source inspected; Windows API availability as above. |
| x/sys exposes `CreateProcess`, `ProcessInformation.Thread`, `ResumeThread`, `StartupInfoEx`, and `NewProcThreadAttributeList`. Use of the extended structure requires its full size and `EXTENDED_STARTUPINFO_PRESENT`; the attribute value remains valid until the list is deleted. | [`CreateProcessW`][W-create]; [attribute lifetime][W-attrs]; x/sys [`exec_windows.go`][W-xs-exec], [`types_windows.go`][W-xs-types], [`syscall_windows.go`][W-xs-call] | Extended attributes: Vista+; **job-list attribute: 10+**. |
| `PROC_THREAD_ATTRIBUTE_JOB_LIST` is not exported by inspected x/sys v0.50.0; its SDK value is `0x2000D`. | Microsoft `generation/WinSDK/RecompiledIdlHeaders/um/WinBase.h`: [`ProcThreadAttributeJobList`, `ProcThreadAttributeValue`, `PROC_THREAD_ATTRIBUTE_JOB_LIST`][W-sdk]; x/sys [`types_windows.go`][W-xs-types] | SDK constant for Windows 10+ functionality. |

Minimal Go: creation-time assignment. Inputs are an absolute executable path, a writable UTF-16 command-line buffer, and an already configured job; returned process/thread handles remain caller-owned. Sources: [CreateProcessW][W-create], [x/sys bindings][W-xs-call], [attribute container][W-xs-exec].

```go
func createInJob(job windows.Handle, app, line *uint16) (windows.ProcessInformation, error) {
    var pi windows.ProcessInformation
    a, err := windows.NewProcThreadAttributeList(1)
    if err != nil { return pi, err }
    defer a.Delete()
    jobs := []windows.Handle{job}
    const jobList = uintptr(0x2000D)
    if err = a.Update(jobList, unsafe.Pointer(&jobs[0]), unsafe.Sizeof(jobs[0])); err != nil {
        return pi, err
    }
    si := windows.StartupInfoEx{ProcThreadAttributeList: a.List()}
    si.Cb = uint32(unsafe.Sizeof(si))
    err = windows.CreateProcess(app, line, nil, nil, false,
        windows.EXTENDED_STARTUPINFO_PRESENT, nil, nil, &si.StartupInfo, &pi)
    return pi, err
}
```

Minimal Go: suspended alternative. On a post-creation failure it attempts termination; `stopErr` separately reports failed cleanup. Nonzero `pi` retains both handles on all return paths. Sources: [assignment][W-assign], [resume][W-resume], [termination][W-term-process].

```go
func createSuspendedInJob(job windows.Handle, app, line *uint16) (
    pi windows.ProcessInformation, err error, stopErr error) {
    si := windows.StartupInfo{}
    si.Cb = uint32(unsafe.Sizeof(si))
    err = windows.CreateProcess(app, line, nil, nil, false,
        windows.CREATE_SUSPENDED, nil, nil, &si, &pi)
    if err != nil { return }
    err = windows.AssignProcessToJobObject(job, pi.Process)
    if err == nil { _, err = windows.ResumeThread(pi.Thread) }
    if err != nil { stopErr = windows.TerminateProcess(pi.Process, 1) }
    return
}
```

## 2. Last-handle closure, termination, exit codes, and unfinished dumps

**Short answer:** kill-on-close is keyed to the **last job handle**, not to a parent PID exiting. It supplies no graceful-shutdown protocol. A particular exit code, per-process kill order, or completed crash dump must not be inferred from it. [Job lifetime][W-jobs]; [job limits][W-limits]; [termination][W-term-job]

| Fact | Source | Windows versions |
|---|---|---|
| With `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, closing the last handle terminates associated processes. An inherited, duplicated, or independently opened handle postpones that last-handle condition. Merely belonging to a job does not mean a process owns a job handle. | [Job lifetime][W-jobs]; [job creation/handle inheritance][W-create-job] | XP+ / Server 2003+ in current API requirements. |
| A nested job's kill-on-close also covers processes in its child jobs. Microsoft's hierarchy termination description starts at the bottom child job; it does **not** specify a parent-before-child or child-before-parent order among processes inside one job. | [Job lifetime][W-jobs]; [nested-job termination][W-nested] | Windows 8+ / Server 2012+. |
| **Unverified:** a fixed numeric exit code for automatic kill-on-close. The limit's contract does not name one. `TerminateJobObject(job, code)` is different: its explicit `code` becomes members' termination status. Settle automatic closure with experiment W2; do not substitute the explicit API's parameter. | [Limit contract][W-limits]; [`TerminateJobObject`][W-term-job] | Automatic-code observations must be recorded per tested OS build. |
| `TerminateJobObject` cannot be postponed or handled by members; its effect is as though each received `TerminateProcess`. The latter stops threads and requests pending-I/O cancellation; completion waits for I/O to complete or cancel. No user-space buffer flush or crash-report-finalization callback is promised for kill-on-close. The last sentence is the conservative conclusion from the termination contracts, not a claim that every buffered byte is lost. | [`TerminateJobObject`][W-term-job]; [`TerminateProcess`][W-term-process]; [kill-on-close contract][W-limits] | XP+ / Server 2003+. |
| The exact file left by a killed dump writer is **unverified**: absent, partial, or usable output depends on how far the writer got and its file lifecycle. `MiniDumpWriteDump` reports successful completion through its return value; filename existence is not that result. Settle with W3. | [`MiniDumpWriteDump`][W-dump]; [pending I/O][W-term-process] | DbgHelp-dependent; test actual Windows/DbgHelp versions. |
| Waiting on a retained **process handle** can establish that process's termination. The generic job documentation only specifies a signaled job state for an exceeded end-of-job time limit; job-completion-port messages, except specified notification-limit messages, are not guaranteed delivery. | [`TerminateProcess`][W-term-process]; [job signaling/notifications][W-jobs] | API-specific; nested jobs do not change this distinction. |

Minimal Go: observe one retained process handle after closing the job handle. This closes the last handle **only if no other holder exists**. Sources: [job lifetime][W-create-job], [`GetExitCodeProcess`][W-exit], [x/sys declarations][W-xs-call].

```go
func closeJobAndReadExit(job, process windows.Handle) (uint32, error) {
    if err := windows.CloseHandle(job); err != nil { return 0, err }
    state, err := windows.WaitForSingleObject(process, windows.INFINITE)
    if err != nil { return 0, err }
    if state != windows.WAIT_OBJECT_0 { return 0, windows.ERROR_INVALID_DATA }
    var code uint32
    err = windows.GetExitCodeProcess(process, &code)
    return code, err
}
```

## 3. Breakaway and one surviving crash reporter

**Short answer:** job limits have no executable-name exception. Explicit breakaway is a job-wide permission exercised by the process creating the helper; silent breakaway exempts ordinary children generally. A parent cannot remove an already assigned crash reporter from its job. [Limits][W-limits]; [membership][W-jobs]

| Fact | Source | Windows versions |
|---|---|---|
| `JOB_OBJECT_LIMIT_BREAKAWAY_OK` permits a job member's `CreateProcess(..., CREATE_BREAKAWAY_FROM_JOB, ...)` call to create a child outside that job. The creation flag belongs to the **new child creation call**. With no containing job the flag has no effect. | [Limit definition][W-limits]; [creation flag][W-flags] | XP+ / Server 2003+ in current job API requirements. |
| `JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK` makes children escape without the explicit creation flag; it cannot express “only CrashReportClientEditor.exe.” | [Limit definition][W-limits]; [process-tree limitations][W-jobs] | Same; nested ancestors impose additional constraints. |
| Selective survival is possible for cooperating code: enable explicit breakaway, and request it only for the helper. This is **not enforcement against another member requesting the same flag**. Alternatively, a launcher outside the editor job can itself create the reporter outside that job. These are deductions from the membership/creation rules, not a per-helper Job Object feature. | [Creation flags][W-flags]; [job inheritance][W-jobs] | Subject to any outer jobs and their breakaway policies. |
| If Unreal creates the reporter itself, the launcher cannot retroactively change that creation call's flags or detach the assigned reporter. Whether the actual UE build requests breakaway is **unverified here**; inspect its `CreateProc` call chain and measure reporter membership in W4. | [Membership cannot be broken][W-jobs]; UE-specific part requires the engine source/run. | UE build-specific; Windows rule applies generally. |
| Breaking away from an inner job does not imply escape from a restrictive ancestor. Breakaway proceeds up the chain until a job disallows it; if the immediate job disallows it, allowed ancestors do not help. | [Nested jobs][W-nested] | Windows 8+ / Server 2012+. |
| Kill-on-close covers job members, not arbitrary causally related processes. Microsoft explicitly excludes children created through `Win32_Process.Create` from ordinary inheritance; an out-of-job broker is outside this containment promise. | [Managing processes in jobs][W-jobs] | As documented; verify actual launch paths. |

Minimal Go: the flags for a cooperating helper creator. `job` already contains the calling process, its handle permits setting limits, and the two listed flags are the intended complete extended-limit configuration; `app`/`line` name only the intended helper. This replaces that configuration for the **whole job**, not just this call. Sources: [limit definitions][W-limits], [x/sys bindings and structures][W-xs-call].

```go
var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
limits.BasicLimitInformation.LimitFlags =
    windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
_, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
    uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)))
if err != nil { return err }
si := windows.StartupInfo{}
si.Cb = uint32(unsafe.Sizeof(si))
var pi windows.ProcessInformation
err = windows.CreateProcess(app, line, nil, nil, false,
    windows.CREATE_BREAKAWAY_FROM_JOB, nil, nil, &si, &pi)
// On success pi.Process and pi.Thread are caller-owned.
```

## 4. A launcher that already belongs to another job

**Short answer:** an existing job is an incompatibility on older Windows; on newer Windows it is a hierarchy with constraints, not an automatic error and not an unrestricted escape route. [Assignment][W-assign]; [nested jobs][W-nested]

| Fact | Source | Windows versions |
|---|---|---|
| A process can belong to only one job. A launcher-created child ordinarily inherits the outer job, so assigning it to a distinct editor job fails unless permitted breakaway removed the outer membership first. | [`AssignProcessToJobObject`][W-assign] | Windows 7 / Server 2008 R2 and earlier documented versions. |
| Multiple membership must form a valid nested hierarchy; jobs' process sets must be nested, and the documented automatic nesting conditions exclude UI limits. Child membership inherits the parent's job chain by default. | [Nested job hierarchy][W-nested] | Windows 8+ / Server 2012+. |
| Parent-job resource limits continue to constrain nested children; adding an inner job cannot relax inherited limits. Terminating an outer job covers its nested children. | [Effective limits and termination][W-nested]; [`TerminateJobObject`][W-term-job] | Windows 8+ / Server 2012+. |
| `IsProcessInJob(currentProcess, NULL, &result)` answers membership in **any** job. A terminal/CI product name does not establish membership or policy. Its actual restrictions remain **unverified** until W5 measures the concrete host configuration. | [`IsProcessInJob`][W-in-job] | XP+ / Server 2003+. |
| Program Compatibility Assistant can place a process in a compatibility job on Vista/7-era systems. Microsoft's assignment documentation describes permitted breakaway or a UAC-level manifest as ways this can affect job assignment. | [PCA note in assignment documentation][W-assign] | Vista/7; Server 2008/2008 R2. |

Minimal Go: inspect actual membership; Win32 `BOOL` output occupies 32 bits. Source: [`IsProcessInJob`][W-in-job].

```go
func inAnyJob() (bool, error) {
    p := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
    var yes uint32
    ok, _, err := p.Call(^uintptr(0), 0, uintptr(unsafe.Pointer(&yes)))
    if ok == 0 { return false, err }
    return yes != 0, nil
}
```

## 5. Consoles, GUI parents, console membership, and closing the window

**Short answer:** executable subsystem, console attachment, standard-handle inheritance, and window visibility are separate properties. `GetConsoleProcessList` answers who is attached now; it cannot prove who created or “owns” the console. Closing a console gives only a bounded cleanup opportunity. [Console creation][W-console-create]; [attachment][W-attach]; [process list][W-console-list]; [close handling][W-handler]

| Fact | Source | Windows versions |
|---|---|---|
| A console child normally inherits its parent's console; when no console is available it normally receives a new one. GUI-subsystem applications are initialized without console attachment. A GUI parent can explicitly attach or allocate a console before starting children. | [Creation of a console][W-console-create]; [`AllocConsole`][W-alloc] | Desktop Windows; `AllocConsole`: Windows 2000+. |
| A GUI child may still receive inherited standard handles. Having valid redirected stdin/stdout/stderr does not prove console attachment. | [`AttachConsole`, subsystem/handle-inheritance note][W-attach]; [`CreateProcessW`, handle inheritance][W-create] | `AttachConsole`: XP+ / Server 2003+. |
| `AttachConsole(ATTACH_PARENT_PROCESS)` attaches to the parent's existing console; already attached yields `ERROR_ACCESS_DENIED`, parent without console yields `ERROR_INVALID_HANDLE`, nonexistent target yields `ERROR_INVALID_PARAMETER`. | [`AttachConsole`][W-attach] | XP+ / Server 2003+. |
| A process attaches to at most one console. `AllocConsole` allocates one when unattached; `FreeConsole` detaches. An unattached process can subsequently attach/allocate again. | [`AllocConsole`][W-alloc]; [`FreeConsole`][W-free] | Allocation/detachment: Windows 2000+; attachment: XP+. |
| `CREATE_NEW_CONSOLE` selects a new console and conflicts with `DETACHED_PROCESS`. `CREATE_NO_WINDOW` is for console applications and is ignored for GUI applications or when combined with either of those flags. A new console is distinct from a new process group. | [Creation flags][W-flags] | Desktop Win32; verify the intended host on Windows 10/11. |
| `GetConsoleProcessList` returns attached PIDs. A singleton proves only current singleton membership; an original shell could have detached/exited, and children can join a console the launcher created. “Singleton means Explorer launch” is therefore an invalid inference. | [API contract][W-console-list]; deduction from attachment/detachment rules in [`FreeConsole`][W-free] | XP+ / Server 2003+. |
| Console-window close sends `CTRL_CLOSE_EVENT` to attached processes. Returning `TRUE` from its handler does **not** cancel termination. The documented close timeout is `SPI_GETHUNGAPPTIMEOUT`, listed as **5000 ms**, not an unconditional entitlement to five seconds after arbitrary cleanup work begins. | [`HandlerRoutine`, return behavior and timeout table][W-handler] | Handler API: Windows 2000+; host-specific timing remains W6. |
| `AttachConsole`, `AllocConsole`, and `FreeConsole` reset the process's console-control-handler table. Loading `user32.dll`/`gdi32.dll` also affects delivery of logoff/shutdown events, distinct from Ctrl-C/Break and ordinary console close. | [`SetConsoleCtrlHandler`][W-set-handler] | Reset rule as documented; GUI-library note specifically Windows 7/8/8.1/10. |
| In Go, notification of close/logoff/shutdown is `syscall.SIGTERM`; the runtime blocks its native handler after delivering it to give Go cleanup code a chance, but Windows can still terminate the process at the deadline. | [`os/signal` Windows section][W-go-signal]; `src/runtime/os_windows.go`: [`ctrlHandler`][W-go-handler] | Go 1.14+ for SIGTERM mapping ([release notes][W-go-114]); host event rules still apply. |

Minimal Go: singleton detection, deliberately **not** named console ownership. The one-element buffer is valid; if more entries are needed, the return value exceeds one. Sources: [API contract][W-console-list], x/sys [`GetCurrentProcessId`][W-xs-call].

```go
func soleConsoleClient() (bool, error) {
    p := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")
    var ids [1]uint32
    n, _, err := p.Call(uintptr(unsafe.Pointer(&ids[0])), 1)
    if n == 0 { return false, err }
    return n == 1 && ids[0] == windows.GetCurrentProcessId(), nil
}
```

## 6. Ctrl-C, Ctrl-Break, process groups, and Go

**Short answer:** process groups are console-signal groups, not Job Objects. Targeted Ctrl-Break is documented; a nonzero-group Ctrl-C call is explicitly not equivalent. Go's received signal does not distinguish the two. [Event generation][W-generate]; [Go signal contract][W-go-signal]

| Fact | Source | Windows versions |
|---|---|---|
| `CREATE_NEW_PROCESS_GROUP` makes the new PID its group's identifier; descendants belong to the group. It initially disables Ctrl-C for the group. The flag is ignored when combined with `CREATE_NEW_CONSOLE`. | [Creation flags][W-flags] | Desktop Win32. |
| `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, groupPID)` reaches group members sharing the caller's console. A descendant on another console is outside delivery. Group `0` addresses all processes on the caller's console. | [`GenerateConsoleCtrlEvent`][W-generate] | Windows 2000+. |
| Microsoft explicitly says Ctrl-C cannot be restricted to a nonzero process group: that call can report success while the specified group's processes receive no Ctrl-C. Success is not delivery acknowledgement. | [`GenerateConsoleCtrlEvent`][W-generate] | Windows 2000+. |
| Ctrl-C can be ignored through an inheritable `SetConsoleCtrlHandler(NULL, TRUE)` setting or treated as keyboard input when processed-input mode is disabled. Ctrl-Break invokes handlers despite the Ctrl-C-ignore setting. | [`SetConsoleCtrlHandler`][W-set-handler]; [`GenerateConsoleCtrlEvent`][W-generate] | Windows 2000+. |
| Go normally exits on Ctrl-C/Break; `signal.Notify(..., os.Interrupt)` changes both to channel notifications. Both map to the same Go interrupt signal. Console-close handling instead remains subject to forced termination, as in §5. | [`os/signal` Windows section][W-go-signal]; [`ctrlHandler`][W-go-handler] | Go 1.27.4; close-event mapping since Go 1.14 ([release notes][W-go-114]). |
| `os.Process.Signal(os.Interrupt)` is not implemented as `GenerateConsoleCtrlEvent` in inspected Go source. `Process.Kill` uses `TerminateProcess(..., 1)`; this is neither Ctrl-C nor a job-wide operation. | `src/os/exec_windows.go`: [`(*Process).signal`][W-go-process] | Go 1.27.4 inspected; not a promise for every future Go release. |
| Programmatic console attachment can reset Go's already installed native handler table. The exact result after the launcher's attach/free sequence is **unverified**; W7 checks actual receipt instead of assuming `signal.Notify` reinstalled the native handler. | [Handler-table reset][W-set-handler]; [`ctrlHandler`][W-go-handler] | Relevant to any Go build using dynamic attachment. |

Minimal Go: targeted event, with preconditions that `groupPID` is a group-root PID, caller and target share a console, and the receiver has completed handler initialization. Source: [`GenerateConsoleCtrlEvent`][W-generate]; [x/sys binding][W-xs-call].

```go
func sendBreak(groupPID uint32) error {
    if groupPID == 0 { return windows.ERROR_INVALID_PARAMETER }
    return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, groupPID)
}
```

## 7. Drive paths, UNC, extra leading separators, and device prefixes

**Short answer:** a slash-normalizing string rewrite is not Windows name resolution. `C:foo`, `C:\foo`, `\\server\share\foo`, `\\?\...`, and `\\.\...` enter different rules; a successfully normalized string is not proof that a file exists or is the intended file. [Path formats][W-path-formats]; [namespaces][W-names]; [`GetFullPathNameW`][W-full-path]

| Fact | Source | Windows versions |
|---|---|---|
| `C:\dir\file` is drive-rooted; `C:dir\file` is relative to that drive's current directory. `\dir\file` uses the current drive. `\\server\share\file` is UNC and does not acquire the process's current directory. | [Path identification/current-directory rules][W-path-formats]; [relative paths][W-names] | Desktop Windows; current directory is process state. |
| Ordinary Win32 normalization converts `/` to `\` and collapses runs of separators **after the first two**. It recognizes a two-separator prefix as UNC unless a device prefix applies. This does not justify collapsing every leading run to one slash. | [Canonicalize separators / identify the path][W-path-formats] | Microsoft documents the Windows normalization model; no complete per-build matrix. |
| **Unverified:** exact acceptance and file identity of `\\\server\share\file`, `////server/share/file`, or `\\\C:\dir\file` for the actual `CreateFileW` and `CreateProcessW` calls. The normalization description is not an explicit guarantee that malformed UNC prefixes identify a given file. W8 records each API result; no guessed universal three-or-more-slash equivalence is asserted. | Scope limit of [path-format documentation][W-path-formats]; [CreateFileW][W-create-file]; [CreateProcessW][W-create] | Test each supported Windows build and file provider. |
| Exact `\\?\C:\dir\file` and `\\?\UNC\server\share\file` are extended forms; implicit normalization is bypassed. Forward slashes and `.`/`..` must not be assumed to receive ordinary separator/relative-component processing. `GetFullPathNameW` explicitly called on an extended path can still normalize it. | [Extended-path rules][W-long]; [skip normalization][W-path-formats] | Supported Unicode file APIs; unprefixed long-path opt-in begins Windows 10 version 1607. |
| `\\.\` selects the Win32 device namespace, including disks, serial ports and filesystem-backed drive names. `\\.\C:\dir\file` may refer through the drive's filesystem namespace, but `\\.\PhysicalDrive0` is a device, not an alternative spelling of a normal file. API support must be checked; namespace prefixes cannot be blindly stripped. | [Win32 device namespaces][W-names] | API-dependent; not all Win32 APIs accept these forms. |
| `GetFullPathNameW` does not validate existence. For successful opens on a suitable filesystem, matching volume serial and file index can show that differently spelled paths identify the same object; network providers may return incomplete information. | [`GetFullPathNameW`][W-full-path]; [`GetFileInformationByHandle`][W-file-info] | Current API minimum XP/2003; provider-dependent. |

Minimal Go: read the identity returned by opening the **original spelling**, without preprocessing it. Compare `VolumeSerialNumber`, `FileIndexHigh`, `FileIndexLow` for the local NTFS experiment; this is not a portable filesystem identity abstraction. Sources: [CreateFileW][W-create-file], [file identity][W-file-info], [x/sys bindings][W-xs-call].

```go
func pathIdentity(raw string) (windows.ByHandleFileInformation, error) {
    var info windows.ByHandleFileInformation
    p, err := windows.UTF16PtrFromString(raw)
    if err != nil { return info, err }
    h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
        windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE,
        nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
    if err != nil { return info, err }
    defer windows.CloseHandle(h)
    err = windows.GetFileInformationByHandle(h, &info)
    return info, err
}
```

[W-chen]: https://devblogs.microsoft.com/oldnewthing/20230209-00/?p=107812
[W-flags]: https://learn.microsoft.com/en-us/windows/win32/procthread/process-creation-flags
[W-attrs]: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute
[W-create]: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-createprocessw
[W-assign]: https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-assignprocesstojobobject
[W-resume]: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-resumethread
[W-jobs]: https://learn.microsoft.com/en-us/windows/win32/procthread/job-objects
[W-create-job]: https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-createjobobjectw
[W-limits]: https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_basic_limit_information
[W-nested]: https://learn.microsoft.com/en-us/windows/win32/procthread/nested-jobs
[W-term-job]: https://learn.microsoft.com/en-us/windows/win32/api/jobapi2/nf-jobapi2-terminatejobobject
[W-term-process]: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-terminateprocess
[W-exit]: https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getexitcodeprocess
[W-dump]: https://learn.microsoft.com/en-us/windows/win32/api/minidumpapiset/nf-minidumpapiset-minidumpwritedump
[W-in-job]: https://learn.microsoft.com/en-us/windows/win32/api/jobapi/nf-jobapi-isprocessinjob
[W-console-create]: https://learn.microsoft.com/en-us/windows/console/creation-of-a-console
[W-attach]: https://learn.microsoft.com/en-us/windows/console/attachconsole
[W-alloc]: https://learn.microsoft.com/en-us/windows/console/allocconsole
[W-free]: https://learn.microsoft.com/en-us/windows/console/freeconsole
[W-console-list]: https://learn.microsoft.com/en-us/windows/console/getconsoleprocesslist
[W-handler]: https://learn.microsoft.com/en-us/windows/console/handlerroutine
[W-set-handler]: https://learn.microsoft.com/en-us/windows/console/setconsolectrlhandler
[W-generate]: https://learn.microsoft.com/en-us/windows/console/generateconsolectrlevent
[W-go-start]: https://github.com/golang/go/blob/go1.27.4/src/syscall/exec_windows.go
[W-go-process]: https://github.com/golang/go/blob/go1.27.4/src/os/exec_windows.go
[W-go-signal]: https://github.com/golang/go/blob/go1.27.4/src/os/signal/doc.go
[W-go-handler]: https://github.com/golang/go/blob/go1.27.4/src/runtime/os_windows.go
[W-go-114]: https://go.dev/doc/go1.14
[W-xs-call]: https://github.com/golang/sys/blob/v0.50.0/windows/syscall_windows.go
[W-xs-exec]: https://github.com/golang/sys/blob/v0.50.0/windows/exec_windows.go
[W-xs-types]: https://github.com/golang/sys/blob/v0.50.0/windows/types_windows.go
[W-sdk]: https://github.com/microsoft/win32metadata/blob/main/generation/WinSDK/RecompiledIdlHeaders/um/WinBase.h
[W-path-formats]: https://learn.microsoft.com/en-us/dotnet/standard/io/file-path-formats
[W-names]: https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file
[W-long]: https://learn.microsoft.com/en-us/windows/win32/fileio/maximum-file-path-limitation
[W-full-path]: https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfullpathnamew
[W-file-info]: https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfileinformationbyhandle
[W-create-file]: https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-createfilew

## Open facts

All Windows experiments below are **unrun**. A run establishes observations for its recorded Windows build, Go version, x/sys version, host, filesystem and engine build; repeated observations do not turn an undocumented behavior into a Windows API contract.

| ID / unresolved fact | Concrete experiment: what to run | What to inspect |
|---|---|---|
| W1 — launch/creator-death boundaries | Run a disposable helper whose first action creates a second helper. Repeat ordinary creation with delayed assignment, suspended creation with a pause before assignment, and `JOB_LIST` creation. Terminate the creating test process at each marked checkpoint; separately stress termination during `CreateProcessW`. | Retained observer handles, creation timestamps, each PID's `IsProcessInJob`, descendants after creator exit, suspended orphan count. Record creation failures and any partial-creation observations; documentation clarification is still required for a universal interrupted-call guarantee. |
| W2 — automatic termination code, ordering, and extra handles | Put a parent and two children in one kill-on-close job; separately use nested jobs. An observer outside the jobs retains **process handles only**. Close the last job handle; repeat with a second process intentionally retaining a duplicate job handle. Compare with explicit `TerminateJobObject(job, 123)`. | Raw unsigned hexadecimal/decimal exit codes, process-handle signal times, child-vs-parent ordering, and whether the duplicate delays termination. Never infer success from an automatic exit status observed as zero. |
| W3 — crash-dump and file durability | In a disposable test, have one member write buffered counters and another call `MiniDumpWriteDump` for a large sacrificial process. Close the last job handle at several dump-write checkpoints, and compare a reporter outside the job. | Missing/partial/full files, dump-writer return record when available, dump stream/directory readability in WinDbg, last persisted counter and cleanup marker. Inspect complete dump contents rather than size stability alone. |
| W4 — actual UE helper survival | Start the actual UE 5.7 and 5.8 builds with a disposable project and trigger their supported test crash. Observe reporter/helper creation, query job membership, then independently terminate only the editor or terminate the supervisor holding the last job handle. Repeat normal/explicit-breakaway/silent-breakaway test policies. | Executable names, creator PIDs, membership in each known job, dump completion and reporter lifetime. Establish whether that UE build requests breakaway, and whether an outer job still contains it. |
| W5 — real outer jobs | Run the membership/creation probes from Explorer, cmd, Windows PowerShell 5.1, pwsh 7, Windows Terminal, the actual CI runner and any supervising launcher. Repeat on every supported OS; on legacy VMs use an appropriate old Go build or a native API probe. | `IsProcessInJob`, assignment/creation errors, queried effective job limits and success of a valid inner hierarchy; record UI restrictions and breakaway permission instead of classifying by terminal name. |
| W6 — console identity and close deadline | Build the same Go probe as console and GUI subsystem executables. Launch each from Explorer and existing shells; exercise attach, allocate, new-console and detached paths. Record PID lists, standard-handle types and a timestamped file heartbeat; close the actual console window or terminal tab. | Console membership versus handles, singleton after the original shell detaches/exits, raw control events, time to process-handle signal and last flushed marker. Compare classic conhost and the actual Windows Terminal/ConPTY host. |
| W7 — signal and handler-reset behavior | Launch a Go `signal.Notify` probe in a fresh process group; from a peer on the same console send Ctrl-Break to the group, Ctrl-C to the group and Ctrl-C to group zero. Repeat after the launcher's exact `FreeConsole`/`AttachConsole` sequence; use sacrificial console processes. | `GenerateConsoleCtrlEvent` return values, actual `os.Interrupt`/`SIGTERM` notifications, unaffected/differently attached siblings, ignore-C inheritance, handler reset, and eventual exit on console close. |
| W8 — exact path/API equivalence | Create one harmless file and one harmless marker executable under `C:\unreagent-path-probe`, expose a temporary test SMB share, and pass literal spellings with 1–5 leading backslashes, forward slashes, mixed separators, `\\?\`, `\\?\UNC\`, and `\\.\` to `GetFullPathNameW`, read-only `CreateFileW`, and `CreateProcessW` separately. Use only the test executable for creation tests. | Input UTF-16 code units, returned normalized text, error codes, opened-file volume/file IDs and executed image path. Compare drive/UNC aliases, trailing dots/spaces and `.`/`..`; do not classify a successful normalization as a successful open. |
| W9 — snippet compatibility | Compile the snippets against unreagent's actual Go/x/sys versions with `GOOS=windows`, then run their API probes on target Windows machines. | Structure sizes, missing exports/constants, error paths and handle ownership. Cross-compilation already passed with Go 1.27.4 and x/sys v0.50.0; actual launcher dependency compatibility and Windows execution remain unverified. |
