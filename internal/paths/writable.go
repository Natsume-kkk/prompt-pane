package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

func ProbeWritableDirectory(directory string) error {
	if !filepath.IsAbs(directory) {
		return fmt.Errorf("write target must be an absolute path: %s", directory)
	}
	directory = filepath.Clean(directory)
	created := missingDirectories(directory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create write target %s: %w", directory, err)
	}
	cleanupDirectories := func() error {
		for _, path := range created {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	}
	probe, err := os.CreateTemp(directory, ".prompt-pane-write-probe-*")
	if err != nil {
		_ = cleanupDirectories()
		return fmt.Errorf("write target is not writable %s: %w", directory, err)
	}
	probePath := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		_ = cleanupDirectories()
		return fmt.Errorf("close write probe in %s: %w", directory, err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("clean write probe in %s: %w", directory, err)
	}
	if err := cleanupDirectories(); err != nil {
		return fmt.Errorf("clean write probe directories for %s: %w", directory, err)
	}
	return nil
}

func missingDirectories(directory string) []string {
	var missing []string
	for current := directory; ; current = filepath.Dir(current) {
		if _, err := os.Stat(current); err == nil {
			return missing
		} else if !os.IsNotExist(err) {
			return missing
		}
		missing = append(missing, current)
		if parent := filepath.Dir(current); parent == current {
			return missing
		}
	}
}
