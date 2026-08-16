package command

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type powerShellInfo struct {
	Path    string
	Version string
}

func requireWindowsX64() error {
	return checkPlatform(runtime.GOOS, runtime.GOARCH)
}

func checkPlatform(goos, goarch string) error {
	if goos != "windows" || goarch != "amd64" {
		return fmt.Errorf("Prompt Pane v1.1.0 supports Windows x64 only (current: %s/%s)", goos, goarch)
	}
	return nil
}

func findPowerShell() (powerShellInfo, error) {
	return findPowerShellWith(exec.LookPath, probePowerShellVersion)
}

func findPowerShellWith(lookup func(string) (string, error), probe func(string) (string, error)) (powerShellInfo, error) {
	for _, name := range []string{"pwsh", "powershell"} {
		path, err := lookup(name)
		if err != nil {
			continue
		}
		version, err := probe(path)
		if err != nil || !supportedPowerShellVersion(version) {
			continue
		}
		return powerShellInfo{Path: path, Version: strings.TrimSpace(strings.TrimPrefix(version, "\ufeff"))}, nil
	}
	return powerShellInfo{}, fmt.Errorf("PowerShell 5.1 or 7 was not found or could not run")
}

func probePowerShellVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(
		ctx, path, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
		`[Console]::Out.Write($PSVersionTable.PSVersion.ToString())`,
	).Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func supportedPowerShellVersion(version string) bool {
	parts := strings.Split(strings.TrimSpace(strings.TrimPrefix(version, "\ufeff")), ".")
	if len(parts) < 2 {
		return false
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return false
	}
	return major >= 7 || major == 5 && minor >= 1
}
