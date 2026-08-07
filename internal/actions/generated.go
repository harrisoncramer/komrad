package actions

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteGenerated writes an executable script into dir under name, creating the directory
// if it is not there yet. It always overwrites, since a generated script is rewritten in
// full whenever the config that produced it changes, rather than patched in place.
func WriteGenerated(dir, name, content string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create the actions directory %s: %w", dir, err)
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		return fmt.Errorf("failed to write the generated action %s: %w", path, err)
	}

	return nil
}

// RemoveGenerated deletes a generated script from dir if it is there. A file that is
// already gone is not an error, since the caller is undoing a generation step it cannot be
// sure ran.
func RemoveGenerated(dir, name string) error {
	path := filepath.Join(dir, name)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove the generated action %s: %w", path, err)
	}

	return nil
}
