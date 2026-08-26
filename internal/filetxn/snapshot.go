package filetxn

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Snapshot struct {
	files []fileSnapshot
}

type fileSnapshot struct {
	path    string
	data    []byte
	mode    os.FileMode
	present bool
}

func Capture(paths ...string) (*Snapshot, error) {
	snapshot := &Snapshot{files: make([]fileSnapshot, 0, len(paths))}
	for _, path := range paths {
		file, err := captureFile(path)
		if err != nil {
			return nil, err
		}
		snapshot.files = append(snapshot.files, file)
	}
	return snapshot, nil
}

func (s *Snapshot) Restore() error {
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
	matched, err := restoredFileMatches(file)
	if err != nil {
		return err
	}
	if matched {
		return nil
	}
	if !file.present {
		if err := os.Remove(file.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove newly installed file %s: %w", file.path, err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(file.path), 0o700); err != nil {
		return fmt.Errorf("create rollback directory for %s: %w", file.path, err)
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
	if err := Replace(temporaryPath, file.path); err != nil {
		return fmt.Errorf("activate rollback file %s: %w", file.path, err)
	}
	return nil
}

func restoredFileMatches(want fileSnapshot) (bool, error) {
	got, err := captureFile(want.path)
	if err != nil {
		return false, err
	}
	return got.present == want.present && bytes.Equal(got.data, want.data), nil
}

func verifyRestoredFile(want fileSnapshot) error {
	matched, err := restoredFileMatches(want)
	if err != nil {
		return err
	}
	if !matched {
		return fmt.Errorf("verify restored installation file %s", want.path)
	}
	return nil
}
