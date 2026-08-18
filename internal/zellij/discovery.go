package zellij

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	processutil "github.com/Natsume-kkk/prompt-pane/internal/process"
)

const Version = "0.44.3"

func Find() (string, error) {
	if path, err := exec.LookPath("zellij"); err == nil {
		if compatible(path) {
			return path, nil
		}
	}
	path, err := ManagedPath()
	if err == nil && compatible(path) {
		return path, nil
	}
	return "", fmt.Errorf("Zellij %s is not available", Version)
}

func ManagedPath() (string, error) {
	root, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "bin", executableName()), nil
}

func compatible(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	output, err := processutil.Output(ctx, path, []string{"--version"}, processutil.OutputOptions{Limit: 4 << 10})
	return err == nil && strings.TrimSpace(string(output)) == "zellij "+Version
}

func executableName() string {
	if filepath.Separator == '\\' {
		return "zellij.exe"
	}
	return "zellij"
}
