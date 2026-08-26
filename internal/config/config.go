package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	"github.com/Natsume-kkk/prompt-pane/internal/theme"
)

const fileName = "config.json"

const (
	InterfaceLanguageChinese = "zh"
	InterfaceLanguageEnglish = "en"
)

var loadPreferredUILanguages = platformPreferredUILanguages

type ThemeSource string

const (
	ThemeDefault     ThemeSource = "default"
	ThemeConfig      ThemeSource = "config"
	ThemeEnvironment ThemeSource = "environment"
)

type Preferences struct {
	Theme             string `json:"theme,omitempty"`
	InterfaceLanguage string `json:"interface_language,omitempty"`
	// LegacyActivityLanguage stays read-only so existing installations retain
	// their choice until the next preference save migrates it.
	LegacyActivityLanguage string `json:"activity_language,omitempty"`
}

func LoadTheme() (string, ThemeSource, error) {
	if value, ok := os.LookupEnv(theme.Environment); ok {
		value = strings.ToLower(strings.TrimSpace(value))
		if !theme.Valid(value) {
			return theme.Auto, ThemeEnvironment, fmt.Errorf("%s has an unsupported theme", theme.Environment)
		}
		return value, ThemeEnvironment, nil
	}
	preferences, exists, err := loadPreferences()
	if err != nil {
		return theme.Auto, ThemeDefault, err
	}
	if !exists {
		return theme.Auto, ThemeDefault, nil
	}
	preferences.Theme = strings.ToLower(strings.TrimSpace(preferences.Theme))
	if preferences.Theme == "" {
		return theme.Auto, ThemeDefault, nil
	}
	if !theme.Valid(preferences.Theme) {
		return theme.Auto, ThemeDefault, fmt.Errorf("theme preferences contain an unsupported theme")
	}
	return preferences.Theme, ThemeConfig, nil
}

func SaveTheme(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if !theme.Valid(name) {
		return fmt.Errorf("unsupported theme")
	}
	preferences, _, err := loadPreferences()
	if err != nil {
		return err
	}
	preferences.Theme = name
	return savePreferences(preferences)
}

func LoadInterfaceLanguage() (string, error) {
	preferences, exists, err := loadPreferences()
	if err != nil {
		return defaultInterfaceLanguage(), err
	}
	if !exists || strings.TrimSpace(preferences.InterfaceLanguage) == "" {
		return defaultInterfaceLanguage(), nil
	}
	language := strings.ToLower(strings.TrimSpace(preferences.InterfaceLanguage))
	if !validInterfaceLanguage(language) {
		return defaultInterfaceLanguage(), fmt.Errorf("interface language preference is unsupported")
	}
	return language, nil
}

func SaveInterfaceLanguage(language string) error {
	language = strings.ToLower(strings.TrimSpace(language))
	if !validInterfaceLanguage(language) {
		return fmt.Errorf("unsupported interface language")
	}
	preferences, _, err := loadPreferences()
	if err != nil {
		return err
	}
	preferences.InterfaceLanguage = language
	return savePreferences(preferences)
}

func validInterfaceLanguage(language string) bool {
	return language == InterfaceLanguageChinese || language == InterfaceLanguageEnglish
}

func defaultInterfaceLanguage() string {
	languages, err := loadPreferredUILanguages()
	if err != nil {
		return InterfaceLanguageChinese
	}
	for _, language := range languages {
		language = strings.ToLower(strings.TrimSpace(language))
		if language == "" {
			continue
		}
		if language == "zh" || strings.HasPrefix(language, "zh-") {
			return InterfaceLanguageChinese
		}
		return InterfaceLanguageEnglish
	}
	return InterfaceLanguageChinese
}

func loadPreferences() (Preferences, bool, error) {
	path, err := Path()
	if err != nil {
		return Preferences{}, false, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Preferences{}, false, nil
	}
	if err != nil {
		return Preferences{}, false, fmt.Errorf("read preferences: %w", err)
	}
	var preferences Preferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return Preferences{}, false, fmt.Errorf("decode preferences")
	}
	if strings.TrimSpace(preferences.InterfaceLanguage) == "" {
		preferences.InterfaceLanguage = preferences.LegacyActivityLanguage
	}
	preferences.LegacyActivityLanguage = ""
	return preferences, true, nil
}

func savePreferences(preferences Preferences) error {
	path, err := Path()
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	data, err := json.MarshalIndent(preferences, "", "  ")
	if err != nil {
		return fmt.Errorf("encode preferences: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(parent, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create preferences: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure preferences: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close preferences: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace preferences: %w", err)
	}
	return nil
}

func Path() (string, error) {
	home, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fileName), nil
}
