//go:build windows

package preferences

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

func replaceSettingsFile(tempPath, settingsPath string) error {
	from, err := windows.UTF16PtrFromString(filepath.Clean(tempPath))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Clean(settingsPath))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
