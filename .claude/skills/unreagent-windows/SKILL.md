---
name: unreagent-windows
description: >
  Windows-specific mechanics of unreagent — cross-compiling from Linux, build
  tags, Job Objects and raw Win32 calls via syscall, the .exe resource
  (icon/version/manifest), code signing, and UE's unattended/crash-reporter
  handling. Use when touching platform-guarded files, the resource, or signing.
---

# unreagent — Windows mechanics

The shipping artefact is a Windows `.exe` produced **on Linux**. Nobody compiles
this on Windows in the normal loop, so every Windows-only path has to be written
so that it still builds — and ideally still tests — on the dev machine.

## Build tags: the two-file pattern

Every platform-specific capability is a pair:

```
internal/supervisor/job_windows.go   //go:build windows   — the real implementation
internal/supervisor/job_other.go     //go:build !windows  — a no-op with identical API
```

The `_other` file is not a stub to be filled in later; it is the documented
degradation. Its doc comment states what is lost (on Linux only the direct child
is killed, not the tree) and why that is acceptable. Follow the pattern exactly:
same type, same method set, no build-tag `if` inside shared files, and
`GOOS=linux go build ./...` must stay green.

## Win32 without CGO

Win32 is called through `syscall.NewLazyDLL` + `NewProc`, never cgo:

```go
kernel32 = syscall.NewLazyDLL("kernel32.dll")
procCreateJobObject = kernel32.NewProc("CreateJobObjectW")
```

This keeps `CGO_ENABLED=0` cross-compilation working offline, which is the whole
reason the launcher can be built from Linux. Adding a cgo dependency breaks CI
(`ci.yml` cross-compiles with CGO disabled) and the release path.

**Error handling for `Call()`:** the third return value is the error, and it is
only meaningful when the primary return indicates failure. Do **not** call
`GetLastError` separately — it races with the Go runtime scheduler, which may
run other calls on the same thread in between. This is a bug this repo has
already fixed once (`22f8ca6`); do not reintroduce it.

Structs passed to Win32 must match the C layout exactly — field order, sizes,
padding. `unsafe.Sizeof` is passed as the length argument; a wrong struct is a
silent memory corruption, not a compile error.

## Job Objects — the no-zombie guarantee

`NewJob` creates the job with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; `Assign`
attaches a PID; closing the handle makes the OS terminate the entire tree.
Every service and every one-shot command goes through it. This is the mechanism
behind the README's "no process zombies" promise and it must survive any
refactor of the supervisor.

## The .exe resource

Icon, version info and the manifest are compiled into
`cmd/launcher/resource_windows_amd64.syso`, generated from
`cmd/launcher/versioninfo.json` + `cmd/launcher/unreagent.manifest` +
`assets/icon.ico`:

```bash
make resource    # needs goversioninfo installed
```

The `.syso` is a **binary tracked in Git LFS** (`.gitattributes`); CI checks out
with `lfs: true`. Bump the version in `versioninfo.json` and regenerate when
shipping — the version shown in `Properties → Details` comes from there, not
from the `-ldflags` build version.

The manifest requests `asInvoker` — no UAC elevation prompt. The launcher must
never need admin rights; if something seems to, the design is wrong.

## Signing

`make win-signed` signs via `osslsigncode` with `signing/codesign.key` — **the
private key is not in the repo and must never be committed.** The public cert
and `signing/import-cert.ps1` are checked in so users can silence the "Unknown
Publisher" warning once. Metadata does not remove that warning; only a
signature does.

## UE-specific Windows behavior

Two editor dialogs block unattended operation, and both are handled at start:

- `-unattended` (config `unreal.unattended`, default true) suppresses the crash
  dialog *and* both recovery systems (PackageAutoSaver, disaster recovery) in
  UE 5.7 — the unclean state is discarded rather than prompted about.
- `killCrashReporter` (default true) kills `CrashReportClientEditor.exe` before
  every (re)start — belt and suspenders, see `killCrashReporter()` in `main.go`.

If a human is working in the editor in parallel, `unattended: false` is the
documented escape hatch. Do not make these behaviors unconditional.

## Testing on Linux

You cannot exercise Job Objects, the taskbar flash, or registry-based engine
detection on the dev box. Verify what you can: the `!windows` half compiles and
behaves, the shared logic has tests, and `GOOS=windows GOARCH=amd64 go build`
succeeds. Then say plainly that the Windows path is unverified — do not claim
it works because it compiled.
