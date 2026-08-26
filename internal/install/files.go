package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Natsume-kkk/prompt-pane/internal/filetxn"
	"github.com/Natsume-kkk/prompt-pane/internal/paths"
)

func PreflightAccess() error {
	root, err := paths.Home()
	if err != nil {
		return err
	}
	for _, target := range []struct {
		description string
		directory   string
	}{
		{description: "Prompt Pane data directory", directory: root},
		{description: "Prompt Pane launcher directory", directory: filepath.Join(root, "bin")},
		{description: "Prompt Pane versions directory", directory: filepath.Join(root, "versions")},
	} {
		description, directory := target.description, target.directory
		if err := paths.ProbeWritableDirectory(directory); err != nil {
			return fmt.Errorf("%s is not writable: %w", description, err)
		}
	}
	return nil
}

func Stage(executable, version string) (Release, bool, error) {
	digest, err := HashFile(executable)
	if err != nil {
		return Release{}, false, fmt.Errorf("inspect Prompt Pane executable: %w", err)
	}
	release := Release{Generation: digest, Version: version}
	if err := validateRelease(release); err != nil {
		return Release{}, false, err
	}
	destination, err := RuntimePath(release)
	if err != nil {
		return Release{}, false, err
	}
	if err := verifyDigest(destination, digest); err == nil {
		return release, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Release{}, false, fmt.Errorf("verify staged Prompt Pane version: %w", err)
	}
	root, err := VersionsRoot()
	if err != nil {
		return Release{}, false, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return Release{}, false, fmt.Errorf("create Prompt Pane versions directory: %w", err)
	}
	temporaryDirectory, err := os.MkdirTemp(root, ".stage-*")
	if err != nil {
		return Release{}, false, fmt.Errorf("stage Prompt Pane version: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	temporaryExecutable := filepath.Join(temporaryDirectory, executableName)
	if err := copyFile(executable, temporaryExecutable); err != nil {
		return Release{}, false, err
	}
	if err := verifyDigest(temporaryExecutable, digest); err != nil {
		return Release{}, false, fmt.Errorf("verify staged Prompt Pane version: %w", err)
	}
	if err := os.Rename(temporaryDirectory, filepath.Dir(destination)); err != nil {
		if verifyErr := verifyDigest(destination, digest); verifyErr == nil {
			return release, false, nil
		}
		return Release{}, false, fmt.Errorf("activate staged Prompt Pane version: %w", err)
	}
	return release, true, nil
}

func VerifyRelease(release Release) error {
	path, err := RuntimePath(release)
	if err != nil {
		return err
	}
	if err := verifyDigest(path, release.Generation); err != nil {
		return fmt.Errorf("verify Prompt Pane %s: %w", ReleaseLabel(release), err)
	}
	return nil
}

func LauncherReady(state State) (bool, error) {
	if err := Validate(state); err != nil {
		return false, err
	}
	path, err := LauncherPath()
	if err != nil {
		return false, err
	}
	digest, err := HashFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect Prompt Pane launcher: %w", err)
	}
	return digest == state.LauncherSHA256, nil
}

func InstallLauncher(source string, existing *State) (string, error) {
	target, err := LauncherPath()
	if err != nil {
		return "", err
	}
	sourceDigest, err := HashFile(source)
	if err != nil {
		return "", fmt.Errorf("inspect Prompt Pane launcher source: %w", err)
	}
	targetDigest, targetErr := HashFile(target)
	if targetErr == nil {
		if existing != nil && targetDigest != existing.LauncherSHA256 {
			return "", fmt.Errorf("Prompt Pane launcher was modified after installation; refusing to replace it")
		}
		if targetDigest == sourceDigest || existing != nil {
			return targetDigest, nil
		}
	} else if !errors.Is(targetErr, os.ErrNotExist) {
		return "", fmt.Errorf("inspect Prompt Pane launcher: %w", targetErr)
	}
	if err := copyFileAtomic(source, target); err != nil {
		return "", fmt.Errorf("install Prompt Pane launcher: %w", err)
	}
	return sourceDigest, nil
}

func CleanupUnreferenced(state State) []error {
	if err := Validate(state); err != nil {
		return []error{err}
	}
	keep := map[string]bool{state.Current.Generation: true}
	if state.Pending != nil {
		keep[state.Pending.Generation] = true
	}
	if state.Previous != nil {
		keep[state.Previous.Generation] = true
	}
	root, err := VersionsRoot()
	if err != nil {
		return []error{err}
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []error{fmt.Errorf("inspect Prompt Pane versions: %w", err)}
	}
	var cleanupErrors []error
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || !validGeneration(name) || keep[name] {
			continue
		}
		target := filepath.Join(root, name)
		if err := os.RemoveAll(target); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("remove unused Prompt Pane version %s: %w", name[:8], err))
		}
	}
	return cleanupErrors
}

func HashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file is not regular")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifyDigest(path, expected string) error {
	digest, err := HashFile(path)
	if err != nil {
		return err
	}
	if digest != expected {
		return fmt.Errorf("content digest does not match its version directory")
	}
	return nil
}

func copyFileAtomic(source, target string) error {
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	targetPath, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	if strings.EqualFold(filepath.Clean(sourcePath), filepath.Clean(targetPath)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(targetPath), ".launcher-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := copyToOpenFile(sourcePath, temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporaryPath, 0o700); err != nil {
		return err
	}
	return filetxn.Replace(temporaryPath, targetPath)
}

func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return fmt.Errorf("create Prompt Pane version directory: %w", err)
	}
	destination, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		return fmt.Errorf("create staged Prompt Pane executable: %w", err)
	}
	if err := copyToOpenFile(source, destination); err != nil {
		_ = destination.Close()
		return fmt.Errorf("copy Prompt Pane executable: %w", err)
	}
	if err := destination.Close(); err != nil {
		return fmt.Errorf("close staged Prompt Pane executable: %w", err)
	}
	return nil
}

func copyToOpenFile(source string, destination *os.File) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	_, err = io.Copy(destination, input)
	return err
}
