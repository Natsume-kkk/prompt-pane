package zellij

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

const (
	EnvExecutable = "PROMPT_PANE_ZELLIJ_PATH"
	EnvPaneID     = "ZELLIJ_PANE_ID"
)

func Launch(path, executable string, run runcontext.Context, codexArgs []string, stdin io.Reader, stdout, stderr io.Writer) error {
	command := exec.Command(path, launchArguments(executable, codexArgs)...)
	overrides := launchOverrides(path, executable, run, os.Getenv("PATH"))
	command.Env = mergeEnvironment(os.Environ(), overrides)
	command.Stdin = stdin
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("run Zellij workspace: %w", err)
	}
	return nil
}

func ClosePane(path, paneID string) error {
	command, err := closePaneCommand(path, paneID)
	if err != nil {
		return err
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("close viewer pane: %w", err)
	}
	return nil
}

func closePaneCommand(path, paneID string) (*exec.Cmd, error) {
	if path == "" {
		return nil, fmt.Errorf("Zellij executable path is missing")
	}
	if paneID == "" {
		return nil, fmt.Errorf("Zellij pane ID is missing")
	}
	return exec.Command(path, "action", "close-pane", "--pane-id", paneID), nil
}

func launchOverrides(path, executable string, run runcontext.Context, inheritedPath string) []string {
	pathValue := filepath.Dir(executable) + string(os.PathListSeparator) + inheritedPath
	overrides := append([]string(nil), run.Environment()...)
	return append(overrides, "PATH="+pathValue, EnvExecutable+"="+path)
}

func launchArguments(executable string, codexArgs []string) []string {
	// Since Zellij 0.41, --session targets an existing session when combined
	// with a layout. Omitting it makes --layout-string create a new session.
	return []string{
		"--layout-string", Layout(executable, codexArgs),
		"options", "--mouse-hover-effects", "false",
	}
}

func mergeEnvironment(base, overrides []string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if !ok || containsEnvironmentKey(overrides, name) {
			continue
		}
		result = append(result, entry)
	}
	return append(result, overrides...)
}

func containsEnvironmentKey(environment []string, name string) bool {
	for _, entry := range environment {
		candidate, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(candidate, name) {
			return true
		}
	}
	return false
}
