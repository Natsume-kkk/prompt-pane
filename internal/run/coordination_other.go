//go:build !windows

package run

import "context"

type noopCoordinationLock struct{}

func AcquireUpdateGate(context.Context) (CoordinationLock, error) {
	return noopCoordinationLock{}, nil
}

func AcquireWorkspaceActivity() (CoordinationLock, error) {
	return noopCoordinationLock{}, nil
}

func AcquireExclusiveWorkspaceActivity() (CoordinationLock, error) {
	return noopCoordinationLock{}, nil
}

func (noopCoordinationLock) Close() error {
	return nil
}
