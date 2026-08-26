package command

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	runtimeinstall "github.com/Natsume-kkk/prompt-pane/internal/install"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider"
	"github.com/Natsume-kkk/prompt-pane/internal/provider/codex"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/setupui"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
	"github.com/Natsume-kkk/prompt-pane/internal/ui"
	appversion "github.com/Natsume-kkk/prompt-pane/internal/version"
	"github.com/Natsume-kkk/prompt-pane/internal/zellij"
)

const Version = appversion.Current

const (
	hookRunEnvironmentExitCode = 10
	hookInputExitCode          = 11
	hookIPCExitCode            = 12
	turnObservationTimeout     = 24 * time.Hour
	stopHookFallbackDelay      = 3500 * time.Millisecond
)

type App struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func New() App {
	return App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}

func (a App) Execute(args []string) int {
	if len(args) == 0 {
		a.help()
		return 0
	}
	a = a.withInputFor(args[0])
	switch args[0] {
	case "codex":
		return a.launchCodex(trimSeparator(args[1:]))
	case "setup":
		if len(args) != 2 || args[1] != "codex" {
			return a.usageError("usage: prompt-pane setup codex")
		}
		return a.setupVersionedCodex()
	case "doctor":
		return a.checkEnvironment(false)
	case "teardown":
		if len(args) != 2 || args[1] != "codex" {
			return a.usageError("usage: prompt-pane teardown codex")
		}
		return a.teardownCodex()
	case "version", "--version", "-v":
		fmt.Fprintf(a.Out, "prompt-pane %s\n", Version)
		return 0
	case "help", "--help", "-h":
		a.help()
		return 0
	case "_agent":
		return a.agent(args[1:])
	case "_hook":
		return a.hook(args[1:])
	case "_observe":
		return a.observe(args[1:])
	case "_view":
		return a.view()
	case "_prepare":
		if len(args) != 2 || args[1] != "codex" {
			return a.usageError("invalid internal prepare invocation")
		}
		return a.prepareCodexLaunch()
	case "_activate":
		if len(args) != 2 || args[1] != "codex" {
			return a.usageError("invalid internal activation invocation")
		}
		return a.activatePendingCodex()
	default:
		return a.usageError("unknown command")
	}
}

func (a App) launchCodex(codexArgs []string) int {
	if err := requireWindowsX64(); err != nil {
		return a.fail(err.Error())
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return a.fail("Codex CLI was not found in PATH")
	}
	executable, err := os.Executable()
	if err != nil {
		return a.fail("cannot locate the Prompt Pane executable")
	}
	prepared, err := runtimeinstall.PreparedCurrentExecutable(executable)
	if err != nil {
		return a.fail(err.Error())
	}
	if !prepared {
		gate, err := acquireUpdateGate()
		if err != nil {
			return a.fail(err.Error())
		}
		defer gate.Close()
		if _, err := a.ensureCodexSetup(codexPath, executable, true, acquireExclusiveWorkspace); err != nil {
			return a.fail(err.Error())
		}
		activity, err := runcontext.AcquireWorkspaceActivity()
		if err != nil {
			return a.fail("cannot register the Prompt Pane workspace as active")
		}
		defer activity.Close()
		if err := gate.Close(); err != nil {
			return a.fail("cannot release the Prompt Pane update gate")
		}
	}
	zellijPath, err := zellij.Find()
	if err != nil {
		return a.fail("Zellij is not ready after setup verification")
	}
	run, err := runcontext.New()
	if err != nil {
		return a.fail("cannot create an isolated Prompt Pane run")
	}
	server := ipc.NewServer(run)
	if err := server.Start(); err != nil {
		return a.fail(err.Error())
	}
	defer server.Close()
	if err := zellij.Launch(zellijPath, executable, run, codexArgs, a.In, a.Out, a.Err); err != nil {
		return a.fail(err.Error())
	}
	return 0
}

func writeSetupCompletion(out io.Writer, firstInstall bool) {
	fmt.Fprintln(out, "Setup complete.")
	if !firstInstall {
		fmt.Fprintln(out, "Run `codex.pp` when ready.")
		return
	}
	fmt.Fprintln(out, "1. Run `codex.pp`.")
	fmt.Fprintln(out, "2. Submit your first prompt.")
	fmt.Fprintln(out, "3. If it does not appear, open `/hooks` in Codex, review and trust Prompt Pane, then restart `codex.pp`.")
}

