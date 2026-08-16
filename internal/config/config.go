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

type ThemeSource string

const (
	ThemeDefault     ThemeSource = "default"
	ThemeConfig      ThemeSource = "config"
	ThemeEnvironment ThemeSource = "environment"
)

type Preferences struct {
	Theme string `json:"theme"`
}

func LoadTheme() (string, ThemeSource, error) {
	if value, ok := os.LookupEnv(theme.Environment); ok {
		value = strings.ToLower(strings.TrimSpace(value))
		if !theme.Valid(value) {
			return theme.Auto, ThemeEnvironment, fmt.Errorf("%s has an unsupported theme", theme.Environment)
		}
		return value, ThemeEnvironment, nil
	}
	path, err := Path()
	if err != nil {
		return theme.Auto, ThemeDefault, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return theme.Auto, ThemeDefault, nil
	}
	if err != nil {
		return theme.Auto, ThemeDefault, fmt.Errorf("read theme preferences: %w", err)
	}
	var preferences Preferences
	if err := json.Unmarshal(data, &preferences); err != nil {
		return theme.Auto, ThemeDefault, fmt.Errorf("decode theme preferences")
	}
	preferences.Theme = strings.ToLower(strings.TrimSpace(preferences.Theme))
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
	path, err := Path()
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create preferences directory: %w", err)
	}
	data, err := json.MarshalIndent(Preferences{Theme: name}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode theme preferences: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(parent, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create theme preferences: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("secure theme preferences: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write theme preferences: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close theme preferences: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace theme preferences: %w", err)
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
