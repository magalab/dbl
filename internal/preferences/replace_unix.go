//go:build !windows

package preferences

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceSettingsFile(tempPath, settingsPath string) error {
	if err := os.Rename(tempPath, settingsPath); err != nil {
		return err
	}
	directory, err := os.OpenFile(filepath.Dir(settingsPath), os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open preferences directory after replacement: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync preferences directory after replacement: %w", err)
	}
	return nil
}
