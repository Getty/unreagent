package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/conflict-industries/unreagent/internal/config"
	"github.com/conflict-industries/unreagent/internal/supervisor"
)

// makeAgentExitHandler builds the callback that fires when the agent ends and is
// not restarted on its own (see ServiceSpec.OnExit). In window mode the agent is
// the leading process — once it ends the session is over, and the launcher must
// not silently hang around with a running editor.
func makeAgentExitHandler(ctx context.Context, stop func(), sup *supervisor.Supervisor, cfg *config.Config, logger func(string), interactive bool) func(success bool) {
	var mu sync.Mutex
	cmdLoopRunning := false // already switched to the launcher console?
	return func(success bool) {
		if success {
			logger("Agent exited (exit 0).")
		} else {
			logger("Agent crashed / restarts exhausted.")
		}

		// If the launcher console is already running (an earlier 'k' choice), a
		// second stdin prompt would compete with it for the input — so only log.
		mu.Lock()
		busy := cmdLoopRunning
		mu.Unlock()
		if busy {
			logger("Launcher console active — 'q' stops everything, 'start agent' restarts the agent.")
			return
		}

		switch cfg.Agent.OnExit {
		case config.OnExitLeave:
			logger("agent.onExit=leave — editor/MCP keep running. Ctrl-C stops the launcher.")
		case config.OnExitShutdown:
			logger("agent.onExit=shutdown — stopping everything.")
			stop()
		default: // ask
			if !interactive {
				// Headless (-p): no TTY to ask on — the command loop reads stdin.
				// Agent end = job done -> shut down cleanly.
				stop()
				return
			}
			switch promptAgentExit(success) {
			case "k":
				fmt.Fprintln(os.Stdout, "Editor keeps running. Launcher console:")
				mu.Lock()
				cmdLoopRunning = true
				mu.Unlock()
				go commandLoop(ctx, stop, sup, logger)
			case "r":
				fmt.Fprintln(os.Stdout, "Restarting agent …")
				if _, err := sup.StartService("agent"); err != nil {
					logger("Agent restart failed: " + err.Error() + " — stopping everything.")
					stop()
				}
			default: // Enter / q / Timeout / EOF
				stop()
			}
		}
	}
}

// promptAgentExit shows the selection menu on the real console (os.Stdout/Stdin —
// in window mode the launcher logs go to the file) and returns the choice. After
// 30s without input, "stop everything" applies (e.g. agent crashed overnight).
func promptAgentExit(success bool) string {
	if success {
		fmt.Fprintln(os.Stdout, "\nAgent exited.")
	} else {
		fmt.Fprintln(os.Stdout, "\nAgent crashed.")
	}
	fmt.Fprintln(os.Stdout, "  [Enter] stop everything (UE + launcher)")
	fmt.Fprintln(os.Stdout, "  [k]     keep the editor running, launcher console")
	fmt.Fprintln(os.Stdout, "  [r]     restart the agent")
	fmt.Fprint(os.Stdout, "> ")

	ch := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		if sc.Scan() {
			ch <- strings.ToLower(strings.TrimSpace(sc.Text()))
		} else {
			ch <- "" // EOF (e.g. stdin closed) -> stop everything
		}
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(30 * time.Second):
		fmt.Fprintln(os.Stdout, "\n(Timeout) — stopping everything.")
		return ""
	}
}

// commandLoop reads control commands from stdin (for manual operation).
func commandLoop(ctx context.Context, stop func(), sup *supervisor.Supervisor, logger func(string)) {
	logger("Commands: 'status' | 'r' (restart all) | 'r <name>' | 'stop <name>' | 'start <name>' | 'c <name>' | 'q'")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		fields := strings.Fields(strings.TrimSpace(sc.Text()))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "q", "quit", "exit":
			logger("Stopping …")
			stop()
			return
		case "status", "s":
			b, _ := json.MarshalIndent(sup.Status(), "", "  ")
			logger("\n" + string(b))
		case "r", "restart":
			if len(fields) > 1 {
				sup.RestartService(fields[1])
			} else {
				for _, n := range sup.ServiceNames() {
					sup.RestartService(n)
				}
			}
		case "stop":
			if len(fields) > 1 {
				sup.StopService(fields[1])
			}
		case "start":
			if len(fields) > 1 {
				sup.StartService(fields[1])
			}
		case "c", "cmd":
			if len(fields) > 1 {
				res, err := sup.RunCommand(fields[1])
				if err != nil {
					logger("Error: " + err.Error())
				} else {
					logger(fmt.Sprintf("exit %d\n%s", res.ExitCode, tailLines(res.Output, 100)))
				}
			}
		default:
			logger("Unknown command: " + fields[0])
		}
	}
}

// killCrashReporter terminates a possibly stuck crash reporter window.
func killCrashReporter() {
	var cmds [][]string
	if runtime.GOOS == "windows" {
		cmds = [][]string{
			{"taskkill", "/F", "/IM", "CrashReportClientEditor.exe"},
			{"taskkill", "/F", "/IM", "CrashReportClient.exe"},
		}
	} else {
		cmds = [][]string{{"pkill", "-f", "CrashReportClient"}}
	}
	for _, c := range cmds {
		_ = exec.Command(c[0], c[1:]...).Run() // ignore errors (the process may not exist)
	}
}

// cleanRecovery removes recovery/crash artefacts for a clean restart.
func cleanRecovery(projectDir string, logger func(string)) {
	saved := filepath.Join(projectDir, "Saved")
	restore := filepath.Join(saved, "Autosaves", "PackageRestoreData.json")
	if err := os.Remove(restore); err == nil {
		logger("Recovery: PackageRestoreData.json removed")
	}
	crashes := filepath.Join(saved, "Crashes")
	if entries, err := os.ReadDir(crashes); err == nil && len(entries) > 0 {
		for _, e := range entries {
			_ = os.RemoveAll(filepath.Join(crashes, e.Name()))
		}
		logger("Recovery: Saved/Crashes emptied")
	}
}
