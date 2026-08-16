package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

func TestThemePreferenceRoundTrip(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Prompt Pane 配置")
	t.Setenv(paths.EnvHome, home)
	t.Setenv(theme.Environment, "")
	os.Unsetenv(theme.Environment)
	if err := SaveTheme(theme.Mocha); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme(theme.Nord); err != nil {
		t.Fatal(err)
	}
	got, source, err := LoadTheme()
	if err != nil || got != theme.Nord || source != ThemeConfig {
		t.Fatalf("theme = %q, source = %q, err = %v", got, source, err)
	}
	data, err := os.ReadFile(filepath.Join(home, fileName))
	if err != nil || string(data) != "{\n  \"theme\": \"nord\"\n}\n" {
		t.Fatalf("config = %q, err = %v", data, err)
	}
}

func TestEnvironmentOverridesConfig(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	if err := SaveTheme(theme.Mocha); err != nil {
		t.Fatal(err)
	}
	t.Setenv(theme.Environment, theme.Dracula)
	got, source, err := LoadTheme()
	if err != nil || got != theme.Dracula || source != ThemeEnvironment {
		t.Fatalf("theme = %q, source = %q, err = %v", got, source, err)
	}
}

func TestInvalidThemeDoesNotOverwriteConfig(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	if err := SaveTheme(theme.Latte); err != nil {
		t.Fatal(err)
	}
	if err := SaveTheme("unknown"); err == nil {
		t.Fatal("invalid theme was saved")
	}
	got, _, err := LoadTheme()
	if err != nil || got != theme.Latte {
		t.Fatalf("theme = %q, err = %v", got, err)
	}
}

func TestCorruptPreferencesFallBackSafely(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.EnvHome, home)
	if err := os.WriteFile(filepath.Join(home, fileName), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, source, err := LoadTheme()
	if err == nil || got != theme.Auto || source != ThemeDefault {
		t.Fatalf("theme = %q, source = %q, err = %v", got, source, err)
	}
}
