package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	processutil "github.com/Natsume-kkk/prompt-pane/internal/process"
	"github.com/Natsume-kkk/prompt-pane/plugins"
)

const (
	pluginName      = "prompt-pane"
	marketplaceName = "prompt-pane"
)

type pluginList struct {
	Installed []struct {
		Name        string `json:"name"`
		Installed   bool   `json:"installed"`
		Enabled     bool   `json:"enabled"`
		Marketplace string `json:"marketplaceName"`
	} `json:"installed"`
}

func PluginInstalled(codexPath string) bool {
	executable, err := os.Executable()
	if err != nil {
		return false
	}
	return PluginInstalledFor(codexPath, executable)
}

func PluginInstalledFor(codexPath, executable string) bool {
	listed := pluginListed(codexPath)
	home, err := codexHome()
	if err != nil || !cachedPluginMatches(executable, home) {
		return false
	}
	// Codex 0.147 on Windows can return an empty plugin list for a cached,
	// enabled personal plugin. The cache and exact config entry are the runtime
	// facts in that known failure mode.
	return listed || pluginEnabled(home)
}

func pluginListed(codexPath string) bool {
	output, err := runCodex(codexPath, "plugin", "list", "--json")
	if err != nil {
		return false
	}
	var list pluginList
	if json.Unmarshal(output, &list) != nil {
		return false
	}
	for _, entry := range list.Installed {
		if entry.Name == pluginName && entry.Marketplace == marketplaceName && entry.Installed && entry.Enabled {
			return true
		}
	}
	return false
}

func codexHome() (string, error) {
	if override := os.Getenv("CODEX_HOME"); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("CODEX_HOME must be an absolute path")
		}
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func cachedPluginMatches(executable, home string) bool {
	version, err := installedPluginVersion()
	if err != nil || version == "" || filepath.Base(version) != version {
		return false
	}
	root := filepath.Join(home, "plugins", "cache", marketplaceName, pluginName, version)
	manifest, err := readPluginManifest(filepath.Join(root, ".codex-plugin", "plugin.json"))
	if err != nil || manifest.Name != pluginName || manifest.Version != version {
		return false
	}
	return sameFileContents(executable, filepath.Join(root, "bin", "prompt-pane.exe"))
}

func installedPluginVersion() (string, error) {
	root, err := marketplaceRoot()
	if err != nil {
		return "", err
	}
	manifest, err := readPluginManifest(filepath.Join(root, "plugins", pluginName, ".codex-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	if manifest.Name != pluginName || manifest.Version == "" || filepath.Base(manifest.Version) != manifest.Version {
		return "", fmt.Errorf("installed plugin manifest identity is invalid")
	}
	return manifest.Version, nil
}

func pluginEnabled(home string) bool {
	file, err := os.Open(filepath.Join(home, "config.toml"))
	if err != nil {
		return false
	}
	defer file.Close()

	const section = `[plugins."prompt-pane@prompt-pane"]`
	inSection := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inSection = line == section
			continue
		}
		if !inSection {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(key) == "enabled" {
			return strings.TrimSpace(value) == "true"
		}
	}
	return false
}

type pluginManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func embeddedPluginVersion() (string, error) {
	data, err := fs.ReadFile(plugins.Content, pluginName+"/.codex-plugin/plugin.json")
	if err != nil {
		return "", err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	return manifest.Version, nil
}

func readPluginManifest(path string) (pluginManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pluginManifest{}, err
	}
	var manifest pluginManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return pluginManifest{}, err
	}
	return manifest, nil
}

func sameFileContents(left, right string) bool {
	leftHash, leftSize, err := fileHash(left)
	if err != nil {
		return false
	}
	rightHash, rightSize, err := fileHash(right)
	return err == nil && leftSize == rightSize && leftHash == rightHash
}

func fileHash(path string) ([sha256.Size]byte, int64, error) {
	var digest [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return digest, 0, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return digest, 0, fmt.Errorf("plugin executable is not a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return digest, 0, err
	}
	copy(digest[:], hash.Sum(nil))
	return digest, info.Size(), nil
}

func InstallPlugin(codexPath string) error {
	root, err := marketplaceRoot()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate Prompt Pane executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o700); err != nil {
		return fmt.Errorf("create Prompt Pane data directory: %w", err)
	}
	stagedRoot, err := os.MkdirTemp(filepath.Dir(root), ".codex-marketplace-stage-*")
	if err != nil {
		return fmt.Errorf("create Prompt Pane marketplace staging directory: %w", err)
	}
	defer os.RemoveAll(stagedRoot)
	if err := writeMarketplace(stagedRoot, executable); err != nil {
		return err
	}
	if err := validateMarketplace(stagedRoot, executable); err != nil {
		return fmt.Errorf("validate staged Prompt Pane marketplace: %w", err)
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("replace Prompt Pane marketplace: %w", err)
	}
	if err := os.Rename(stagedRoot, root); err != nil {
		return fmt.Errorf("activate Prompt Pane marketplace: %w", err)
	}
	if err := validateMarketplace(root, executable); err != nil {
		return fmt.Errorf("validate active Prompt Pane marketplace: %w", err)
	}
	// A previous interrupted setup can leave either registration behind. The
	// confirmed setup operation replaces only Prompt Pane's exact plugin IDs.
	_, _ = runCodex(codexPath, "plugin", "remove", pluginName+"@"+marketplaceName, "--json")
	_, _ = runCodex(codexPath, "plugin", "marketplace", "remove", marketplaceName, "--json")
	if _, err := runCodex(codexPath, "plugin", "marketplace", "add", root, "--json"); err != nil {
		return fmt.Errorf("register Prompt Pane marketplace: %w", err)
	}
	if _, err := runCodex(codexPath, "plugin", "add", pluginName+"@"+marketplaceName, "--json"); err != nil {
		return fmt.Errorf("install Prompt Pane plugin: %w", err)
	}
	if !PluginInstalled(codexPath) {
		return fmt.Errorf("Codex did not activate the installed Prompt Pane plugin")
	}
	return nil
}

