package launcher

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimeinstall "github.com/Natsume-kkk/prompt-pane/internal/install"
	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

func TestManagedInvocationRequiresVersionedInstallState(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "Prompt Pane data"))
	launcherPath, err := runtimeinstall.LauncherPath()
	if err != nil {
		t.Fatal(err)
	}
	if managed, err := IsManagedInvocation(launcherPath); err != nil || managed {
		t.Fatalf("launcher without state managed = %v, err = %v", managed, err)
	}
	source := filepath.Join(root, "source.exe")
	if err := os.WriteFile(source, []byte("synthetic runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	release, _, err := runtimeinstall.Stage(source, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := runtimeinstall.InstallLauncher(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	state := runtimeinstall.State{SchemaVersion: runtimeinstall.SchemaVersion, LauncherSHA256: digest, Current: &release}
	if err := runtimeinstall.Save(state); err != nil {
		t.Fatal(err)
	}
	for _, invocation := range []string{launcherPath, filepath.Join(root, "bin", "codex.pp.exe")} {
		if managed, err := IsManagedInvocation(invocation); err != nil || !managed {
			t.Fatalf("managed invocation %q = %v, err = %v", invocation, managed, err)
		}
	}
	if managed, err := IsManagedInvocation(filepath.Join(root, "other.exe")); err != nil || managed {
		t.Fatalf("unmanaged invocation = %v, err = %v", managed, err)
	}
}

func TestLaunchEnvironmentReplacesPreparedGeneration(t *testing.T) {
	t.Setenv(runtimeinstall.EnvPreparedGeneration, "stale")
	environment := launchEnvironment(strings.Repeat("a", 64))
	var matches []string
	for _, entry := range environment {
		if strings.HasPrefix(strings.ToUpper(entry), strings.ToUpper(runtimeinstall.EnvPreparedGeneration)+"=") {
			matches = append(matches, entry)
		}
	}
	if len(matches) != 1 || matches[0] != runtimeinstall.EnvPreparedGeneration+"="+strings.Repeat("a", 64) {
		t.Fatalf("prepared generation entries = %q", matches)
	}
}

func TestActiveWorkspaceKeepsPendingVersionAndLaunchesCurrent(t *testing.T) {
	current, pending := prepareLauncherState(t)
	externalActivity, err := runcontext.AcquireWorkspaceActivity()
	if err != nil {
		t.Fatal(err)
	}
	defer externalActivity.Close()

	var calls []string
	app := App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	app.runProcess = func(path string, args []string, prepared string) (int, error) {
		calls = append(calls, filepath.Base(filepath.Dir(path))+":"+strings.Join(args, " ")+":"+prepared)
		if len(args) > 0 && args[0] == "codex" {
			if lock, err := runcontext.AcquireExclusiveWorkspaceActivity(); !errors.Is(err, runcontext.ErrWorkspacesActive) {
				if lock != nil {
					_ = lock.Close()
				}
				t.Fatalf("workspace activity was not held during managed launch: %v", err)
			}
		}
		return 0, nil
	}
	if code := app.Execute([]string{"codex", "resume"}); code != 0 {
		t.Fatalf("launch exit code = %d", code)
	}
	state, err := runtimeinstall.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Current.Generation != current.Generation || state.Pending == nil || state.Pending.Generation != pending.Generation {
		t.Fatalf("active launch changed versions: %#v", state)
	}
	if len(calls) != 3 || !strings.Contains(calls[0], pending.Generation+":_activate codex:") || !strings.Contains(calls[1], current.Generation+":_prepare codex:") || !strings.Contains(calls[2], current.Generation+":codex resume:"+current.Generation) {
		t.Fatalf("launch calls = %q", calls)
	}
}

func TestIdleLaunchActivatesPendingBeforeStartingWorkspace(t *testing.T) {
	_, pending := prepareLauncherState(t)
	var calls []string
	app := App{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	app.runProcess = func(path string, args []string, prepared string) (int, error) {
		calls = append(calls, filepath.Base(filepath.Dir(path))+":"+strings.Join(args, " ")+":"+prepared)
		if len(args) > 0 && args[0] == "_activate" {
			state, err := runtimeinstall.Load()
			if err != nil {
				return 1, err
			}
			state, err = runtimeinstall.ActivatePending(state)
			if err != nil {
				return 1, err
			}
			return 0, runtimeinstall.Save(state)
		}
		return 0, nil
	}
	if code := app.Execute([]string{"codex"}); code != 0 {
		t.Fatalf("launch exit code = %d", code)
	}
	state, err := runtimeinstall.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Current.Generation != pending.Generation || state.Pending != nil {
		t.Fatalf("idle launch did not activate pending version: %#v", state)
	}
	if len(calls) != 3 || !strings.Contains(calls[0], pending.Generation+":_activate codex:") || !strings.Contains(calls[1], pending.Generation+":_prepare codex:") || !strings.Contains(calls[2], pending.Generation+":codex:"+pending.Generation) {
		t.Fatalf("launch calls = %q", calls)
	}
}

func TestActivationFailureFallsBackToCurrentVersion(t *testing.T) {
	current, pending := prepareLauncherState(t)
	var calls []string
	var stderr bytes.Buffer
	app := App{Out: &bytes.Buffer{}, Err: &stderr}
	app.runProcess = func(path string, args []string, prepared string) (int, error) {
		calls = append(calls, filepath.Base(filepath.Dir(path))+":"+strings.Join(args, " ")+":"+prepared)
		if len(args) > 0 && args[0] == "_activate" {
			return 23, nil
		}
		return 0, nil
	}
	if code := app.Execute([]string{"codex", "resume"}); code != 0 {
		t.Fatalf("fallback launch exit code = %d", code)
	}
	state, err := runtimeinstall.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Current.Generation != current.Generation || state.Pending == nil || state.Pending.Generation != pending.Generation {
		t.Fatalf("activation failure changed versions: %#v", state)
	}
	if len(calls) != 3 || !strings.Contains(calls[0], pending.Generation+":_activate codex:") || !strings.Contains(calls[1], current.Generation+":_prepare codex:") || !strings.Contains(calls[2], current.Generation+":codex resume:"+current.Generation) {
		t.Fatalf("fallback calls = %q", calls)
	}
	if !strings.Contains(stderr.String(), "continuing with the current version") {
		t.Fatalf("fallback warning = %q", stderr.String())
	}
}

func prepareLauncherState(t *testing.T) (runtimeinstall.Release, runtimeinstall.Release) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "Prompt Pane data"))
	source := filepath.Join(root, "source.exe")
	if err := os.WriteFile(source, []byte("current runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	current, _, err := runtimeinstall.Stage(source, "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := runtimeinstall.InstallLauncher(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("pending runtime"), 0o700); err != nil {
		t.Fatal(err)
	}
	pending, _, err := runtimeinstall.Stage(source, "2.0.0")
	if err != nil {
		t.Fatal(err)
	}
	state := runtimeinstall.State{
		SchemaVersion: runtimeinstall.SchemaVersion, LauncherSHA256: digest, Current: &current, Pending: &pending,
	}
	if err := runtimeinstall.Save(state); err != nil {
		t.Fatal(err)
	}
	return current, pending
}
