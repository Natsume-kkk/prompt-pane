package setupui

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestCompletionImmediatelyShowsStableStepList(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{Step: 5, Steps: 5, Stage: "Installation verified"})
	updated, _ := model.Update(workEvent{done: true})
	model = updated.(Model)
	view := model.View().Content
	if !model.finalFrame || strings.Count(view, "✓") != 5 || !strings.Contains(view, "Installation ready") || !strings.Contains(view, "Running final checks") {
		t.Fatalf("completed stepper = %q", view)
	}
}

func TestCompletedModelRendersStepperAndCommand(t *testing.T) {
	for _, test := range []struct {
		mode  CompletionMode
		title string
		next  string
	}{
		{SetupCompletion, "Installation ready", "Running final checks"},
		{RepairCompletion, "Repair complete", "Starting Codex…"},
	} {
		for _, width := range []int{20, 40, 80, 120} {
			model := NewModel(nil, test.mode, Progress{Step: 1, Steps: 5, Stage: "Checking environment"})
			model.width = width
			model.progress = Progress{Step: 5, Steps: 5, Stage: "Verifying installation"}
			model.finalFrame = true
			view := model.View().Content
			if !strings.Contains(view, test.title) || !strings.Contains(view, test.next) {
				t.Fatalf("width %d completed view = %q", width, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if (strings.Contains(line, test.title) || strings.Contains(line, test.next) || strings.Contains(line, "[5/5]")) && strings.HasPrefix(line, " ") {
					t.Fatalf("width %d completion content remained centered: %q", width, line)
				}
			}
			if test.mode != SetupCompletion {
				if gotLines := strings.Count(view, "\n") + 1; gotLines > 2 {
					t.Fatalf("width %d compact completion lines = %d, want at most 2: %q", width, gotLines, view)
				}
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width %d line exceeds width: %q", width, line)
				}
			}
		}
	}
}

func TestNonInteractiveProgressUsesPlainLines(t *testing.T) {
	var output bytes.Buffer
	err := Run(&output, SetupCompletion, Progress{Step: 1, Steps: 5, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 1, Steps: 5, Stage: "Checking environment"})
		report(Progress{Step: 2, Steps: 5, Stage: "Downloading Zellij", Downloaded: 1024, Total: 2048})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if strings.Count(got, "[1/5] Checking environment\n") != 1 || strings.Contains(got, "Checking environment  0%") {
		t.Fatalf("initial progress was not emitted exactly once: %q", got)
	}
	if !strings.Contains(got, "[2/5] Downloading Zellij  50%  1.0 KB / 2.0 KB") {
		t.Fatalf("plain progress = %q", got)
	}
}

func TestRefreshProgressUsesPlainLines(t *testing.T) {
	var output bytes.Buffer
	err := Run(&output, RefreshCompletion, Progress{Step: 1, Steps: 2, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 2, Steps: 2, Stage: "Installation verified"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"[1/2] Checking environment", "[2/2] Installation verified", "Refresh ready\nRunning final checks\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("refresh progress missing %q: %q", want, got)
		}
	}
}

func TestNonInteractiveFailureDoesNotClaimCompletion(t *testing.T) {
	var output bytes.Buffer
	wantErr := "synthetic failure"
	err := Run(&output, SetupCompletion, Progress{Step: 1, Steps: 5, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 3, Steps: 5, Stage: "Installing Codex plugin"})
		return fmt.Errorf("%s", wantErr)
	})
	if err == nil || err.Error() != wantErr {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(output.String(), "Installation ready") {
		t.Fatalf("failure claimed completion: %q", output.String())
	}
}

