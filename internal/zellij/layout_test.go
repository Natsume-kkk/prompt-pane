package zellij

import (
	"strings"
	"testing"
)

func TestLayoutHasRatioFocusAndStructuredArgs(t *testing.T) {
	layout := Layout(`C:\Program Files\Prompt Pane\prompt-pane.exe`, []string{"resume", "name with spaces"})
	for _, expected := range []string{`size="70%"`, `size="30%" name="PROMPTS" command="C:\\Program Files\\Prompt Pane\\prompt-pane.exe"`, `focus=true`, `"resume"`, `"name with spaces"`, `shared_except "locked"`, `bind "Alt p" { ToggleFocusFullscreen; }`} {
		if !strings.Contains(layout, expected) {
			t.Fatalf("layout missing %q:\n%s", expected, layout)
		}
	}
	if strings.Contains(layout, `MoveFocus`) || strings.Contains(layout, `Run `) {
		t.Fatalf("hide shortcut must remain one deterministic Zellij action:\n%s", layout)
	}
	if strings.Contains(layout, "close_on_exit") {
		t.Fatalf("command exit must not close a pane implicitly:\n%s", layout)
	}
}
