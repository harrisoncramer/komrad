// Package xfile holds small file helpers.
package xfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Explainer: writing over a file in place leaves a window where a reader, or a process that
// crashes part-way, sees half the old file and half the new one. A shell running a script
// reads it a line at a time and would pick up the tail of a file it did not start, and a
// token or config read mid-write would fail to parse. WriteAtomic writes a new file beside
// the target, sets its mode, syncs it to disk and renames it over the target. A rename
// within one directory is atomic, so a reader sees the whole old file or the whole new one,
// and the mode is the one asked for rather than whatever the target was first created with.

// WriteAtomic replaces the file at path with content written under mode.
func WriteAtomic(path string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("failed to stage %s: %w", path, err)
	}

	defer func() { _ = os.Remove(temporary.Name()) }() //nolint:errcheck // gone already on success

	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("failed to set the mode of %s: %w", path, err)
	}

	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()

		return fmt.Errorf("failed to sync %s: %w", path, err)
	}

	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	if err := os.Rename(temporary.Name(), path); err != nil { //nolint:forbidigo // the one rename the rule funnels every other write through
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}

	return nil
}
