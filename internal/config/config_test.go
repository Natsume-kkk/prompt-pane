package config

import (
	"fmt"
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

func TestInterfaceLanguageRoundTripPreservesTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.EnvHome, home)
	if err := SaveTheme(theme.Nord); err != nil {
		t.Fatal(err)
	}
	if err := SaveInterfaceLanguage(InterfaceLanguageEnglish); err != nil {
		t.Fatal(err)
	}
	language, err := LoadInterfaceLanguage()
	if err != nil || language != InterfaceLanguageEnglish {
		t.Fatalf("interface language = %q, err = %v", language, err)
	}
	name, source, err := LoadTheme()
	if err != nil || name != theme.Nord || source != ThemeConfig {
		t.Fatalf("theme after language save = %q, %q, %v", name, source, err)
	}
	data, err := os.ReadFile(filepath.Join(home, fileName))
	if err != nil || string(data) != "{\n  \"theme\": \"nord\",\n  \"interface_language\": \"en\"\n}\n" {
		t.Fatalf("combined preferences = %q, err = %v", data, err)
	}
}

func TestInvalidInterfaceLanguageDoesNotOverwriteConfig(t *testing.T) {
	t.Setenv(paths.EnvHome, t.TempDir())
	if err := SaveInterfaceLanguage(InterfaceLanguageChinese); err != nil {
		t.Fatal(err)
	}
	if err := SaveInterfaceLanguage("unknown"); err == nil {
		t.Fatal("invalid interface language was saved")
	}
	if language, err := LoadInterfaceLanguage(); err != nil || language != InterfaceLanguageChinese {
		t.Fatalf("interface language = %q, err = %v", language, err)
	}
}

func TestDefaultInterfaceLanguageUsesPreferredUILanguage(t *testing.T) {
	original := loadPreferredUILanguages
	t.Cleanup(func() { loadPreferredUILanguages = original })

	for _, test := range []struct {
		name      string
		languages []string
		err       error
		want      string
	}{
		{name: "simplified Chinese", languages: []string{"zh-CN"}, want: InterfaceLanguageChinese},
		{name: "traditional Chinese", languages: []string{"zh-Hant-TW"}, want: InterfaceLanguageChinese},
		{name: "English", languages: []string{"en-US", "zh-CN"}, want: InterfaceLanguageEnglish},
		{name: "unsupported language", languages: []string{"fr-FR"}, want: InterfaceLanguageEnglish},
		{name: "detection failure", err: fmt.Errorf("unavailable"), want: InterfaceLanguageChinese},
		{name: "empty result", want: InterfaceLanguageChinese},
	} {
		t.Run(test.name, func(t *testing.T) {
			loadPreferredUILanguages = func() ([]string, error) {
				return test.languages, test.err
			}
			if got := defaultInterfaceLanguage(); got != test.want {
				t.Fatalf("default interface language = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSavedInterfaceLanguageOverridesPreferredUILanguage(t *testing.T) {
	original := loadPreferredUILanguages
	t.Cleanup(func() { loadPreferredUILanguages = original })
	loadPreferredUILanguages = func() ([]string, error) {
		return []string{"en-US"}, nil
	}
	t.Setenv(paths.EnvHome, t.TempDir())

	if language, err := LoadInterfaceLanguage(); err != nil || language != InterfaceLanguageEnglish {
		t.Fatalf("detected interface language = %q, err = %v", language, err)
	}
	if err := SaveInterfaceLanguage(InterfaceLanguageChinese); err != nil {
		t.Fatal(err)
	}
	detected := false
	loadPreferredUILanguages = func() ([]string, error) {
		detected = true
		return nil, fmt.Errorf("saved preferences should bypass detection")
	}
	if language, err := LoadInterfaceLanguage(); err != nil || language != InterfaceLanguageChinese {
		t.Fatalf("saved interface language = %q, err = %v", language, err)
	}
	if detected {
		t.Fatal("saved interface language still queried the system language")
	}
}

func TestLegacyActivityLanguageMigratesOnNextPreferenceSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.EnvHome, home)
	path := filepath.Join(home, fileName)
	legacy := []byte("{\n  \"theme\": \"nord\",\n  \"activity_language\": \"en\"\n}\n")
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}

	language, err := LoadInterfaceLanguage()
	if err != nil || language != InterfaceLanguageEnglish {
		t.Fatalf("legacy interface language = %q, err = %v", language, err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(legacy) {
		t.Fatalf("read-only load rewrote legacy preferences: %q, err = %v", data, err)
	}
	if err := SaveTheme(theme.Mocha); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{\n  \"theme\": \"mocha\",\n  \"interface_language\": \"en\"\n}\n" {
		t.Fatalf("migrated preferences = %q, err = %v", data, err)
	}
}

func TestInterfaceLanguageTakesPriorityOverLegacyKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv(paths.EnvHome, home)
	path := filepath.Join(home, fileName)
	preferences := []byte("{\n  \"interface_language\": \"zh\",\n  \"activity_language\": \"en\"\n}\n")
	if err := os.WriteFile(path, preferences, 0o600); err != nil {
		t.Fatal(err)
	}

	language, err := LoadInterfaceLanguage()
	if err != nil || language != InterfaceLanguageChinese {
		t.Fatalf("preferred interface language = %q, err = %v", language, err)
	}
	if err := SaveTheme(theme.Mocha); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "{\n  \"theme\": \"mocha\",\n  \"interface_language\": \"zh\"\n}\n" {
		t.Fatalf("normalized preferences = %q, err = %v", data, err)
	}
}
