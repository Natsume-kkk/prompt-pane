package zellij

import (
	"strings"
	"testing"
)

func TestLayoutHasRatioFocusAndStructuredArgs(t *testing.T) {
	layout := Layout(`C:\Program Files\Prompt Pane\prompt-pane.exe`, []string{"resume", "name with spaces"})
	for _, expected := range []string{`size="70%"`, `size="30%" name=" " command="C:\\Program Files\\Prompt Pane\\prompt-pane.exe"`, `focus=true`, `"resume"`, `"name with spaces"`} {
		if !strings.Contains(layout, expected) {
			t.Fatalf("layout missing %q:\n%s", expected, layout)
		}
	}
	if strings.Contains(layout, "close_on_exit") {
		t.Fatalf("command exit must not close a pane implicitly:\n%s", layout)
	}
}
