package zellij

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	processutil "github.com/Natsume-kkk/prompt-pane/internal/process"
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
	args, err := closePaneArguments(path, paneID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := processutil.Output(ctx, path, args, processutil.OutputOptions{Limit: 4 << 10}); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return fmt.Errorf("close viewer pane timed out")
		}
		return fmt.Errorf("close viewer pane: %w", err)
	}
	return nil
}

func closePaneArguments(path, paneID string) ([]string, error) {
	if path == "" {
		return nil, fmt.Errorf("Zellij executable path is missing")
	}
	if paneID == "" {
		return nil, fmt.Errorf("Zellij pane ID is missing")
	}
	return []string{"action", "close-pane", "--pane-id", paneID}, nil
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
		"options",
		"--on-force-close", "quit",
		"--mouse-hover-effects", "false",
		"--mouse-click-through", "true",
	}
}

func mergeEnvironment(base, overrides []string) []string {
	overridden := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		name, _, ok := strings.Cut(entry, "=")
		if ok {
			overridden[foldEnvironmentKey(name)] = struct{}{}
		}
	}
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		name, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if _, replaced := overridden[foldEnvironmentKey(name)]; replaced {
			continue
		}
		result = append(result, entry)
	}
	return append(result, overrides...)
}

func foldEnvironmentKey(name string) string {
	return strings.Map(func(value rune) rune {
		canonical := value
		for folded := unicode.SimpleFold(value); folded != value; folded = unicode.SimpleFold(folded) {
			canonical = min(canonical, folded)
		}
		return canonical
	}, name)
}
