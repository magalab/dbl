//go:build windows

package safefile

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func publishNewFile(temporary, destination string) error {
	from, err := windows.UTF16PtrFromString(filepath.Clean(temporary))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Clean(destination))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
}