func (a App) ensureCodexSetup(codexPath, executable string, beforeLaunch bool, prepareChange func() (func(), error)) (bool, error) {
	_, zellijErr := zellij.Find()
	zellijReady := zellijErr == nil
	pluginReady := codex.PluginInstalled(codexPath)
	aliasPath, aliasReady, err := shortcut.Installed(codexPath)
	if err != nil {
		return false, err
	}
	aliasManaged, err := shortcut.Managed(codexPath)
	if err != nil {
		return false, err
	}
	if !aliasReady {
		if err := shortcut.Preflight(codexPath, executable); err != nil {
			return false, err
		}
	}
	if zellijReady && pluginReady && aliasReady {
		return false, nil
	}
	if _, err := findPowerShell(); err != nil {
		return false, err
	}
	releaseChange, err := prepareChange()
	if err != nil {
		return false, err
	}
	defer releaseChange()

	fmt.Fprintln(a.Out, "Prompt Pane will prepare the missing or outdated components:")
	if !zellijReady {
		fmt.Fprintf(a.Out, "- Zellij %s in your Prompt Pane user data directory\n", zellij.Version)
	}
	if !pluginReady {
		fmt.Fprintln(a.Out, "- The local Codex plugin for authenticated prompt delivery")
	}
	if !aliasReady {
		fmt.Fprintf(a.Out, "- codex.pp at %s\n", aliasPath)
	}
	completion := setupCompletionMode(beforeLaunch, aliasManaged)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	transaction, err := captureSetupTransaction(codexPath, !pluginReady, !aliasReady, false)
	if err != nil {
		return false, fmt.Errorf("prepare installation rollback: %w", err)
	}
	defer transaction.discard()
	steps := setupStepCount(zellijReady, pluginReady, aliasReady)
	initial := setupui.Progress{
		Step: 1, Steps: steps, Stage: "Checking environment",
		Plan: setupPlan(zellijReady, pluginReady, aliasReady),
	}
	err = setupui.Run(a.Out, completion, initial, func(report setupui.Reporter) error {
		step := 1
		if !zellijReady {
			if err := zellij.PreflightManagedInstallAccess(); err != nil {
				return err
			}
		}
		if !pluginReady {
			if err := codex.PreflightInstallAccess(); err != nil {
				return err
			}
		}
		if !aliasReady {
			if err := shortcut.PreflightInstallAccess(codexPath); err != nil {
				return err
			}
		}
		report(setupui.Progress{Step: step, Steps: steps, Stage: "Environment ready"})

		if !zellijReady {
			step++
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Downloading Zellij"})
			lastReport := time.Time{}
			lastPercent := -1
			_, err := zellij.InstallManagedWithProgress(ctx, func(downloaded, total int64) {
				stagePercent := 0
				if total > 0 {
					stagePercent = int(min(downloaded, total) * 100 / total)
				}
				now := time.Now()
				if stagePercent == lastPercent && now.Sub(lastReport) < 200*time.Millisecond && downloaded != total {
					return
				}
				lastPercent = stagePercent
				lastReport = now
				report(setupui.Progress{
					Step: step, Steps: steps, Stage: "Downloading Zellij",
					Downloaded: downloaded, Total: total,
				})
			})
			if err != nil {
				return err
			}
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Zellij ready"})
		}

		if !pluginReady {
			step++
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Installing Codex plugin"})
			transaction.pluginChanged = true
			if err := codex.InstallPlugin(codexPath); err != nil {
				return err
			}
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Codex plugin ready"})
		}

		if !aliasReady {
			step++
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Installing codex.pp"})
			transaction.aliasChanged = true
			if _, err := shortcut.Install(codexPath, executable); err != nil {
				return err
			}
			report(setupui.Progress{Step: step, Steps: steps, Stage: "codex.pp ready"})
		}

		step++
		report(setupui.Progress{Step: step, Steps: steps, Stage: "Verifying installation"})
		if _, err := zellij.Find(); err != nil {
			return fmt.Errorf("verify Zellij installation: %w", err)
		}
		if !codex.PluginInstalled(codexPath) {
			return fmt.Errorf("verify Codex plugin installation")
		}
		if _, installed, err := shortcut.Installed(codexPath); err != nil || !installed {
			if err != nil {
				return fmt.Errorf("verify codex.pp installation: %w", err)
			}
			return fmt.Errorf("verify codex.pp installation")
		}
		report(setupui.Progress{Step: step, Steps: steps, Stage: "Installation verified"})
		return nil
	})
	if err != nil {
		return false, transaction.rollbackAfter(err)
	}
	return true, nil
}

