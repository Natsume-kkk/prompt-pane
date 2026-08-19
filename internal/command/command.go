package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Natsume-kkk/prompt-pane/internal/config"
	"github.com/Natsume-kkk/prompt-pane/internal/ipc"
	"github.com/Natsume-kkk/prompt-pane/internal/provider/codex"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/setupui"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
	"github.com/Natsume-kkk/prompt-pane/internal/ui"
	appversion "github.com/Natsume-kkk/prompt-pane/internal/version"
	"github.com/Natsume-kkk/prompt-pane/internal/zellij"
)

const Version = appversion.Current

const (
	hookRunEnvironmentExitCode = 10
	hookInputExitCode          = 11
	hookIPCExitCode            = 12
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
		return a.setupCodex()
	case "doctor":
		return a.doctor()
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
	case "_view":
		return a.view()
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
	themeName, _, err := config.LoadTheme()
	if err != nil {
		themeName = theme.Auto
	}
	if err := zellij.Launch(zellijPath, executable, themeName, run, codexArgs, a.In, a.Out, a.Err); err != nil {
		return a.fail(err.Error())
	}
	return 0
}

func (a App) setupCodex() int {
	if err := requireWindowsX64(); err != nil {
		return a.fail(err.Error())
	}
	if _, err := findPowerShell(); err != nil {
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
	previouslyManaged, err := shortcut.Managed(codexPath)
	if err != nil {
		return a.fail(err.Error())
	}
	gate, err := acquireUpdateGate()
	if err != nil {
		return a.fail(err.Error())
	}
	defer gate.Close()
	changed, err := a.ensureCodexSetup(codexPath, executable, false, acquireExclusiveWorkspace)
	if err != nil {
		return a.fail(err.Error())
	}
	if !changed {
		fmt.Fprintln(a.Out, "Managed components are already up to date. Running final checks.")
	}
	if code := a.checkEnvironment(true); code != 0 {
		return code
	}
	writeSetupCompletion(a.Out, !previouslyManaged)
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
	aliasPath, aliasReady, err := shortcut.Installed(codexPath, executable)
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
	completion := setupAnimationMode(beforeLaunch, aliasManaged)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	transaction, err := captureSetupTransaction(codexPath, !pluginReady, !aliasReady)
	if err != nil {
		return false, fmt.Errorf("prepare installation rollback: %w", err)
	}
	defer transaction.discard()
	steps := setupStepCount(zellijReady, pluginReady, aliasReady)
	initial := setupui.Progress{Step: 1, Steps: steps, Stage: "Checking environment", Percent: setupPercent(1, steps, 0)}
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
		report(setupui.Progress{Step: step, Steps: steps, Stage: "Environment ready", Percent: setupPercent(step, steps, 100)})

		if !zellijReady {
			step++
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Downloading Zellij", Percent: setupPercent(step, steps, 0)})
			lastReport := time.Time{}
			lastPercent := -1
			_, err := zellij.InstallManagedWithProgress(ctx, func(downloaded, total int64) {
				stagePercent := 0
				if total > 0 {
					stagePercent = int(min(downloaded, total) * 100 / total)
				}
				percent := setupPercent(step, steps, stagePercent)
				now := time.Now()
				if percent == lastPercent && now.Sub(lastReport) < 200*time.Millisecond && downloaded != total {
					return
				}
				lastPercent = percent
				lastReport = now
				report(setupui.Progress{
					Step: step, Steps: steps, Stage: "Downloading Zellij", Percent: percent,
					Downloaded: downloaded, Total: total,
				})
			})
			if err != nil {
				return err
			}
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Zellij ready", Percent: setupPercent(step, steps, 100)})
		}

		if !pluginReady {
			step++
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Installing Codex plugin", Percent: setupPercent(step, steps, 0)})
			transaction.pluginChanged = true
			if err := codex.InstallPlugin(codexPath); err != nil {
				return err
			}
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Codex plugin ready", Percent: setupPercent(step, steps, 100)})
		}

		if !aliasReady {
			step++
			report(setupui.Progress{Step: step, Steps: steps, Stage: "Installing codex.pp", Percent: setupPercent(step, steps, 0)})
			transaction.aliasChanged = true
			if _, err := shortcut.Install(codexPath, executable); err != nil {
				return err
			}
			report(setupui.Progress{Step: step, Steps: steps, Stage: "codex.pp ready", Percent: setupPercent(step, steps, 100)})
		}

		step++
		report(setupui.Progress{Step: step, Steps: steps, Stage: "Verifying installation", Percent: setupPercent(step, steps, 0)})
		if _, err := zellij.Find(); err != nil {
			return fmt.Errorf("verify Zellij installation: %w", err)
		}
		if !codex.PluginInstalled(codexPath) {
			return fmt.Errorf("verify Codex plugin installation")
		}
		if _, installed, err := shortcut.Installed(codexPath, executable); err != nil || !installed {
			if err != nil {
				return fmt.Errorf("verify codex.pp installation: %w", err)
			}
			return fmt.Errorf("verify codex.pp installation")
		}
		report(setupui.Progress{Step: step, Steps: steps, Stage: "Installation verified", Percent: setupPercent(step, steps, 100)})
		return nil
	})
	if err != nil {
		if rollbackErr := transaction.rollback(); rollbackErr != nil {
			return false, fmt.Errorf("%w; installation rollback failed: %v", err, rollbackErr)
		}
		return false, err
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

func (a App) doctor() int {
	return a.checkEnvironment(false)
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
	if executableErr != nil {
		fmt.Fprintln(a.Out, "[FAIL] Prompt Pane executable: cannot locate the current program")
		ok = false
		prerequisitesReady = false
	} else {
		fmt.Fprintf(a.Out, "[OK]   Prompt Pane executable: %s\n", executable)
	}
	if codexPath, err := exec.LookPath("codex"); err != nil {
		fmt.Fprintln(a.Out, "[FAIL] Codex CLI: not found")
		ok = false
		prerequisitesReady = false
	} else {
		fmt.Fprintf(a.Out, "[OK]   Codex CLI: %s\n", codexPath)
		if codex.PluginInstalled(codexPath) {
			fmt.Fprintln(a.Out, "[OK]   Codex plugin: installed and enabled")
		} else {
			fmt.Fprintln(a.Out, "[FAIL] Codex plugin: not installed or disabled")
			ok = false
		}
		if executableErr != nil {
			fmt.Fprintln(a.Out, "[FAIL] codex.pp: cannot locate the Prompt Pane executable")
			ok = false
		} else if path, installed, err := shortcut.Installed(codexPath, executable); err != nil {
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

func setupPercent(step, steps, stagePercent int) int {
	stagePercent = min(100, max(0, stagePercent))
	return ((step-1)*100 + stagePercent) / steps
}

func setupAnimationMode(beforeLaunch, aliasManaged bool) setupui.CompletionMode {
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
	event, err := codex.DecodeHook(a.In)
	if err != nil {
		if errors.Is(err, codex.ErrMetricsUnavailable) {
			return 0
		}
		return a.failCode(hookInputExitCode, err.Error())
	}
	ctx, cancel := ipc.HookContext()
	defer cancel()
	if err := ipc.SendEvent(ctx, run, event); err != nil {
		return a.failCode(hookIPCExitCode, "Prompt Pane hook could not reach the local viewer")
	}
	return 0
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
