package shortcut

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

func TestCodexAliasInvocation(t *testing.T) {
	for _, name := range []string{"codex.pp", "codex.pp.exe", `C:\tools\CODEX.PP.EXE`} {
		if !IsCodexAlias(name) {
			t.Fatalf("%q was not recognized", name)
		}
	}
	if IsCodexAlias("codex.cmd") {
		t.Fatal("the user's Codex command was treated as the Prompt Pane alias")
	}
}

func TestInstallInspectAndRemoveCodexAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "state"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	executable := filepath.Join(root, "prompt-pane.exe")
	if err := os.WriteFile(codexPath, []byte("codex"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("prompt-pane-v1"), 0o700); err != nil {
		t.Fatal(err)
	}

	target, err := Install(codexPath, executable)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(target) != AliasName {
		t.Fatalf("target = %q", target)
	}
	if _, ok, err := Installed(codexPath, executable); err != nil || !ok {
		t.Fatalf("installed = %v, err = %v", ok, err)
	}
	if managed, err := Managed(codexPath); err != nil || !managed {
		t.Fatalf("managed = %v, err = %v", managed, err)
	}
	if removed, err := Remove(codexPath); err != nil || !removed {
		t.Fatal(err)
	}
	if managed, err := Managed(codexPath); err != nil || managed {
		t.Fatalf("managed after removal = %v, err = %v", managed, err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("alias still exists: %v", err)
	}
}

func TestInstallationSnapshotRestoresExecutableAndOwnershipState(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "state"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	oldExecutable := filepath.Join(root, "prompt-pane-old.exe")
	newExecutable := filepath.Join(root, "prompt-pane-new.exe")
	for path, data := range map[string]string{
		codexPath:     "codex",
		oldExecutable: "old executable",
		newExecutable: "new executable",
	} {
		if err := os.WriteFile(path, []byte(data), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Install(codexPath, oldExecutable); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureInstallation(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(codexPath, newExecutable); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	if _, installed, err := Installed(codexPath, oldExecutable); err != nil || !installed {
		t.Fatalf("restored installation = %v, err = %v", installed, err)
	}
	if _, installed, err := Installed(codexPath, newExecutable); err != nil || installed {
		t.Fatalf("new installation remained active = %v, err = %v", installed, err)
	}
}

func TestInstallAdoptsRunningAliasWithoutReplacingIt(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "Prompt Pane data"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	target := filepath.Join(bin, AliasName)
	if err := os.WriteFile(codexPath, []byte("codex"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := []byte("running prompt pane")
	if err := os.WriteFile(target, content, 0o700); err != nil {
		t.Fatal(err)
	}

	installed, err := Install(codexPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if installed != target {
		t.Fatalf("installed path = %q, want %q", installed, target)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("running alias was replaced: %q", got)
	}
	if _, ready, err := Installed(codexPath, target); err != nil || !ready {
		t.Fatalf("adopted alias ready = %v, err = %v", ready, err)
	}
}

func TestInstallRefusesUnmanagedCollision(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "state"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	target := filepath.Join(bin, AliasName)
	executable := filepath.Join(root, "prompt-pane.exe")
	if err := os.WriteFile(target, []byte("someone else"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("prompt-pane"), 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := Install(codexPath, executable)
	if err == nil || !strings.Contains(err.Error(), "not managed") {
		t.Fatalf("collision error = %v", err)
	}
	data, readErr := os.ReadFile(target)
	if readErr != nil || string(data) != "someone else" {
		t.Fatal("collision target was modified")
	}
}

func TestRemoveRefusesModifiedManagedAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "state"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	executable := filepath.Join(root, "prompt-pane.exe")
	if err := os.WriteFile(executable, []byte("prompt-pane"), 0o700); err != nil {
		t.Fatal(err)
	}
	target, err := Install(codexPath, executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("modified"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(codexPath); err == nil || !strings.Contains(err.Error(), "modified") {
		t.Fatalf("remove error = %v", err)
	}
}

func TestInstallUpdatesOnlyPreviouslyManagedAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "state"))
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(bin, "codex.cmd")
	executable := filepath.Join(root, "prompt-pane.exe")
	if err := os.WriteFile(executable, []byte("prompt-pane-v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	target, err := Install(codexPath, executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("prompt-pane-v2"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Preflight(codexPath, executable); err != nil {
		t.Fatalf("managed update was rejected: %v", err)
	}
	if _, err := Install(codexPath, executable); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "prompt-pane-v2" {
		t.Fatalf("updated alias = %q, err = %v", data, err)
	}
}

func TestPreflightInstallAccessSupportsUnicodePaths(t *testing.T) {
	root := t.TempDir()
	t.Setenv(paths.EnvHome, filepath.Join(root, "Prompt Pane 数据"))
	codexPath := filepath.Join(root, "Codex 工具", "codex.cmd")

	if err := PreflightInstallAccess(codexPath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Dir(codexPath), filepath.Join(root, "Prompt Pane 数据")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("preflight-created directory %q was not cleaned: %v", path, err)
		}
	}
}