func acquireUpdateGate() (runcontext.CoordinationLock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return runcontext.AcquireUpdateGate(ctx)
}

func acquireExclusiveWorkspace() (func(), error) {
	lock, err := runcontext.AcquireExclusiveWorkspaceActivity()
	if err != nil {
		return nil, err
	}
	return func() { _ = lock.Close() }, nil
}

func (a App) checkEnvironment(afterSetup bool) int {
	ok := true
	prerequisitesReady := true
	platformReady := requireWindowsX64() == nil
	if !platformReady {
		fmt.Fprintf(a.Out, "[FAIL] Platform: %s/%s; Windows x64 is required\n", runtime.GOOS, runtime.GOARCH)
		ok = false
		prerequisitesReady = false
	} else {
		fmt.Fprintln(a.Out, "[OK]   Platform: Windows x64")
	}
	if !platformReady {
		fmt.Fprintln(a.Out, "[FAIL] PowerShell: not checked because Windows x64 is required")
		prerequisitesReady = false
	} else if shell, err := findPowerShell(); err != nil {
		fmt.Fprintf(a.Out, "[FAIL] PowerShell: %s\n", err)
		ok = false
		prerequisitesReady = false
	} else {
		fmt.Fprintf(a.Out, "[OK]   PowerShell %s: %s\n", shell.Version, shell.Path)
	}
	executable, executableErr := os.Executable()
	pluginExecutable := executable
	if executableErr != nil {
		fmt.Fprintln(a.Out, "[FAIL] Prompt Pane executable: cannot locate the current program")
		ok = false
		prerequisitesReady = false
	} else {
		fmt.Fprintf(a.Out, "[OK]   Prompt Pane executable: %s\n", executable)
	}
	installState, installStateReady, installStateErr := runtimeinstall.LoadIfPresent()
	if installStateErr != nil {
		fmt.Fprintf(a.Out, "[FAIL] Prompt Pane install state: %s\n", installStateErr)
		ok = false
	} else if !installStateReady {
		fmt.Fprintln(a.Out, "[FAIL] Prompt Pane install state: versioned installation is not initialized")
		ok = false
	} else {
		currentPath, currentPathErr := runtimeinstall.RuntimePath(*installState.Current)
		if currentPathErr != nil || runtimeinstall.VerifyRelease(*installState.Current) != nil {
			fmt.Fprintln(a.Out, "[FAIL] Prompt Pane current version: missing or invalid")
			ok = false
		} else {
			pluginExecutable = currentPath
			fmt.Fprintf(a.Out, "[OK]   Prompt Pane current version: %s at %s\n", runtimeinstall.ReleaseLabel(*installState.Current), currentPath)
		}
		if installState.Pending != nil {
			if err := runtimeinstall.VerifyRelease(*installState.Pending); err != nil {
				fmt.Fprintf(a.Out, "[FAIL] Prompt Pane pending version: %s\n", err)
				ok = false
			} else {
				fmt.Fprintf(a.Out, "[INFO] Prompt Pane pending version: %s; activates after all workspaces close\n", runtimeinstall.ReleaseLabel(*installState.Pending))
			}
		}
		if launcherReady, err := runtimeinstall.LauncherReady(installState); err != nil {
			fmt.Fprintf(a.Out, "[FAIL] Prompt Pane launcher: %s\n", err)
			ok = false
		} else if !launcherReady {
			fmt.Fprintln(a.Out, "[FAIL] Prompt Pane launcher: missing or modified")
			ok = false
		} else if launcherPath, err := runtimeinstall.LauncherPath(); err != nil {
			fmt.Fprintf(a.Out, "[FAIL] Prompt Pane launcher: %s\n", err)
			ok = false
		} else {
			fmt.Fprintf(a.Out, "[OK]   Prompt Pane launcher: %s\n", launcherPath)
		}
	}
	if codexPath, err := exec.LookPath("codex"); err != nil {
		fmt.Fprintln(a.Out, "[FAIL] Codex CLI: not found")
		ok = false
		prerequisitesReady = false
	} else {
		fmt.Fprintf(a.Out, "[OK]   Codex CLI: %s\n", codexPath)
		if pluginExecutable != "" && codex.PluginInstalledFor(codexPath, pluginExecutable) {
			fmt.Fprintln(a.Out, "[OK]   Codex plugin: installed and enabled")
		} else {
			fmt.Fprintln(a.Out, "[FAIL] Codex plugin: not installed or disabled")
			ok = false
		}
		if executableErr != nil {
			fmt.Fprintln(a.Out, "[FAIL] codex.pp: cannot locate the Prompt Pane executable")
			ok = false
		} else if path, installed, err := shortcut.Installed(codexPath); err != nil {
			fmt.Fprintf(a.Out, "[FAIL] codex.pp: %s\n", err)
			ok = false
		} else if !installed {
			fmt.Fprintf(a.Out, "[FAIL] codex.pp: not installed or outdated (%s)\n", path)
			ok = false
		} else {
			fmt.Fprintf(a.Out, "[OK]   codex.pp: %s\n", path)
		}
	}
	if path, err := zellij.Find(); err != nil {
		fmt.Fprintf(a.Out, "[FAIL] Zellij %s: not found\n", zellij.Version)
		ok = false
	} else {
		fmt.Fprintf(a.Out, "[OK]   Zellij %s: %s\n", zellij.Version, path)
	}
	if ok {
		fmt.Fprintln(a.Out, "Environment is ready. Hook trust is not detectable; review and trust it in Codex with `/hooks` if needed.")
		return 0
	}
	fmt.Fprintln(a.Out, doctorFailureMessage(prerequisitesReady, afterSetup))
	return 1
}