func validateMarketplace(root, executable string) error {
	version, err := embeddedPluginVersion()
	if err != nil {
		return err
	}
	manifest, err := readPluginManifest(filepath.Join(root, "plugins", pluginName, ".codex-plugin", "plugin.json"))
	if err != nil {
		return err
	}
	if manifest.Name != pluginName || manifest.Version != version {
		return fmt.Errorf("plugin manifest identity does not match the embedded plugin")
	}
	if !sameFileContents(executable, filepath.Join(root, "plugins", pluginName, "bin", "prompt-pane.exe")) {
		return fmt.Errorf("plugin executable does not match the current Prompt Pane executable")
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "plugins", "marketplace.json")); err != nil {
		return err
	}
	return nil
}

func PreflightInstallAccess() error {
	root, err := marketplaceRoot()
	if err != nil {
		return err
	}
	if err := paths.ProbeWritableDirectory(root); err != nil {
		return fmt.Errorf("Prompt Pane plugin directory is not writable: %w", err)
	}
	home, err := codexHome()
	if err != nil {
		return err
	}
	if err := paths.ProbeWritableDirectory(home); err != nil {
		return fmt.Errorf("Codex configuration directory is not writable: %w", err)
	}
	return nil
}

func RemovePlugin(codexPath string) error {
	if PluginInstalled(codexPath) {
		if _, err := runCodex(codexPath, "plugin", "remove", pluginName+"@"+marketplaceName, "--json"); err != nil {
			return fmt.Errorf("remove Prompt Pane plugin: %w", err)
		}
	}
	_, _ = runCodex(codexPath, "plugin", "marketplace", "remove", marketplaceName, "--json")
	root, err := marketplaceRoot()
	if err != nil {
		return err
	}
	if filepath.Base(root) != "codex-marketplace" || filepath.Base(filepath.Dir(root)) != "PromptPane" && os.Getenv(paths.EnvHome) == "" {
		return fmt.Errorf("refusing to remove an unexpected marketplace path")
	}
	if err := os.RemoveAll(root); err != nil {
		return fmt.Errorf("remove Prompt Pane marketplace: %w", err)
	}
	return nil
}

func marketplaceRoot() (string, error) {
	config, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "codex-marketplace"), nil
}

func writeMarketplace(root, executable string) error {
	pluginRoot := filepath.Join(root, "plugins", pluginName)
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}
	if err := fs.WalkDir(plugins.Content, pluginName, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(pluginName, filepath.FromSlash(path))
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(pluginRoot, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := fs.ReadFile(plugins.Content, path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	}); err != nil {
		return fmt.Errorf("extract Prompt Pane plugin: %w", err)
	}
	if err := copyExecutable(executable, filepath.Join(pluginRoot, "bin", "prompt-pane.exe")); err != nil {
		return err
	}
	marketplace := map[string]any{
		"name":      marketplaceName,
		"interface": map[string]string{"displayName": "Prompt Pane"},
		"plugins": []any{map[string]any{
			"name":     pluginName,
			"source":   map[string]string{"source": "local", "path": "./plugins/" + pluginName},
			"policy":   map[string]string{"installation": "AVAILABLE", "authentication": "ON_INSTALL"},
			"category": "Productivity",
		}},
	}
	data, err := json.MarshalIndent(marketplace, "", "  ")
	if err != nil {
		return fmt.Errorf("encode marketplace: %w", err)
	}
	data = append(data, '\n')
	manifestRoot := filepath.Join(root, ".agents", "plugins")
	if err := os.MkdirAll(manifestRoot, 0o700); err != nil {
		return fmt.Errorf("create marketplace manifest directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(manifestRoot, "marketplace.json"), data, 0o600); err != nil {
		return fmt.Errorf("write marketplace: %w", err)
	}
	return nil
}

func copyExecutable(source, target string) error {
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return fmt.Errorf("resolve Prompt Pane executable: %w", err)
	}
	targetPath, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve plugin executable: %w", err)
	}
	if strings.EqualFold(filepath.Clean(sourcePath), filepath.Clean(targetPath)) {
		return nil
	}
	info, err := os.Stat(sourcePath)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Prompt Pane executable is not a regular file")
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("read Prompt Pane executable: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return fmt.Errorf("create plugin binary directory: %w", err)
	}
	if err := os.WriteFile(targetPath, data, 0o700); err != nil {
		return fmt.Errorf("write plugin executable: %w", err)
	}
	return nil
}

func runCodex(path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := processutil.Output(ctx, path, args, processutil.OutputOptions{Limit: 1 << 20})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("Codex command timed out")
		}
		return nil, fmt.Errorf("Codex command failed")
	}
	return output, nil
}