func TestRepairCompletionStartsCodexWithoutRunInstruction(t *testing.T) {
	var output bytes.Buffer
	err := Run(&output, RepairCompletion, Progress{Step: 1, Steps: 2, Stage: "Checking environment"}, func(report Reporter) error {
		report(Progress{Step: 2, Steps: 2, Stage: "Installation verified"})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "Repair complete") || !strings.Contains(got, "Starting Codex…") || strings.Contains(got, "Run: codex.pp") {
		t.Fatalf("repair completion = %q", got)
	}
}

func TestStatusTruncationIsLeftAlignedAndUTF8Safe(t *testing.T) {
	line := fitLine("[2/3] 检查 Unicode environment", 17)
	if !utf8.ValidString(line) || lipgloss.Width(line) > 17 {
		t.Fatalf("status line is invalid or too wide: %q", line)
	}
	if got := fitLine("ready", 11); got != "ready" {
		t.Fatalf("left-aligned line = %q", got)
	}
}

func TestInitialProgressUsesTheActualStageCount(t *testing.T) {
	initial := Progress{Step: 1, Steps: 5, Stage: "Checking environment"}
	model := NewModel(nil, SetupCompletion, initial)
	status := ansi.Strip(model.currentStageLine())
	if model.progress.Step != initial.Step || model.progress.Steps != initial.Steps || model.progress.Stage != initial.Stage || strings.Contains(status, "Preparing") {
		t.Fatalf("initial progress = %#v, status = %q", model.progress, status)
	}
}

func TestFailureStatusStaysExplicit(t *testing.T) {
	for _, noColor := range []bool{false, true} {
		for _, width := range []int{8, 20, 40, 80} {
			model := NewModel(nil, SetupCompletion, Progress{Step: 3, Steps: 5, Stage: "Installing Codex plugin"})
			model.width = width
			model.noColor = noColor
			model.failed = true
			model.done = true
			view := model.View().Content
			if !strings.Contains(view, "[FAIL]") || strings.Contains(view, "Installation ready") {
				t.Fatalf("color=%v width=%d failure view = %q", !noColor, width, view)
			}
			for _, line := range strings.Split(view, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("color=%v width=%d line exceeds width: %q", !noColor, width, line)
				}
			}
			if !noColor && width >= 20 && !strings.Contains(view, model.stepperGraphicStyle(model.colors.Error).Render("×")) {
				t.Fatalf("width=%d failure marker did not use the error role: %q", width, view)
			}
		}
	}
}

func TestStepperUsesActualPlanAndOnlyRealDownloadPercentage(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{
		Step: 2, Steps: 4, Stage: "Downloading Zellij", Downloaded: 1024, Total: 2048,
		Plan: []string{"Environment", "Zellij 0.44.3", "Codex plugin", "Installation verification"},
	})
	model.width = 120
	model.noColor = true
	view := ansi.Strip(model.View().Content)
	for _, want := range []string{"Prompt Pane setup", "✓ Environment", "[2/4] Downloading Zellij", "50%", "1.0 KB / 2.0 KB", "○ Codex plugin", "○ Installation verification"} {
		if !strings.Contains(view, want) {
			t.Fatalf("stepper missed %q: %q", want, view)
		}
	}
}

func TestStepperFailureKeepsPendingStagesAndFullError(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{
		Step: 2, Steps: 3, Stage: "Installing Codex plugin",
		Plan: []string{"Environment", "Codex plugin", "Installation verification"},
	})
	model.width = 120
	model.noColor = true
	updated, _ := model.Update(workEvent{done: true, err: fmt.Errorf("plugin directory is not writable; check permissions")})
	view := ansi.Strip(updated.(Model).View().Content)
	for _, want := range []string{"✓ Environment", "× [FAIL] [2/3] Installing Codex plugin", "○ Installation verification", "Error: plugin directory is not writable; check permissions"} {
		if !strings.Contains(view, want) {
			t.Fatalf("failure stepper missed %q: %q", want, view)
		}
	}
	if strings.Contains(view, "Installation ready") {
		t.Fatalf("failure stepper claimed completion: %q", view)
	}
}

func TestStepperUsesThemeRolesWithoutColoringWholeStage(t *testing.T) {
	model := NewModel(nil, SetupCompletion, Progress{
		Step: 1, Steps: 2, Stage: "Downloading Zellij", Downloaded: 1, Total: 2,
		Plan: []string{"Zellij 0.44.3", "Installation verification"},
	})
	model.width = 120
	model.noColor = false
	line := model.currentStageLine()
	if !strings.Contains(line, model.stepperGraphicStyle(model.colors.FocusMarker).Render(spinnerFrames[0])) ||
		!strings.Contains(line, model.stepperBodyStyle(false).Render("Downloading Zellij")) ||
		!strings.Contains(line, model.stepperGraphicStyle(model.colors.ProgressFill).Render("████████")) {
		t.Fatalf("stepper did not use focus/body/progress roles independently: %q", line)
	}
}