func doctorFailureMessage(prerequisitesReady, afterSetup bool) string {
	if afterSetup {
		return "Setup could not verify a ready environment. Review the [FAIL] checks above."
	}
	if prerequisitesReady {
		return "Environment is not ready. Run `prompt-pane setup codex`, then rerun `prompt-pane doctor`."
	}
	return "Environment is not ready. Resolve the [FAIL] prerequisites, then rerun `prompt-pane doctor`."
}

func setupStepCount(zellijReady, pluginReady, aliasReady bool) int {
	steps := 2
	if !zellijReady {
		steps++
	}
	if !pluginReady {
		steps++
	}
	if !aliasReady {
		steps++
	}
	return steps
}

func setupPlan(zellijReady, pluginReady, aliasReady bool) []string {
	plan := []string{"Environment"}
	if !zellijReady {
		plan = append(plan, "Zellij "+zellij.Version)
	}
	if !pluginReady {
		plan = append(plan, "Codex plugin")
	}
	if !aliasReady {
		plan = append(plan, "codex.pp")
	}
	return append(plan, "Installation verification")
}

func setupCompletionMode(beforeLaunch, aliasManaged bool) setupui.CompletionMode {
	if beforeLaunch {
		return setupui.RepairCompletion
	}
	if aliasManaged {
		return setupui.RefreshCompletion
	}
	return setupui.SetupCompletion
}

func (a App) teardownCodex() int {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return a.fail("Codex CLI was not found in PATH")
	}
	if err := shortcut.PreflightRemove(codexPath); err != nil {
		return a.fail(err.Error())
	}
	if !a.confirm("Remove codex.pp, the Prompt Pane Codex plugin, and its local marketplace files? [y/N] ") {
		return a.fail("teardown cancelled; nothing was removed")
	}
	aliasRemoved, err := shortcut.Remove(codexPath)
	if err != nil {
		return a.fail(err.Error())
	}
	if err := codex.RemovePlugin(codexPath); err != nil {
		return a.fail(err.Error())
	}
	if aliasRemoved {
		fmt.Fprintln(a.Out, "Removed codex.pp and the Prompt Pane Codex integration. Managed Zellij was kept.")
	} else {
		fmt.Fprintln(a.Out, "Removed the Prompt Pane Codex integration. No managed codex.pp was present; managed Zellij was kept.")
	}
	return 0
}

func (a App) agent(args []string) int {
	if len(args) == 0 || args[0] != "codex" {
		return a.usageError("invalid internal agent command")
	}
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return a.fail("Codex CLI was not found")
	}
	command := exec.Command(codexPath, trimSeparator(args[1:])...)
	command.Env = os.Environ()
	command.Stdin = a.In
	command.Stdout = a.Out
	command.Stderr = a.Err
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		return a.fail("could not start Codex")
	}
	return 0
}

