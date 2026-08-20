//go:build !windows

package shortcut

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
