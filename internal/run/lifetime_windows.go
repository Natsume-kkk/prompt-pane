//go:build windows

package run

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	lifetimeOnce sync.Once
	lifetimeErr  error
)

func EnsureProcessLifetime() error {
	lifetimeOnce.Do(func() {
		job, err := newKillOnCloseJob()
		if err != nil {
			lifetimeErr = err
			return
		}
		if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
			_ = windows.CloseHandle(job)
			lifetimeErr = fmt.Errorf("join Windows process lifetime: %w", err)
			return
		}
		// This process intentionally owns the only handle until exit. Closing it
		// earlier would terminate the owner together with all child processes.
	})
	return lifetimeErr
}

func newKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("create Windows process lifetime: %w", err)
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
		return 0, fmt.Errorf("configure Windows process lifetime: %w", err)
	}
	return job, nil
}
