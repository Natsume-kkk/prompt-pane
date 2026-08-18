//go:build !windows

package run

func EnsureProcessLifetime() error {
	return nil
}
