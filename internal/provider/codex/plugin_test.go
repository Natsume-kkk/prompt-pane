package codex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	"github.com/Natsume-kkk/prompt-pane/plugins"
)

func TestWindowsHooksInvokePluginExecutableWithPowerShell(t *testing.T) {
	data, err := plugins.Content.ReadFile("prompt-pane/hooks/hooks.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				CommandWindows string `json:"commandWindows"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatal(err)
	}
	const expected = `& "${PLUGIN_ROOT}/bin/prompt-pane.exe" _hook codex; exit $LASTEXITCODE`
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"} {
		groups := config.Hooks[event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s must contain exactly one command hook", event)
		}
		if groups[0].Hooks[0].CommandWindows != expected {
			t.Fatalf("%s Windows command = %q, want %q", event, groups[0].Hooks[0].CommandWindows, expected)
		}
	}
	if got := config.Hooks["SessionStart"][0].Matcher; got != "^(startup|resume|clear|compact)$" {
		t.Fatalf("SessionStart matcher = %q", got)
	}
}

func TestWriteMarketplaceUsesCodexDirectoryContract(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(t.TempDir(), "prompt-pane.exe")
	if err := os.WriteFile(executable, []byte("synthetic executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMarketplace(root, executable); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(root, ".agents", "plugins", "marketplace.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read marketplace manifest: %v", err)
	}
	var manifest struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode marketplace manifest: %v", err)
	}
	if manifest.Name != marketplaceName || len(manifest.Plugins) != 1 {
		t.Fatalf("unexpected marketplace manifest: %#v", manifest)
	}
	plugin := manifest.Plugins[0]
	if plugin.Name != pluginName || plugin.Source.Source != "local" || plugin.Source.Path != "./plugins/prompt-pane" {
		t.Fatalf("unexpected plugin source: %#v", plugin)
	}

	for _, relative := range []string{
		filepath.Join("plugins", pluginName, ".codex-plugin", "plugin.json"),
		filepath.Join("plugins", pluginName, "hooks", "hooks.json"),
		filepath.Join("plugins", pluginName, "bin", "prompt-pane.exe"),
	} {
		if info, err := os.Stat(filepath.Join(root, relative)); err != nil || !info.Mode().IsRegular() {
			t.Fatalf("embedded plugin file %q is missing or invalid", relative)
		}
	}
	installed, err := os.ReadFile(filepath.Join(root, "plugins", pluginName, "bin", "prompt-pane.exe"))
	if err != nil || string(installed) != "synthetic executable" {
		t.Fatal("plugin executable was not copied exactly")
	}
	if _, err := os.Stat(filepath.Join(root, "marketplace.json")); !os.IsNotExist(err) {
		t.Fatal("legacy root marketplace manifest should not be written")
	}
}

func TestCachedPluginFallbackRequiresEnabledMatchingPayload(t *testing.T) {
	version, err := embeddedPluginVersion()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	promptPaneHome := t.TempDir()
	t.Setenv(paths.EnvHome, promptPaneHome)
	executable := filepath.Join(t.TempDir(), "prompt-pane.exe")
	if err := os.WriteFile(executable, []byte("current executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(home, "plugins", "cache", marketplaceName, pluginName, version)
	if err := os.MkdirAll(filepath.Join(cache, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cache, "bin"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(`{"name":%q,"version":%q}`, pluginName, version)
	installedManifest := filepath.Join(promptPaneHome, "codex-marketplace", "plugins", pluginName, ".codex-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(installedManifest), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedManifest, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, ".codex-plugin", "plugin.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "bin", "prompt-pane.exe"), []byte("current executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := "[plugins.\"prompt-pane@prompt-pane\"]\nenabled = true\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	if !cachedPluginMatches(executable, home) || !pluginEnabled(home) {
		t.Fatal("matching enabled plugin was not accepted")
	}
	if !PluginInstalledFor(filepath.Join(home, "missing-codex"), executable) {
		t.Fatal("explicit matching current executable was not accepted")
	}
	if err := os.WriteFile(filepath.Join(cache, "bin", "prompt-pane.exe"), []byte("stale executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cachedPluginMatches(executable, home) {
		t.Fatal("stale cached executable was accepted")
	}
	if PluginInstalledFor(filepath.Join(home, "missing-codex"), executable) {
		t.Fatal("explicit stale executable was accepted")
	}
}

func TestPluginEnabledIsScopedToExactPluginSection(t *testing.T) {
	home := t.TempDir()
	config := "[plugins.\"other@prompt-pane\"]\nenabled = true\n\n[plugins.\"prompt-pane@prompt-pane\"]\nenabled = false\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	if pluginEnabled(home) {
		t.Fatal("another plugin or disabled Prompt Pane was accepted")
	}
}

func TestPreflightInstallAccessSupportsCustomUnicodePaths(t *testing.T) {
	root := t.TempDir()
	promptPaneHome := filepath.Join(root, "Prompt Pane 数据")
	codexHome := filepath.Join(root, "Codex 配置")
	t.Setenv("PROMPT_PANE_HOME", promptPaneHome)
	t.Setenv("CODEX_HOME", codexHome)

	if err := PreflightInstallAccess(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{promptPaneHome, codexHome} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("preflight-created directory %q was not cleaned: %v", path, err)
		}
	}
}

func TestInstallationSnapshotRestoresMarketplaceAndRegistration(t *testing.T) {
	root := t.TempDir()
	promptPaneHome := filepath.Join(root, "prompt-pane-home")
	codexConfig := filepath.Join(root, "codex-home")
	t.Setenv("PROMPT_PANE_HOME", promptPaneHome)
	t.Setenv("CODEX_HOME", codexConfig)
	if err := os.MkdirAll(codexConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	config := "[plugins.\"prompt-pane@prompt-pane\"]\nenabled = true\n"
	if err := os.WriteFile(filepath.Join(codexConfig, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(root, "codex.cmd")
	command := "@echo {\"installed\":[{\"name\":\"prompt-pane\",\"installed\":true,\"enabled\":true,\"marketplaceName\":\"prompt-pane\"}]}\r\n"
	if err := os.WriteFile(codexPath, []byte(command), 0o700); err != nil {
		t.Fatal(err)
	}
	marketplace, err := marketplaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	oldFile := filepath.Join(marketplace, "plugins", pluginName, "old.txt")
	if err := os.MkdirAll(filepath.Dir(oldFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("old marketplace"), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureInstallation(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Discard()
	if err := os.RemoveAll(marketplace); err != nil {
		t.Fatal(err)
	}
	newFile := filepath.Join(marketplace, "new.txt")
	if err := os.MkdirAll(marketplace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("new marketplace"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := snapshot.Restore(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(oldFile)
	if err != nil || string(data) != "old marketplace" {
		t.Fatalf("restored marketplace = %q, err = %v", data, err)
	}
	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Fatalf("failed marketplace content remained: %v", err)
	}
}
