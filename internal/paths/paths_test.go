package paths

import (
	"path/filepath"
	"testing"
)

func TestHomeUsesAndValidatesExplicitOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "Prompt Pane", "..", "PromptPane")
	t.Setenv(EnvHome, override)
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Clean(override); got != want {
		t.Fatalf("Home() = %q, want %q", got, want)
	}

	t.Setenv(EnvHome, "relative")
	if _, err := Home(); err == nil {
		t.Fatal("Home() accepted a relative override")
	}
}
