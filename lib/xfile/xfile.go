// Package xfile holds small file helpers.
package xfile

import (
	"fmt"
	"os"
	"path/filepath"
)

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

	if err := os.Rename(temporary.Name(), path); err != nil {
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}

	return nil
}
