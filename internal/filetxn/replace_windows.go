//go:build windows

package filetxn

import "golang.org/x/sys/windows"

func Replace(source, destination string) error {
	return windows.Rename(source, destination)
}
