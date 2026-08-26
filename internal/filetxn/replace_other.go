//go:build !windows

package filetxn

import "os"

func Replace(source, destination string) error {
	return os.Rename(source, destination)
}
