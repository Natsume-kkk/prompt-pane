//go:build windows

package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Natsume-kkk/prompt-pane/internal/paths"
	"golang.org/x/sys/windows"
)

const (
	updateGateOffset uint32 = iota
	workspaceActivityOffset
)

type fileCoordinationLock struct {
	file       *os.File
	overlapped windows.Overlapped
}

func AcquireUpdateGate(ctx context.Context) (CoordinationLock, error) {
	return acquireFileLock(ctx, updateGateOffset, true, true)
}

func AcquireWorkspaceActivity() (CoordinationLock, error) {
	return acquireFileLock(context.Background(), workspaceActivityOffset, false, false)
}

func AcquireExclusiveWorkspaceActivity() (CoordinationLock, error) {
	lock, err := acquireFileLock(context.Background(), workspaceActivityOffset, true, false)
	if errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return nil, ErrWorkspacesActive
	}
	return lock, err
}

func acquireFileLock(ctx context.Context, offset uint32, exclusive, wait bool) (CoordinationLock, error) {
	file, err := openCoordinationFile()
	if err != nil {
		return nil, err
	}
	overlapped := windows.Overlapped{Offset: offset}
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	for {
		err = windows.LockFileEx(windows.Handle(file.Fd()), flags, 0, 1, 0, &overlapped)
		if err == nil {
			return &fileCoordinationLock{file: file, overlapped: overlapped}, nil
		}
		if !wait || !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("wait for Prompt Pane update gate: %w", ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func openCoordinationFile() (*os.File, error) {
	home, err := paths.Home()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, fmt.Errorf("create Prompt Pane data directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(home, "coordination.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open Prompt Pane coordination lock: %w", err)
	}
	return file, nil
}

func (l *fileCoordinationLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	err := windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &l.overlapped)
	closeErr := l.file.Close()
	l.file = nil
	if err != nil {
		return err
	}
	return closeErr
}
