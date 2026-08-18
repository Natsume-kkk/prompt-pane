//go:build windows

package zellij

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func containWorkspaceProcessTree() error {
	job, err := newKillOnCloseJob()
	if err != nil {
		return err
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		_ = windows.CloseHandle(job)
		return fmt.Errorf("join Windows Job Object: %w", err)
	}
	// The launcher intentionally owns this handle until process exit. Closing it
	// earlier would terminate the launcher together with the workspace children.
	return nil
}

func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create Windows Job Object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		return 0, fmt.Errorf("configure Windows Job Object: %w", err)
	}
	return job, nil
}
