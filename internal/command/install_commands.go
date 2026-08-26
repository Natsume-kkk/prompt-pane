package command

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	runtimeinstall "github.com/Natsume-kkk/prompt-pane/internal/install"
	"github.com/Natsume-kkk/prompt-pane/internal/provider/codex"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
	"github.com/Natsume-kkk/prompt-pane/internal/zellij"
)

func (a App) setupVersionedCodex() int {
	codexPath, executable, err := resolveInstallEnvironment()
	if err != nil {
		return a.fail(err.Error())
	}
	gate, err := acquireUpdateGate()
	if err != nil {
		return a.fail(err.Error())
	}
	defer gate.Close()
	state, installed, err := runtimeinstall.LoadIfPresent()
	if err != nil {
		return a.fail(err.Error())
	}
	if installed {
		if err := runtimeinstall.VerifyRelease(*state.Current); err != nil {
			return a.fail(err.Error())
		}
	}
	if !installed {
		releaseChange, lockErr := acquireExclusiveWorkspace()
		if errors.Is(lockErr, runcontext.ErrWorkspacesActive) {
			return a.fail("close all running Prompt Pane workspaces once to migrate the existing single-version installation, then rerun setup")
		}
		if lockErr != nil {
			return a.fail(lockErr.Error())
		}
		defer releaseChange()
		previouslyManaged, err := shortcut.Managed(codexPath)
		if err != nil {
			return a.fail(err.Error())
		}
		if err := runtimeinstall.PreflightAccess(); err != nil {
			return a.fail(err.Error())
		}
		release, _, err := runtimeinstall.Stage(executable, Version)
		if err != nil {
			return a.fail(err.Error())
		}
		if err := a.bootstrapVersionedInstallation(codexPath, executable, release); err != nil {
			return a.fail(err.Error())
		}
		fmt.Fprintln(a.Out, "Running final checks.")
		if code := a.checkEnvironment(true); code != 0 {
			return code
		}
		writeSetupCompletion(a.Out, !previouslyManaged)
		return 0
	}
	if err := runtimeinstall.PreflightAccess(); err != nil {
		return a.fail(err.Error())
	}
	release, _, err := runtimeinstall.Stage(executable, Version)
	if err != nil {
		return a.fail(err.Error())
	}

	next, pendingChanged, err := runtimeinstall.SetPending(state, release)
	if err != nil {
		return a.fail(err.Error())
	}
	if pendingChanged {
		if err := runtimeinstall.Save(next); err != nil {
			return a.fail(err.Error())
		}
		state = next
		reportCleanupWarnings(a.Err, runtimeinstall.CleanupUnreferenced(state))
	}
	if state.Current.Generation == release.Generation {
		changed, err := a.ensureCurrentInstallation(codexPath, executable, state, false)
		if err != nil {
			return a.fail(err.Error())
		}
		if !changed {
			fmt.Fprintln(a.Out, "Managed components are already up to date. Running final checks.")
		}
		if code := a.checkEnvironment(true); code != 0 {
			return code
		}
		writeSetupCompletion(a.Out, false)
		return 0
	}

	releaseChange, lockErr := acquireExclusiveWorkspace()
	if errors.Is(lockErr, runcontext.ErrWorkspacesActive) {
		writeStagedCompletion(a.Out, *state.Current, release)
		return 0
	}
	if lockErr != nil {
		return a.fail(lockErr.Error())
	}
	defer releaseChange()
	if err := a.activatePendingLocked(codexPath, executable, state, false); err != nil {
		return a.fail(err.Error())
	}
	fmt.Fprintln(a.Out, "Running final checks.")
	if code := a.checkEnvironment(true); code != 0 {
		return code
	}
	writeSetupCompletion(a.Out, false)
	return 0
}

func (a App) prepareCodexLaunch() int {
	codexPath, executable, err := resolveInstallEnvironment()
	if err != nil {
		return a.fail(err.Error())
	}
	gate, err := acquireUpdateGate()
	if err != nil {
		return a.fail(err.Error())
	}
	defer gate.Close()
	state, err := runtimeinstall.Load()
	if err != nil {
		return a.fail(err.Error())
	}
	if !runtimeinstall.ReleasePathMatches(*state.Current, executable) {
		return a.fail("the managed launcher selected a Prompt Pane version that is not current")
	}
	if _, err := a.ensureCurrentInstallation(codexPath, executable, state, true); err != nil {
		return a.fail(err.Error())
	}
	return 0
}

