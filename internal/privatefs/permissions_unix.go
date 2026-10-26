//go:build !windows

package privatefs

import "os"

// Restrict grants access to the current user on POSIX filesystems.
func Restrict(path string, directory bool) error {
	mode := os.FileMode(0o600)
	if directory {
		mode = 0o700
	}
	return os.Chmod(path, mode)
}
