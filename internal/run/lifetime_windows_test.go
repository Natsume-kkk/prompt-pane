//go:build windows

package run

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const lifetimeHelperEnvironment = "PROMPT_PANE_LIFETIME_HELPER"

const leafLifetimeRoleEnvironment = "PROMPT_PANE_LEAF_LIFETIME_ROLE"

func TestProcessLifetimeKillsChildrenWhenOwnerStops(t *testing.T) {
	if os.Getenv(lifetimeHelperEnvironment) == "1" {
		runLifetimeHelper(t)
		return
	}

	pidFile := t.TempDir() + `\child.pid`
	command := exec.Command(os.Args[0], "-test.run=^TestProcessLifetimeKillsChildrenWhenOwnerStops$")
	command.Env = append(os.Environ(), lifetimeHelperEnvironment+"=1", "PROMPT_PANE_LIFETIME_PID_FILE="+pidFile)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
		}
	})

	childPID := waitForLifetimeChildPID(t, pidFile, &stderr)
	if err := command.Wait(); err != nil {
		t.Fatalf("wait for lifetime owner: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	deadline := time.Now().Add(3 * time.Second)
	for processIsRunning(childPID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processIsRunning(childPID) {
		t.Fatalf("child process %d survived its process lifetime owner", childPID)
	}
}

func TestInheritedLifetimeLetsHookObserverSurviveHookAndDieWithWorkspace(t *testing.T) {
	switch os.Getenv(leafLifetimeRoleEnvironment) {
	case "workspace":
		runLeafLifetimeWorkspace(t)
		return
	case "hook":
		runLeafLifetimeHook(t)
		return
	}

	directory := t.TempDir()
	pidFile := directory + `\observer.pid`
	readyFile := directory + `\hook-exited`
	workspace := exec.Command(os.Args[0], "-test.run=^TestInheritedLifetimeLetsHookObserverSurviveHookAndDieWithWorkspace$")
	workspace.Env = append(os.Environ(),
		leafLifetimeRoleEnvironment+"=workspace",
		"PROMPT_PANE_LEAF_OBSERVER_PID_FILE="+pidFile,
		"PROMPT_PANE_LEAF_HOOK_EXITED_FILE="+readyFile,
	)
	var stderr bytes.Buffer
	workspace.Stderr = &stderr
	if err := workspace.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if workspace.ProcessState == nil {
			_ = workspace.Process.Kill()
		}
	})

	observerPID := waitForLifetimeChildPID(t, pidFile, &stderr)
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hook did not exit: %s", strings.TrimSpace(stderr.String()))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !processIsRunning(observerPID) {
		t.Fatalf("observer process %d did not survive its hook parent", observerPID)
	}
	if err := workspace.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = workspace.Wait()
	deadline = time.Now().Add(3 * time.Second)
	for processIsRunning(observerPID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if processIsRunning(observerPID) {
		t.Fatalf("observer process %d survived its workspace owner", observerPID)
	}
}

func runLeafLifetimeWorkspace(t *testing.T) {
	if err := EnsureProcessLifetime(); err != nil {
		t.Fatal(err)
	}
	hook := exec.Command(os.Args[0], "-test.run=^TestInheritedLifetimeLetsHookObserverSurviveHookAndDieWithWorkspace$")
	hook.Env = append(os.Environ(), leafLifetimeRoleEnvironment+"=hook")
	if err := hook.Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("PROMPT_PANE_LEAF_HOOK_EXITED_FILE"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Second)
}

func runLeafLifetimeHook(t *testing.T) {
	observer := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30")
	if err := observer.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("PROMPT_PANE_LEAF_OBSERVER_PID_FILE"), []byte(strconv.Itoa(observer.Process.Pid)), 0o600); err != nil {
		_ = observer.Process.Kill()
		t.Fatal(err)
	}
	if err := observer.Process.Release(); err != nil {
		t.Fatal(err)
	}
}

func runLifetimeHelper(t *testing.T) {
	if err := EnsureProcessLifetime(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	pidFile := os.Getenv("PROMPT_PANE_LIFETIME_PID_FILE")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}

func waitForLifetimeChildPID(t *testing.T, path string, stderr *bytes.Buffer) uint32 {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if err != nil {
				t.Fatalf("parse lifetime child PID: %v", err)
			}
			return uint32(pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("lifetime helper did not publish child PID: %s", strings.TrimSpace(stderr.String()))
	return 0
}

func processIsRunning(pid uint32) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == 259
}

func TestProcessLifetimeUsesKillOnClose(t *testing.T) {
	job, err := newKillOnCloseJob()
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(job)

	queried := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	if err := windows.QueryInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&queried)), uint32(unsafe.Sizeof(queried)), nil); err != nil {
		t.Fatal(err)
	}
	if queried.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 {
		t.Fatalf("process lifetime flags = %#x, want KILL_ON_JOB_CLOSE", queried.BasicLimitInformation.LimitFlags)
	}
}