func (a App) activatePendingCodex() int {
	codexPath, executable, err := resolveInstallEnvironment()
	if err != nil {
		return a.fail(err.Error())
	}
	gate, err := acquireUpdateGate()
	if err != nil {
		return a.fail(err.Error())
	}
	defer gate.Close()
	state, err := runtimeinstall.Load()
	if err != nil {
		return a.fail(err.Error())
	}
	if state.Pending == nil {
		return 0
	}
	if !runtimeinstall.ReleasePathMatches(*state.Pending, executable) {
		return a.fail("only the staged Prompt Pane version can activate itself")
	}
	releaseChange, lockErr := acquireExclusiveWorkspace()
	if errors.Is(lockErr, runcontext.ErrWorkspacesActive) {
		return 0
	}
	if lockErr != nil {
		return a.fail(lockErr.Error())
	}
	defer releaseChange()
	if err := a.activatePendingLocked(codexPath, executable, state, true); err != nil {
		return a.fail(err.Error())
	}
	return 0
}

func resolveInstallEnvironment() (string, string, error) {
	if err := requireWindowsX64(); err != nil {
		return "", "", err
	}
	if _, err := findPowerShell(); err != nil {
		return "", "", err
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return "", "", fmt.Errorf("Codex CLI was not found in PATH")
	}
	executable, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("cannot locate the Prompt Pane executable")
	}
	return codexPath, executable, nil
}

func (a App) bootstrapVersionedInstallation(codexPath, executable string, release runtimeinstall.Release) error {
	transaction, err := captureSetupTransaction(codexPath, true, true, true)
	if err != nil {
		return fmt.Errorf("prepare installation rollback: %w", err)
	}
	defer transaction.discard()
	transaction.pluginChanged = true
	transaction.aliasChanged = true
	transaction.installChanged = true

	launcherDigest, err := runtimeinstall.InstallLauncher(executable, nil)
	if err != nil {
		return transaction.rollbackAfter(err)
	}
	if _, err := a.ensureCodexSetup(codexPath, executable, false, noWorkspaceChange); err != nil {
		return transaction.rollbackAfter(err)
	}
	state := runtimeinstall.State{
		SchemaVersion:  runtimeinstall.SchemaVersion,
		LauncherSHA256: launcherDigest,
		Current:        &release,
	}
	if err := runtimeinstall.Save(state); err != nil {
		return transaction.rollbackAfter(err)
	}
	if err := verifyVersionedInstallation(codexPath, executable, state); err != nil {
		return transaction.rollbackAfter(err)
	}
	reportCleanupWarnings(a.Err, runtimeinstall.CleanupUnreferenced(state))
	return nil
}

func (a App) ensureCurrentInstallation(codexPath, executable string, state runtimeinstall.State, beforeLaunch bool) (bool, error) {
	launcherReady, err := runtimeinstall.LauncherReady(state)
	if err != nil {
		return false, err
	}
	if launcherReady {
		return a.ensureCodexSetup(codexPath, executable, beforeLaunch, acquireExclusiveWorkspace)
	}
	releaseChange, err := acquireExclusiveWorkspace()
	if err != nil {
		return false, err
	}
	defer releaseChange()
	transaction, err := captureSetupTransaction(codexPath, true, true, true)
	if err != nil {
		return false, fmt.Errorf("prepare installation rollback: %w", err)
	}
	defer transaction.discard()
	transaction.pluginChanged = true
	transaction.aliasChanged = true
	transaction.installChanged = true
	launcherDigest, err := runtimeinstall.InstallLauncher(executable, &state)
	if err == nil {
		state.LauncherSHA256 = launcherDigest
		err = runtimeinstall.Save(state)
	}
	if err == nil {
		_, err = a.ensureCodexSetup(codexPath, executable, beforeLaunch, noWorkspaceChange)
	}
	if err == nil {
		err = verifyVersionedInstallation(codexPath, executable, state)
	}
	if err != nil {
		return false, transaction.rollbackAfter(err)
	}
	return true, nil
}

