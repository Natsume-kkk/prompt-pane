package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

const EnvHome = "PROMPT_PANE_HOME"

func Home() (string, error) {
	if override := os.Getenv(EnvHome); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("%s must be an absolute path", EnvHome)
		}
		return filepath.Clean(override), nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate user config directory: %w", err)
	}
	return filepath.Join(root, "PromptPane"), nil
}
