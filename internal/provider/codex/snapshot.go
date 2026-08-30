package codex

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Natsume-kkk/prompt-pane/internal/filetxn"
)

type InstallationSnapshot struct {
	codexPath  string
	root       string
	backupRoot string
	rootExists bool
	registered bool
	enabled    bool
	config     *filetxn.Snapshot
}

func CaptureInstallation(codexPath string) (*InstallationSnapshot, error) {
	root, err := marketplaceRoot()
	if err != nil {
		return nil, err
	}
	home, err := codexHome()
	if err != nil {
		return nil, err
	}
	config, err := filetxn.Capture(filepath.Join(home, "config.toml"))
	if err != nil {
		return nil, fmt.Errorf("snapshot Codex plugin configuration: %w", err)
	}
	listed := inspectListedPlugin(codexPath)
	configured := inspectPluginConfig(home)
	snapshot := &InstallationSnapshot{
		codexPath:  codexPath,
		root:       root,
		registered: listed.installed || configured.present,
		enabled:    listed.enabled || configured.enabled,
		config:     config,
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Prompt Pane marketplace: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Prompt Pane marketplace is not a directory")
	}
	temporary, err := os.MkdirTemp("", "prompt-pane-plugin-snapshot-*")
	if err != nil {
		return nil, fmt.Errorf("create plugin rollback snapshot: %w", err)
	}
	snapshot.backupRoot = filepath.Join(temporary, "codex-marketplace")
	if err := copyTree(root, snapshot.backupRoot); err != nil {
		_ = os.RemoveAll(temporary)
		return nil, fmt.Errorf("snapshot Prompt Pane marketplace: %w", err)
	}
	snapshot.rootExists = true
	return snapshot, nil
}

func (s *InstallationSnapshot) Restore() error {
	if s == nil {
		return nil
	}
	var restoreErr error
	_, _ = runCodex(s.codexPath, "plugin", "remove", pluginName+"@"+marketplaceName, "--json")
	_, _ = runCodex(s.codexPath, "plugin", "marketplace", "remove", marketplaceName, "--json")
	if err := os.RemoveAll(s.root); err != nil {
		restoreErr = errors.Join(restoreErr, fmt.Errorf("remove failed plugin installation during rollback: %w", err))
	}
	if restoreErr == nil && s.rootExists {
		if err := copyTree(s.backupRoot, s.root); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore Prompt Pane marketplace: %w", err))
		}
		if restoreErr == nil {
			equal, err := treesEqual(s.backupRoot, s.root)
			if err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("verify restored Prompt Pane marketplace: %w", err))
			} else if !equal {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("verify restored Prompt Pane marketplace: restored files do not match the snapshot"))
			}
		}
	}
	if s.registered {
		if !s.rootExists {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore Prompt Pane plugin registration: previous marketplace files are unavailable"))
		} else if restoreErr == nil {
			if _, err := runCodex(s.codexPath, "plugin", "marketplace", "add", s.root, "--json"); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("restore Prompt Pane marketplace registration: %w", err))
			}
			if restoreErr == nil {
				if _, err := runCodex(s.codexPath, "plugin", "add", pluginName+"@"+marketplaceName, "--json"); err != nil {
					restoreErr = errors.Join(restoreErr, fmt.Errorf("restore Prompt Pane plugin registration: %w", err))
				}
			}
		}
	}
	if s.config != nil {
		if err := s.config.Restore(); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("restore Codex plugin configuration: %w", err))
		}
	}
	if restoreErr != nil {
		return restoreErr
	}
	home, err := codexHome()
	if err != nil {
		return fmt.Errorf("verify restored Prompt Pane plugin state: %w", err)
	}
	listed := inspectListedPlugin(s.codexPath)
	configured := inspectPluginConfig(home)
	registered := listed.installed || configured.present
	enabled := listed.enabled || configured.enabled
	if registered != s.registered || enabled != s.enabled {
		return fmt.Errorf("verify restored Prompt Pane plugin state")
	}
	return nil
}

func (s *InstallationSnapshot) Discard() error {
	if s == nil || s.backupRoot == "" {
		return nil
	}
	return os.RemoveAll(filepath.Dir(s.backupRoot))
}

func copyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		inputCloseErr := input.Close()
		closeErr := output.Close()
		if copyErr != nil {
			return copyErr
		}
		if inputCloseErr != nil {
			return inputCloseErr
		}
		return closeErr
	})
}

func treesEqual(left, right string) (bool, error) {
	leftFiles := map[string][]byte{}
	if err := filepath.WalkDir(left, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(left, path)
		if err != nil {
			return err
		}
		leftFiles[relative], err = os.ReadFile(path)
		return err
	}); err != nil {
		return false, err
	}
	seen := 0
	if err := filepath.WalkDir(right, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(right, path)
		if err != nil {
			return err
		}
		want, ok := leftFiles[relative]
		if !ok {
			return fmt.Errorf("unexpected restored file %s", relative)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(got, want) {
			return fmt.Errorf("restored file differs: %s", relative)
		}
		seen++
		return nil
	}); err != nil {
		return false, err
	}
	return seen == len(leftFiles), nil
}
