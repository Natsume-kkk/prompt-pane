package shortcut

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

const (
	AliasName = "codex.pp.exe"
	stateName = "codex-launcher.json"
)

type state struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

func IsCodexAlias(invocation string) bool {
	base := filepath.Base(invocation)
	return strings.EqualFold(base, AliasName) || strings.EqualFold(base, "codex.pp")
}

func Target(codexPath string) (string, error) {
	path, err := filepath.Abs(codexPath)
	if err != nil {
		return "", fmt.Errorf("resolve Codex CLI path: %w", err)
	}
	return filepath.Join(filepath.Dir(path), AliasName), nil
}

func Installed(codexPath, executable string) (string, bool, error) {
	target, err := Target(codexPath)
	if err != nil {
		return "", false, err
	}
	installed, err := readState()
	if errors.Is(err, os.ErrNotExist) {
		return target, false, nil
	}
	if err != nil {
		return target, false, err
	}
	if !samePath(installed.Path, target) {
		return target, false, nil
	}
	targetHash, err := fileHash(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return target, false, nil
		}
		return target, false, err
	}
	sourceHash, err := fileHash(executable)
	if err != nil {
		return target, false, fmt.Errorf("inspect Prompt Pane executable: %w", err)
	}
	return target, targetHash == installed.SHA256 && targetHash == sourceHash, nil
}

func Managed(codexPath string) (bool, error) {
	target, err := Target(codexPath)
	if err != nil {
		return false, err
	}
	installed, err := readState()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return samePath(installed.Path, target), nil
}

func Install(codexPath, executable string) (string, error) {
	target, err := Target(codexPath)
	if err != nil {
		return "", err
	}
	sourceHash, err := fileHash(executable)
	if err != nil {
		return "", fmt.Errorf("inspect Prompt Pane executable: %w", err)
	}
	if err := validateReplacement(target, sourceHash); err != nil {
		return "", err
	}
	if samePath(target, executable) {
		if err := writeState(state{Path: target, SHA256: sourceHash}); err != nil {
			return "", err
		}
		return target, nil
	}

	source, err := os.Open(executable)
	if err != nil {
		return "", fmt.Errorf("open Prompt Pane executable: %w", err)
	}
	defer source.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), "codex.pp-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create codex.pp temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := io.Copy(temporary, source); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("copy codex.pp executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close codex.pp executable: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o700); err != nil {
		return "", fmt.Errorf("prepare codex.pp executable: %w", err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("replace codex.pp executable: %w", err)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return "", fmt.Errorf("activate codex.pp executable: %w", err)
	}
	if err := writeState(state{Path: target, SHA256: sourceHash}); err != nil {
		_ = os.Remove(target)
		return "", err
	}
	return target, nil
}

func Preflight(codexPath, executable string) error {
	target, err := Target(codexPath)
	if err != nil {
		return err
	}
	sourceHash, err := fileHash(executable)
	if err != nil {
		return fmt.Errorf("inspect Prompt Pane executable: %w", err)
	}
	return validateReplacement(target, sourceHash)
}

func PreflightInstallAccess(codexPath string) error {
	target, err := Target(codexPath)
	if err != nil {
		return err
	}
	if err := paths.ProbeWritableDirectory(filepath.Dir(target)); err != nil {
		return fmt.Errorf("codex.pp target directory is not writable: %w", err)
	}
	root, err := paths.Home()
	if err != nil {
		return err
	}
	if err := paths.ProbeWritableDirectory(root); err != nil {
		return fmt.Errorf("Prompt Pane data directory is not writable: %w", err)
	}
	return nil
}

func Remove(codexPath string) (bool, error) {
	target, present, err := ownedInstallation(codexPath)
	if err != nil || !present {
		return false, err
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove codex.pp executable: %w", err)
	}
	statePath, err := statePath()
	if err != nil {
		return false, err
	}
	if err := os.Remove(statePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("remove codex.pp ownership state: %w", err)
	}
	return true, nil
}

func PreflightRemove(codexPath string) error {
	_, _, err := ownedInstallation(codexPath)
	return err
}

func ownedInstallation(codexPath string) (string, bool, error) {
	target, err := Target(codexPath)
	if err != nil {
		return "", false, err
	}
	installed, err := readState()
	if errors.Is(err, os.ErrNotExist) {
		return target, false, nil
	}
	if err != nil {
		return target, false, err
	}
	if !samePath(installed.Path, target) || !strings.EqualFold(filepath.Base(installed.Path), AliasName) {
		return target, false, fmt.Errorf("refusing to remove an unexpected codex.pp path")
	}
	digest, err := fileHash(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return target, false, fmt.Errorf("inspect codex.pp executable: %w", err)
	}
	if err == nil && digest != installed.SHA256 {
		return target, false, fmt.Errorf("codex.pp was modified after installation; refusing to remove it")
	}
	return target, true, nil
}

func validateReplacement(target, sourceHash string) error {
	targetHash, targetErr := fileHash(target)
	if errors.Is(targetErr, os.ErrNotExist) {
		return nil
	}
	if targetErr != nil {
		return fmt.Errorf("inspect existing codex.pp executable: %w", targetErr)
	}
	if targetHash == sourceHash {
		return nil
	}
	installed, stateErr := readState()
	if stateErr == nil && samePath(installed.Path, target) && installed.SHA256 == targetHash {
		return nil
	}
	return fmt.Errorf("%s already exists and is not managed by Prompt Pane", target)
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func readState() (state, error) {
	var installed state
	path, err := statePath()
	if err != nil {
		return installed, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return installed, err
	}
	if err := json.Unmarshal(data, &installed); err != nil {
		return installed, fmt.Errorf("decode codex.pp ownership state: %w", err)
	}
	if installed.Path == "" || len(installed.SHA256) != sha256.Size*2 {
		return installed, fmt.Errorf("codex.pp ownership state is incomplete")
	}
	return installed, nil
}

func writeState(installed state) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create Prompt Pane data directory: %w", err)
	}
	data, err := json.MarshalIndent(installed, "", "  ")
	if err != nil {
		return fmt.Errorf("encode codex.pp ownership state: %w", err)
	}
	data = append(data, '\n')
	temporary, err := os.CreateTemp(filepath.Dir(path), ".codex-launcher-*.tmp")
	if err != nil {
		return fmt.Errorf("create codex.pp ownership state: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write codex.pp ownership state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close codex.pp ownership state: %w", err)
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return fmt.Errorf("prepare codex.pp ownership state: %w", err)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace codex.pp ownership state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("activate codex.pp ownership state: %w", err)
	}
	return nil
}

func statePath() (string, error) {
	root, err := paths.Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, stateName), nil
}

func samePath(left, right string) bool {
	return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
}
