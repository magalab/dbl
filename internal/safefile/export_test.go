package safefile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStagedExportPublishesCompleteFileWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "result.csv")
	staged, err := CreateExport(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staged.File().WriteString("complete,csv\n1,2\n"); err != nil {
		t.Fatal(err)
	}
	if err := staged.Commit(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "complete,csv\n1,2\n" {
		t.Fatalf("published contents = %q", contents)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(destination)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("published permissions = %04o, want 0600", got)
		}
	}
	if staged.File() != nil {
		t.Fatal("finished export still exposes a file handle")
	}
	if err := staged.Commit(); !errors.Is(err, ErrFinished) {
		t.Fatalf("second commit error = %v, want ErrFinished", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".dbl-export-") {
			t.Errorf("staging file remains after commit: %s", entry.Name())
		}
	}
}

func TestStagedExportAbortRemovesPartialOutput(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "result.json")
	staged, err := CreateExport(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staged.File().WriteString(`{"partial":`); err != nil {
		t.Fatal(err)
	}
	if err := staged.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination stat error = %v, want not exist", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory contains partial export artifacts: %#v", entries)
	}
}

func TestStagedExportCommitDoesNotReplaceRacingDestination(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "result.txt")
	staged, err := CreateExport(destination)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := staged.File().WriteString("new"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := staged.Commit(); err == nil || IsPublished(err) {
		t.Fatalf("commit error = %v, want unpublished collision", err)
	}
	if err := staged.Abort(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "existing" {
		t.Fatalf("existing destination changed to %q", contents)
	}
}
