---
name: unreagent-windows
description: >
  Windows-specific mechanics of unreagent — cross-compiling from Linux, build
  tags, Job Objects and raw Win32 calls via syscall, the .exe resource
  (icon/version/manifest) and code signing. Use when touching platform-guarded
  files, the resource, or signing. What UE itself does (-unattended, the crash
  reporter): skill unreagent-ue.
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
Every service and every one-shot command goes through it, and so does the bridge
smoke test (`smokeTestMCP`, `cmd/launcher/bridge.go`) via `supervisor.NewJob()`,
closing the job before it kills the child. Only the short-lived `reg query`
and `git check-ignore` (`unignoredTokenFile`,
`cmd/launcher/mcpconfig.go`) helpers run without one. This is the
mechanism behind the README's "no process zombies" promise and it must survive
any refactor of the supervisor.

`Assign` runs after `Start` at every spawn site, so whatever the child spawns in
that gap escapes the job — tracked on k37.

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

## UE-specific behavior

What `-unattended` does, what the crash reporter is, where crash artefacts
land and which of that is verified: skill `unreagent-ue`. Which starts spare a
crash's evidence is launcher logic: `followsCrash` and `editorPreStart` in
`cmd/launcher/lifecycle.go`.

Two rules stay here because they are about this code, not about UE:

- The launcher kills no crash reporter (k154): no `taskkill`, no `pkill`. The
  editor's reporter is in its Job Object; after a failed exit the supervisor
  keeps that job open while the reporter is in it, `ServiceSpec.ReporterGrace`
  at most (`closeAfterReporter`, `internal/supervisor/reporter.go`;
  `crashReporterRunning` in `job_windows.go` looks via
  `QueryInformationJobObject` and `QueryFullProcessImageNameW`, a no-op in
  `job_other.go`), and every close goes through `Supervisor.closeJob`. The next
  start waits (`ServiceStatus.Waiting`). `KILL_ON_JOB_CLOSE` and the job
  assignment are unchanged, and a launcher shutdown closes the kept job at
  once. Cross-compiled only: unverified on Windows, where the old close was
  measured (k154 runs 33, 34, 52). Do not add a kill by image name back, and do
  not leave a job open without a bound.
- `unreal.unattended` and `unreal.cleanOnRestart` stay switchable:
  `unattended: false` is the documented escape hatch for a human working in
  the editor. Do not make these behaviors unconditional. `unreal.killCrashReporter`
  is accepted for old configs and does nothing.

## Testing on Linux

You cannot exercise Job Objects or registry-based engine detection on the dev
box. Verify what you can: the `!windows` half compiles and behaves, the shared
logic has tests, and `GOOS=windows GOARCH=amd64 go build` succeeds. Then say
plainly that the Windows path is unverified — do not claim it works because it
compiled.
