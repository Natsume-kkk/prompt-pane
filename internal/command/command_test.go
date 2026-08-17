package command

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	runcontext "github.com/Natsume-kkk/prompt-pane/internal/run"
	"github.com/Natsume-kkk/prompt-pane/internal/setupui"
	"github.com/Natsume-kkk/prompt-pane/internal/ui"
	"github.com/Natsume-kkk/prompt-pane/plugins"
)

type failOnRead struct{ t *testing.T }

func TestViewerCloseRequiresAnExplicitUIRequest(t *testing.T) {
	model := ui.New(nil)
	if viewerCloseRequested(model) {
		t.Fatal("new viewer requested pane closure")
	}
	updated, _ := model.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if !viewerCloseRequested(updated) {
		t.Fatal("Ctrl+X did not reach command orchestration as a close request")
	}
}

func (r failOnRead) Read([]byte) (int, error) {
	r.t.Fatal("setup unexpectedly read confirmation input")
	return 0, io.EOF
}

func TestVersion(t *testing.T) {
	var output bytes.Buffer
	app := App{In: strings.NewReader(""), Out: &output, Err: &output}
	if code := app.Execute([]string{"version"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if !strings.Contains(output.String(), Version) {
		t.Fatalf("version output = %q", output.String())
	}
}

func TestHelpShowsCodexAliasAndCompatibilityCommand(t *testing.T) {
	var output bytes.Buffer
	app := App{In: strings.NewReader(""), Out: &output, Err: &output}
	if code := app.Execute([]string{"help"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, command := range []string{"codex.pp [<codex-args>]", "prompt-pane codex [-- <codex-args>]"} {
		if !strings.Contains(output.String(), command) {
			t.Fatalf("help is missing %q: %q", command, output.String())
		}
	}
}

func TestUnknownCommandUsesUsageExitCode(t *testing.T) {
	var output bytes.Buffer
	app := App{In: strings.NewReader(""), Out: &output, Err: &output}
	if code := app.Execute([]string{"unknown"}); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestConfirmDefaultsToNo(t *testing.T) {
	app := App{In: strings.NewReader("\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if app.confirm("continue? ") {
		t.Fatal("empty confirmation was accepted")
	}
}

func TestConfirmReusesBufferedInput(t *testing.T) {
	app := App{In: strings.NewReader("y\ny\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}.withBufferedInput()
	if !app.confirm("first? ") || !app.confirm("second? ") {
		t.Fatal("consecutive confirmations did not consume one answer each")
	}
}

func TestInputBufferingIsLimitedToTeardown(t *testing.T) {
	terminal, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer terminal.Close()

	for _, command := range []string{"codex", "setup", "doctor", "_agent", "_hook", "_view"} {
		app := (App{In: terminal}).withInputFor(command)
		if app.In != terminal {
			t.Fatalf("%s replaced the original stdin handle", command)
		}
	}
	app := (App{In: terminal}).withInputFor("teardown")
	if _, ok := app.In.(*bufio.Reader); !ok {
		t.Fatal("teardown did not buffer confirmation input")
	}
}

func TestSetupStepCountIncludesOnlyRequiredWork(t *testing.T) {
	tests := []struct {
		zellijReady bool
		pluginReady bool
		aliasReady  bool
		want        int
	}{
		{true, true, true, 2},
		{true, false, true, 3},
		{false, true, false, 4},
		{false, false, false, 5},
	}
	for _, test := range tests {
		if got := setupStepCount(test.zellijReady, test.pluginReady, test.aliasReady); got != test.want {
			t.Fatalf("setupStepCount(%v, %v, %v) = %d, want %d", test.zellijReady, test.pluginReady, test.aliasReady, got, test.want)
		}
	}
}

func TestSetupPercentIsMonotonicAcrossDynamicStages(t *testing.T) {
	previous := -1
	for step := 1; step <= 4; step++ {
		for _, within := range []int{0, 25, 50, 100} {
			current := setupPercent(step, 4, within)
			if current < previous {
				t.Fatalf("progress moved backwards: %d after %d", current, previous)
			}
			previous = current
		}
	}
	if previous != 100 || setupPercent(1, 2, -10) != 0 || setupPercent(2, 2, 150) != 100 {
		t.Fatalf("progress boundaries are incorrect: final=%d", previous)
	}
}

func TestSetupAnimationModeMatchesOperation(t *testing.T) {
	for _, test := range []struct {
		beforeLaunch bool
		aliasManaged bool
		want         setupui.CompletionMode
	}{
		{want: setupui.SetupCompletion},
		{aliasManaged: true, want: setupui.RefreshCompletion},
		{beforeLaunch: true, want: setupui.RepairCompletion},
		{beforeLaunch: true, aliasManaged: true, want: setupui.RepairCompletion},
	} {
		if got := setupAnimationMode(test.beforeLaunch, test.aliasManaged); got != test.want {
			t.Fatalf("beforeLaunch=%v aliasManaged=%v mode=%d, want %d", test.beforeLaunch, test.aliasManaged, got, test.want)
		}
	}
}

func TestDoctorFailureMessageProvidesOneActionableNextStep(t *testing.T) {
	if got := doctorFailureMessage(false, false); !strings.Contains(got, "Resolve the [FAIL] prerequisites") || strings.Contains(got, "setup codex") {
		t.Fatalf("prerequisite failure message = %q", got)
	}
	if got := doctorFailureMessage(true, false); !strings.Contains(got, "prompt-pane setup codex") || !strings.Contains(got, "prompt-pane doctor") {
		t.Fatalf("managed failure message = %q", got)
	}
	if got := doctorFailureMessage(true, true); !strings.Contains(got, "Setup could not verify") || strings.Contains(got, "setup codex") {
		t.Fatalf("setup verification failure message = %q", got)
	}
}

func TestLaunchRepairNeedsNoConfirmationAndSetupRunsFinalChecks(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows x64 is the v1.1.0 target")
	}
	if _, err := findPowerShell(); err != nil {
		t.Skip(err)
	}
	root := t.TempDir()
	promptPaneHome := filepath.Join(root, "Prompt Pane data")
	codexHome := filepath.Join(root, "Codex data")
	t.Setenv("PROMPT_PANE_HOME", promptPaneHome)
	t.Setenv("CODEX_HOME", codexHome)

	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	if err := os.WriteFile(codexPath, []byte("@exit /b 1\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "zellij.cmd"), []byte("@echo zellij 0.44.3\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	manifestData, err := plugins.Content.ReadFile("prompt-pane/.codex-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(codexHome, "plugins", "cache", "prompt-pane", "prompt-pane", manifest.Version)
	if err := os.MkdirAll(filepath.Join(cache, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cache, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, ".codex-plugin", "plugin.json"), manifestData, 0o600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "bin", "prompt-pane.exe"), binary, 0o700); err != nil {
		t.Fatal(err)
	}
	config := "[plugins.\"prompt-pane@prompt-pane\"]\nenabled = true\n"
	if err := os.WriteFile(filepath.Join(codexHome, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	app := App{In: failOnRead{t}, Out: &output, Err: &output}
	changed, err := app.ensureCodexSetup(codexPath, executable, true)
	if err != nil || !changed {
		t.Fatalf("changed = %v, err = %v", changed, err)
	}
	for _, unexpected := range []string{"[y/N]", "Continue with setup?", "Repair these components"} {
		if strings.Contains(output.String(), unexpected) {
			t.Fatalf("repair still requested confirmation %q: %q", unexpected, output.String())
		}
	}
	for _, want := range []string{"missing or outdated components", "Repair complete", "Starting Codex…"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("repair output is missing %q: %q", want, output.String())
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "codex.pp.exe")); err != nil {
		t.Fatalf("automatic repair did not install codex.pp: %v", err)
	}

	output.Reset()
	if code := app.setupCodex(); code != 0 {
		t.Fatalf("setup exit code = %d, output = %q", code, output.String())
	}
	for _, want := range []string{"Running final checks", "[OK]   Platform", "Environment is ready", "Setup complete.", "Run `codex.pp` when ready."} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("setup output is missing %q: %q", want, output.String())
		}
	}
}

func TestSetupCompletionExplainsFirstPromptTrustOnlyOnFirstInstall(t *testing.T) {
	var firstInstall bytes.Buffer
	writeSetupCompletion(&firstInstall, true)
	for _, want := range []string{"Setup complete.", "1. Run `codex.pp`.", "2. Submit your first prompt.", "open `/hooks` in Codex", "restart `codex.pp`"} {
		if !strings.Contains(firstInstall.String(), want) {
			t.Fatalf("first-install completion is missing %q: %q", want, firstInstall.String())
		}
	}

	var refresh bytes.Buffer
	writeSetupCompletion(&refresh, false)
	if output := refresh.String(); !strings.Contains(output, "Run `codex.pp` when ready.") || strings.Contains(output, "/hooks") || strings.Contains(output, "first prompt") {
		t.Fatalf("refresh completion repeated first-install guidance: %q", output)
	}
}

func TestHookOutsidePromptPaneRunIsSilent(t *testing.T) {
	for _, name := range []string{runcontext.EnvRunID, runcontext.EnvToken, runcontext.EnvEndpoint} {
		t.Setenv(name, "")
	}
	var output bytes.Buffer
	app := App{In: strings.NewReader(""), Out: &output, Err: &output}
	if code := app.Execute([]string{"_hook", "codex"}); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	if output.Len() != 0 {
		t.Fatalf("hook output = %q", output.String())
	}
}

func TestHookRejectsIncompletePromptPaneRun(t *testing.T) {
	t.Setenv(runcontext.EnvRunID, "active")
	t.Setenv(runcontext.EnvToken, "")
	t.Setenv(runcontext.EnvEndpoint, "")
	var output bytes.Buffer
	app := App{In: strings.NewReader(""), Out: &output, Err: &output}
	if code := app.Execute([]string{"_hook", "codex"}); code != hookRunEnvironmentExitCode {
		t.Fatalf("exit code = %d, want %d", code, hookRunEnvironmentExitCode)
	}
	if output.String() != "prompt-pane: Prompt Pane hook is not attached to an active run\n" {
		t.Fatalf("hook output = %q", output.String())
	}
}

func TestHookFailureCodesIdentifySafeDiagnosticStage(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		runcontext.EnvRunID:    run.ID,
		runcontext.EnvToken:    run.Token,
		runcontext.EnvEndpoint: run.Endpoint,
	} {
		t.Setenv(name, value)
	}

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "invalid input", input: `{`, want: hookInputExitCode},
		{name: "unavailable IPC", input: `{"session_id":"thr_synthetic","hook_event_name":"SessionStart","source":"startup"}`, want: hookIPCExitCode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			app := App{In: strings.NewReader(test.input), Out: &output, Err: &output}
			if code := app.Execute([]string{"_hook", "codex"}); code != test.want {
				t.Fatalf("exit code = %d, want %d; output = %q", code, test.want, output.String())
			}
			if strings.Contains(output.String(), "thr_synthetic") {
				t.Fatalf("diagnostic output leaked input: %q", output.String())
			}
		})
	}
}

func TestStopHookSilentlySkipsUnavailableMetrics(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		runcontext.EnvRunID: run.ID, runcontext.EnvToken: run.Token, runcontext.EnvEndpoint: run.Endpoint,
	} {
		t.Setenv(name, value)
	}
	var output bytes.Buffer
	app := App{In: strings.NewReader(`{"session_id":"thr_synthetic","hook_event_name":"Stop","transcript_path":"Z:\\missing\\session.jsonl"}`), Out: &output, Err: &output}
	if code := app.Execute([]string{"_hook", "codex"}); code != 0 || output.Len() != 0 {
		t.Fatalf("Stop hook exit = %d, output = %q", code, output.String())
	}
}

func TestStopHookSilentlySkipsMissingTranscript(t *testing.T) {
	run, err := runcontext.New()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		runcontext.EnvRunID: run.ID, runcontext.EnvToken: run.Token, runcontext.EnvEndpoint: run.Endpoint,
	} {
		t.Setenv(name, value)
	}
	var output bytes.Buffer
	app := App{In: strings.NewReader(`{"session_id":"thr_synthetic","hook_event_name":"Stop","transcript_path":null}`), Out: &output, Err: &output}
	if code := app.Execute([]string{"_hook", "codex"}); code != 0 || output.Len() != 0 {
		t.Fatalf("Stop hook exit = %d, output = %q", code, output.String())
	}
}