func (a App) hook(args []string) int {
	if len(args) != 1 || args[0] != "codex" {
		return a.usageError("invalid internal hook command")
	}
	if os.Getenv(runcontext.EnvRunID) == "" && os.Getenv(runcontext.EnvToken) == "" && os.Getenv(runcontext.EnvEndpoint) == "" {
		// The installed hook is global to Codex, but only Prompt Pane launches carry
		// the authenticated run environment. Other Codex sessions stay unaffected.
		return 0
	}
	run, err := runcontext.FromEnvironment()
	if err != nil {
		return a.failCode(hookRunEnvironmentExitCode, "Prompt Pane hook is not attached to an active run")
	}
	event, observation, err := codex.DecodeHookWithObservation(a.In)
	if err != nil {
		return a.failCode(hookInputExitCode, err.Error())
	}
	if observation != nil {
		prepared, prepareErr := codex.PrepareTurnObservation(*observation)
		if prepareErr == nil {
			observation = &prepared
		} else {
			observation = nil
		}
	}
	ctx, cancel := ipc.HookContext()
	defer cancel()
	if err := ipc.SendEvent(ctx, run, event); err != nil {
		return a.failCode(hookIPCExitCode, "Prompt Pane hook could not reach the local viewer")
	}
	if observation != nil {
		if executable, executableErr := os.Executable(); executableErr == nil {
			_ = codex.StartTurnObserver(executable, *observation)
		}
	}
	return 0
}

func (a App) observe(args []string) int {
	run, err := runcontext.FromEnvironment()
	if err != nil {
		return 0
	}
	observation, err := codex.ParseTurnObservation(args)
	if err != nil {
		return 0
	}
	_ = observeTurn(run, observation)
	return 0
}

func observeTurn(run runcontext.Context, observation codex.TurnObservation) error {
	ctx, cancel := context.WithTimeout(context.Background(), turnObservationTimeout)
	defer cancel()
	end, err := codex.WaitForTurnEnd(ctx, observation)
	if err != nil {
		return err
	}
	if end == codex.TurnEndComplete {
		timer := time.NewTimer(stopHookFallbackDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	sendContext, sendCancel := ipc.HookContext()
	defer sendCancel()
	return ipc.SendEvent(sendContext, run, provider.Event{
		Kind:      provider.TurnCompleted,
		SessionID: observation.SessionID,
		TurnID:    observation.TurnID,
	})
}

func (a App) view() int {
	run, err := runcontext.FromEnvironment()
	if err != nil {
		return a.fail(err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, decoder, err := ipc.Subscribe(ctx, run)
	if err != nil {
		return a.fail(err.Error())
	}
	defer conn.Close()
	program := tea.NewProgram(ui.New(decoder))
	finalModel, err := program.Run()
	if err != nil {
		return a.fail("Prompt Pane viewer stopped unexpectedly")
	}
	if viewerCloseRequested(finalModel) {
		if err := zellij.ClosePane(os.Getenv(zellij.EnvExecutable), os.Getenv(zellij.EnvPaneID)); err != nil {
			return a.fail(err.Error())
		}
	}
	return 0
}

func viewerCloseRequested(model tea.Model) bool {
	viewer, ok := model.(interface{ CloseRequested() bool })
	return ok && viewer.CloseRequested()
}

func (a App) confirm(prompt string) bool {
	fmt.Fprint(a.Out, prompt)
	reader, ok := a.In.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(a.In)
	}
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

func (a App) withBufferedInput() App {
	if _, ok := a.In.(*bufio.Reader); !ok {
		a.In = bufio.NewReader(a.In)
	}
	return a
}

func (a App) withInputFor(command string) App {
	if command == "teardown" {
		return a.withBufferedInput()
	}
	return a
}

func (a App) help() {
	fmt.Fprintln(a.Out, `Prompt Pane shows the current AI CLI session's user prompts in a companion pane.

Usage:
  codex.pp [<codex-args>]
  prompt-pane codex [-- <codex-args>]
  prompt-pane setup codex
  prompt-pane doctor
  prompt-pane teardown codex
  prompt-pane version`)
}

func (a App) usageError(message string) int {
	fmt.Fprintln(a.Err, "prompt-pane:", message)
	return 2
}

func (a App) fail(message string) int {
	return a.failCode(1, message)
}

func (a App) failCode(code int, message string) int {
	fmt.Fprintln(a.Err, "prompt-pane:", message)
	return code
}

func trimSeparator(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}
