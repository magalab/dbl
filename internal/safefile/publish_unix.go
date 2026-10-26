//go:build !windows

package safefile

import (
	"fmt"
	"os"
	"path/filepath"
)

func publishNewFile(temporary, destination string) error {
	if err := os.Link(temporary, destination); err != nil {
		return fmt.Errorf("publish export without replacing an existing file: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return &PublishedError{cause: fmt.Errorf("remove export staging link: %w", err)}
	}
	directory, err := os.Open(filepath.Dir(destination))
	if err != nil {
		return &PublishedError{cause: fmt.Errorf("open export directory for sync: %w", err)}
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return &PublishedError{cause: fmt.Errorf("sync export directory: %w", err)}
	}
	return nil
}