func (a App) activatePendingLocked(codexPath, executable string, state runtimeinstall.State, beforeLaunch bool) error {
	if state.Pending == nil {
		return nil
	}
	if err := runtimeinstall.VerifyRelease(*state.Pending); err != nil {
		return err
	}
	transaction, err := captureSetupTransaction(codexPath, true, true, true)
	if err != nil {
		return fmt.Errorf("prepare installation rollback: %w", err)
	}
	defer transaction.discard()
	transaction.pluginChanged = true
	transaction.aliasChanged = true
	transaction.installChanged = true

	launcherDigest, err := runtimeinstall.InstallLauncher(executable, &state)
	if err != nil {
		return transaction.rollbackAfter(err)
	}
	state.LauncherSHA256 = launcherDigest
	if _, err := a.ensureCodexSetup(codexPath, executable, beforeLaunch, noWorkspaceChange); err != nil {
		return transaction.rollbackAfter(err)
	}
	next, err := runtimeinstall.ActivatePending(state)
	if err != nil {
		return transaction.rollbackAfter(err)
	}
	if err := runtimeinstall.Save(next); err != nil {
		return transaction.rollbackAfter(err)
	}
	if err := verifyVersionedInstallation(codexPath, executable, next); err != nil {
		return transaction.rollbackAfter(err)
	}
	reportCleanupWarnings(a.Err, runtimeinstall.CleanupUnreferenced(next))
	fmt.Fprintf(a.Out, "Activated Prompt Pane %s.\n", runtimeinstall.ReleaseLabel(*next.Current))
	return nil
}

func verifyVersionedInstallation(codexPath, executable string, want runtimeinstall.State) error {
	got, err := runtimeinstall.Load()
	if err != nil {
		return fmt.Errorf("verify Prompt Pane install state: %w", err)
	}
	if got.SchemaVersion != want.SchemaVersion || got.LauncherSHA256 != want.LauncherSHA256 ||
		!sameInstallRelease(got.Current, want.Current) ||
		!sameInstallRelease(got.Pending, want.Pending) ||
		!sameInstallRelease(got.Previous, want.Previous) {
		return fmt.Errorf("verify Prompt Pane install state")
	}
	if err := runtimeinstall.VerifyRelease(*got.Current); err != nil {
		return err
	}
	ready, err := runtimeinstall.LauncherReady(got)
	if err != nil || !ready {
		if err != nil {
			return fmt.Errorf("verify Prompt Pane launcher: %w", err)
		}
		return fmt.Errorf("verify Prompt Pane launcher")
	}
	if !codex.PluginInstalled(codexPath) {
		return fmt.Errorf("verify Codex plugin installation")
	}
	if _, ready, err := shortcut.Installed(codexPath); err != nil || !ready {
		if err != nil {
			return fmt.Errorf("verify codex.pp installation: %w", err)
		}
		return fmt.Errorf("verify codex.pp installation")
	}
	if _, err := zellij.Find(); err != nil {
		return fmt.Errorf("verify Zellij installation: %w", err)
	}
	if !runtimeinstall.ReleasePathMatches(*got.Current, executable) {
		digest, hashErr := runtimeinstall.HashFile(executable)
		if hashErr != nil || digest != got.Current.Generation {
			return fmt.Errorf("verify active Prompt Pane executable")
		}
	}
	return nil
}

func sameInstallRelease(left, right *runtimeinstall.Release) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Generation == right.Generation && left.Version == right.Version
}

func writeStagedCompletion(out io.Writer, current, pending runtimeinstall.Release) {
	fmt.Fprintf(out, "Prompt Pane %s is staged and verified.\n", runtimeinstall.ReleaseLabel(pending))
	fmt.Fprintf(out, "Running and newly opened workspaces will continue using %s until all Prompt Pane workspaces close.\n", runtimeinstall.ReleaseLabel(current))
	fmt.Fprintln(out, "The staged version will activate automatically the next time you run `codex.pp`.")
}

func reportCleanupWarnings(out io.Writer, cleanupErrors []error) {
	for _, err := range cleanupErrors {
		fmt.Fprintf(out, "prompt-pane: warning: %v; the unused version will be retried during a later upgrade\n", err)
	}
}

func noWorkspaceChange() (func(), error) {
	return func() {}, nil
}
