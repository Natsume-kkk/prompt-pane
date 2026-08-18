package zellij

import (
	"slices"
	"strings"
	"testing"

	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
)

func TestLaunchArgumentsCreateNewSessionFromLayoutString(t *testing.T) {
	arguments := launchArguments(`C:\Program Files\Prompt Pane\prompt-pane.exe`, []string{"resume"})
	if len(arguments) != 9 || arguments[0] != "--layout-string" {
		t.Fatalf("unexpected launch arguments: %q", arguments)
	}
	if slices.Contains(arguments, "--session") {
		t.Fatalf("--session targets an existing Zellij session: %q", arguments)
	}
	if !strings.Contains(arguments[1], `"resume"`) {
		t.Fatalf("layout did not preserve Codex arguments: %q", arguments[1])
	}
	if want := []string{"options", "--on-force-close", "quit", "--mouse-hover-effects", "false", "--mouse-click-through", "true"}; !slices.Equal(arguments[2:], want) {
		t.Fatalf("launch did not apply session-scoped workspace options: got %q, want %q", arguments[2:], want)
	}
	if slices.Contains(arguments, "--focus-follows-mouse") {
		t.Fatalf("launch changed focus merely by hovering: %q", arguments)
	}
}

func TestLaunchOverridesExposeTheExactZellijExecutable(t *testing.T) {
	run := runcontext.Context{ID: strings.Repeat("1", 32), Token: strings.Repeat("2", 64), Endpoint: `\\.\pipe\prompt-pane-test`}
	overrides := launchOverrides(
		`C:\Prompt Pane\zellij.exe`,
		`D:\Apps\prompt-pane.exe`,
		run,
		`C:\Windows\System32`,
	)
	joined := strings.Join(overrides, "\n")
	for _, want := range []string{
		EnvExecutable + `=C:\Prompt Pane\zellij.exe`,
		`PATH=D:\Apps;C:\Windows\System32`,
		runcontext.EnvRunID + "=" + run.ID,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("launch environment is missing %q: %q", want, overrides)
		}
	}
}

func TestClosePaneTargetsTheCurrentPane(t *testing.T) {
	arguments, err := closePaneArguments(`C:\Prompt Pane\zellij.exe`, "terminal_7")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"action", "close-pane", "--pane-id", "terminal_7"}
	if !slices.Equal(arguments, want) {
		t.Fatalf("close pane argv = %q, want %q", arguments, want)
	}
	for _, test := range []struct {
		path   string
		paneID string
	}{{paneID: "terminal_7"}, {path: `C:\Prompt Pane\zellij.exe`}} {
		if _, err := closePaneArguments(test.path, test.paneID); err == nil {
			t.Fatalf("accepted incomplete close target: %#v", test)
		}
	}
}

func TestMergeEnvironmentReplacesWindowsKeysCaseInsensitively(t *testing.T) {
	merged := mergeEnvironment(
		[]string{"Path=old", "PROMPT_PANE_TOKEN=old", EnvExecutable + "=old", "KEEP=value"},
		[]string{"PATH=new", "PROMPT_PANE_TOKEN=new", EnvExecutable + "=new"},
	)
	joined := strings.Join(merged, "\n")
	if strings.Contains(joined, "Path=old") || strings.Contains(joined, "PROMPT_PANE_TOKEN=old") || strings.Contains(joined, EnvExecutable+"=old") {
		t.Fatalf("old values survived: %q", joined)
	}
	for _, expected := range []string{"PATH=new", "PROMPT_PANE_TOKEN=new", EnvExecutable + "=new", "KEEP=value"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing %q in %q", expected, joined)
		}
	}
}
