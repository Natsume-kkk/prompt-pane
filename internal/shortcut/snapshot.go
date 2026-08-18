package shortcut

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type InstallationSnapshot struct {
	files []fileSnapshot
}

type fileSnapshot struct {
	path    string
	data    []byte
	mode    os.FileMode
	present bool
}

func CaptureInstallation(codexPath string) (*InstallationSnapshot, error) {
	target, err := Target(codexPath)
	if err != nil {
		return nil, err
	}
	stateFile, err := statePath()
	if err != nil {
		return nil, err
	}
	snapshot := &InstallationSnapshot{}
	for _, path := range []string{target, stateFile} {
		file, err := captureFile(path)
		if err != nil {
			return nil, err
		}
		snapshot.files = append(snapshot.files, file)
	}
	return snapshot, nil
}

func (s *InstallationSnapshot) Restore() error {
	if s == nil {
		return nil
	}
	var restoreErr error
	for _, file := range s.files {
		if err := restoreFile(file); err != nil {
			restoreErr = errors.Join(restoreErr, err)
		}
	}
	if restoreErr == nil {
		for _, file := range s.files {
			if err := verifyRestoredFile(file); err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
	}
	return restoreErr
}

func captureFile(path string) (fileSnapshot, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fileSnapshot{path: path}, nil
	}
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("inspect installation file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fileSnapshot{}, fmt.Errorf("installation file %s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fileSnapshot{}, fmt.Errorf("snapshot installation file %s: %w", path, err)
	}
	return fileSnapshot{path: path, data: data, mode: info.Mode(), present: true}, nil
}

func restoreFile(file fileSnapshot) error {
	if !file.present {
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove newly installed file %s: %w", file.path, err)
		}
		return nil
	}
	temporary, err := os.CreateTemp(filepath.Dir(file.path), ".prompt-pane-restore-*.tmp")
	if err != nil {
		return fmt.Errorf("create rollback file for %s: %w", file.path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(file.data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write rollback file for %s: %w", file.path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close rollback file for %s: %w", file.path, err)
	}
	if err := os.Chmod(temporaryPath, file.mode.Perm()); err != nil {
		return fmt.Errorf("prepare rollback file for %s: %w", file.path, err)
	}
	if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace installation file %s during rollback: %w", file.path, err)
	}
	if err := os.Rename(temporaryPath, file.path); err != nil {
		return fmt.Errorf("activate rollback file %s: %w", file.path, err)
	}
	return nil
}

func verifyRestoredFile(want fileSnapshot) error {
	got, err := captureFile(want.path)
	if err != nil {
		return err
	}
	if got.present != want.present || !bytes.Equal(got.data, want.data) {
		return fmt.Errorf("verify restored installation file %s", want.path)
	}
	return nil
}
