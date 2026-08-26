//go:build !windows

package config

func platformPreferredUILanguages() ([]string, error) {
	return nil, nil
}
