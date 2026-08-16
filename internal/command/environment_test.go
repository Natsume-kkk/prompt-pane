package command

import (
	"errors"
	"testing"
)

func TestCheckPlatformAcceptsOnlyWindowsX64(t *testing.T) {
	if err := checkPlatform("windows", "amd64"); err != nil {
		t.Fatalf("windows/amd64 was rejected: %v", err)
	}
	for _, platform := range [][2]string{{"windows", "arm64"}, {"linux", "amd64"}} {
		if err := checkPlatform(platform[0], platform[1]); err == nil {
			t.Fatalf("%s/%s was accepted", platform[0], platform[1])
		}
	}
}

func TestSupportedPowerShellVersions(t *testing.T) {
	for _, version := range []string{"5.1.26100.4652", "7.0.0", "\ufeff7.4.2\r\n"} {
		if !supportedPowerShellVersion(version) {
			t.Fatalf("supported version %q was rejected", version)
		}
	}
	for _, version := range []string{"5.0.0", "6.2.7", "7", "not-a-version"} {
		if supportedPowerShellVersion(version) {
			t.Fatalf("unsupported version %q was accepted", version)
		}
	}
}

func TestFindPowerShellPrefersPowerShellSeven(t *testing.T) {
	paths := map[string]string{"pwsh": `C:\Program Files\PowerShell\7\pwsh.exe`, "powershell": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`}
	versions := map[string]string{paths["pwsh"]: "7.4.2", paths["powershell"]: "5.1.26100"}
	shell, err := findPowerShellWith(
		func(name string) (string, error) { return paths[name], nil },
		func(path string) (string, error) { return versions[path], nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if shell.Path != paths["pwsh"] || shell.Version != "7.4.2" {
		t.Fatalf("shell = %#v", shell)
	}
}

func TestFindPowerShellFallsBackToWindowsPowerShell(t *testing.T) {
	paths := map[string]string{"pwsh": `C:\tools\pwsh.exe`, "powershell": `C:\Windows\powershell.exe`}
	shell, err := findPowerShellWith(
		func(name string) (string, error) { return paths[name], nil },
		func(path string) (string, error) {
			if path == paths["pwsh"] {
				return "6.2.7", nil
			}
			return "5.1.19041", nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if shell.Path != paths["powershell"] || shell.Version != "5.1.19041" {
		t.Fatalf("shell = %#v", shell)
	}
}

func TestFindPowerShellRejectsMissingOrUnsupportedShells(t *testing.T) {
	_, err := findPowerShellWith(
		func(string) (string, error) { return "", errors.New("not found") },
		func(string) (string, error) { return "", nil },
	)
	if err == nil {
		t.Fatal("missing PowerShell was accepted")
	}
}
