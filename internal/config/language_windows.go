//go:build windows

package config

import "golang.org/x/sys/windows"

func platformPreferredUILanguages() ([]string, error) {
	return windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
}
