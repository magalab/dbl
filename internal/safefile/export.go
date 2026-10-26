// Package safefile writes user-requested exports without exposing partial
// files at their final destination.
package safefile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magalab/dbl/internal/privatefs"
)

var ErrFinished = errors.New("staged export is already finished")

// PublishedError means the complete file is already visible at the destination,
// but a durability step after publication failed.
type PublishedError struct{ cause error }

func (err *PublishedError) Error() string {
	return fmt.Sprintf("export published but durability is uncertain: %v", err.cause)
}
func (err *PublishedError) Unwrap() error { return err.cause }

func IsPublished(err error) bool {
	var published *PublishedError
	return errors.As(err, &published)
}

// StagedExport keeps incomplete output under a private temporary name. Commit
// syncs and closes the file before publishing it without replacing a target.
type StagedExport struct {
	file        *os.File
	temporary   string
	destination string
	finished    bool
}

func CreateExport(destination string) (*StagedExport, error) {
	destination = strings.TrimSpace(destination)
	if destination == "" {
		return nil, errors.New("export destination is empty")
	}
	absDestination, err := filepath.Abs(destination)
	if err != nil {
		return nil, fmt.Errorf("resolve export destination: %w", err)
	}
	if _, err := os.Lstat(absDestination); err == nil {
		return nil, fmt.Errorf("export destination already exists: %s", absDestination)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect export destination: %w", err)
	}
	directory := filepath.Dir(absDestination)
	file, err := os.CreateTemp(directory, ".dbl-export-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create private export staging file: %w", err)
	}
	if err := privatefs.Restrict(file.Name(), false); err != nil {
		_ = file.Close()
		_ = os.Remove(file.Name())
		return nil, fmt.Errorf("restrict export staging file permissions: %w", err)
	}
	return &StagedExport{file: file, temporary: file.Name(), destination: absDestination}, nil
}

func (staged *StagedExport) File() *os.File {
	if staged == nil || staged.finished {
		return nil
	}
	return staged.file
}

func (staged *StagedExport) Commit() error {
	if staged == nil || staged.finished || staged.file == nil {
		return ErrFinished
	}
	if err := staged.file.Sync(); err != nil {
		return fmt.Errorf("sync export staging file: %w", err)
	}
	if err := staged.file.Close(); err != nil {
		staged.file = nil
		return fmt.Errorf("close export staging file: %w", err)
	}
	staged.file = nil
	err := publishNewFile(staged.temporary, staged.destination)
	if IsPublished(err) {
		staged.finished = true
		return err
	}
	if err != nil {
		return err
	}
	staged.finished = true
	return nil
}

func (staged *StagedExport) Abort() error {
	if staged == nil || staged.finished {
		return nil
	}
	staged.finished = true
	var closeErr error
	if staged.file != nil {
		closeErr = staged.file.Close()
		staged.file = nil
	}
	removeErr := os.Remove(staged.temporary)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	return errors.Join(closeErr, removeErr)
}
