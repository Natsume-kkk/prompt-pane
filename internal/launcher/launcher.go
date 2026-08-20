package launcher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	runtimeinstall "github.com/Natsume-kkk/prompt-pane/internal/install"
	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/shortcut"
)

const launchPreparationAttempts = 4

type App struct {
	In         io.Reader
	Out        io.Writer
	Err        io.Writer
	runProcess func(path string, args []string, preparedGeneration string) (int, error)
}

func New() App {
	return App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}

func IsManagedInvocation(invocation string) (bool, error) {
	if !runtimeinstall.IsLauncherPath(invocation) && !shortcut.IsCodexAlias(invocation) {
		return false, nil
	}
	_, present, err := runtimeinstall.LoadIfPresent()
	if err != nil {
		return true, err
	}
	return present, nil
}

func (a App) Execute(args []string) int {
	if len(args) > 0 && args[0] == "codex" {
		return a.launchCodex(args)
	}
	state, err := runtimeinstall.Load()
	if err != nil {
		return a.fail(err)
	}
	target := state.Current
	if len(args) > 0 && args[0] == "setup" && state.Pending != nil {
		target = state.Pending
	}
	path, err := verifiedRuntimePath(*target)
	if err != nil {
		return a.fail(err)
	}
	code, err := a.run(path, args, "")
	if err != nil {
		return a.fail(err)
	}
	return code
}

func (a App) launchCodex(args []string) int {
	for attempt := 0; attempt < launchPreparationAttempts; attempt++ {
		if err := a.tryActivatePending(); err != nil {
			fmt.Fprintf(a.Err, "prompt-pane: pending update activation failed; continuing with the current version: %v\n", err)
		}
		state, err := runtimeinstall.Load()
		if err != nil {
			return a.fail(err)
		}
		current := *state.Current
		path, err := verifiedRuntimePath(current)
		if err != nil {
			return a.fail(err)
		}
		code, err := a.run(path, []string{"_prepare", "codex"}, "")
		if err != nil {
			return a.fail(err)
		}
		if code != 0 {
			return code
		}

		gate, err := acquireUpdateGate()
		if err != nil {
			return a.fail(err)
		}
		latest, err := runtimeinstall.Load()
		if err != nil {
			_ = gate.Close()
			return a.fail(err)
		}
		if latest.Current.Generation != current.Generation || pendingGeneration(latest.Pending) != pendingGeneration(state.Pending) {
			_ = gate.Close()
			continue
		}
		activity, err := runcontext.AcquireWorkspaceActivity()
		if err != nil {
			_ = gate.Close()
			return a.fail(fmt.Errorf("cannot register the Prompt Pane workspace as active"))
		}
		if err := gate.Close(); err != nil {
			_ = activity.Close()
			return a.fail(fmt.Errorf("cannot release the Prompt Pane update gate"))
		}
		code, runErr := a.run(path, args, current.Generation)
		activityErr := activity.Close()
		if runErr != nil {
			return a.fail(runErr)
		}
		if activityErr != nil {
			return a.fail(fmt.Errorf("cannot release the Prompt Pane workspace activity"))
		}
		return code
	}
	return a.fail(fmt.Errorf("Prompt Pane changed versions repeatedly while preparing the workspace; retry the launch"))
}

func pendingGeneration(release *runtimeinstall.Release) string {
	if release == nil {
		return ""
	}
	return release.Generation
}

func (a App) tryActivatePending() error {
	state, err := runtimeinstall.Load()
	if err != nil {
		return err
	}
	if state.Pending == nil {
		return nil
	}
	path, err := verifiedRuntimePath(*state.Pending)
	if err != nil {
		return err
	}
	code, err := a.run(path, []string{"_activate", "codex"}, "")
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("activation process exited with code %d", code)
	}
	return nil
}

func (a App) run(path string, args []string, preparedGeneration string) (int, error) {
	if a.runProcess != nil {
		return a.runProcess(path, args, preparedGeneration)
	}
	command := exec.Command(path, args...)
	command.Stdin = a.In
	command.Stdout = a.Out
	command.Stderr = a.Err
	command.Env = launchEnvironment(preparedGeneration)
	err := command.Run()
	if err == nil {
		return 0, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode(), nil
	}
	return 1, fmt.Errorf("start managed Prompt Pane version: %w", err)
}

func verifiedRuntimePath(release runtimeinstall.Release) (string, error) {
	if err := runtimeinstall.VerifyRelease(release); err != nil {
		return "", err
	}
	return runtimeinstall.RuntimePath(release)
}

func launchEnvironment(preparedGeneration string) []string {
	prefix := strings.ToUpper(runtimeinstall.EnvPreparedGeneration) + "="
	environment := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			continue
		}
		environment = append(environment, entry)
	}
	if preparedGeneration != "" {
		environment = append(environment, runtimeinstall.EnvPreparedGeneration+"="+preparedGeneration)
	}
	return environment
}

func acquireUpdateGate() (runcontext.CoordinationLock, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return runcontext.AcquireUpdateGate(ctx)
}

func (a App) fail(err error) int {
	fmt.Fprintln(a.Err, "prompt-pane:", err)
	return 1
}
