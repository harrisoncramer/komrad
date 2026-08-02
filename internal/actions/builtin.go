package actions

import (
	"embed"
	"fmt"
	"os"
	"path"
	"path/filepath"
)

// builtinRoot is the embedded directory the shipped actions are read out of. The all:
// prefix is what carries the underscored names, which embed leaves out otherwise.
const builtinRoot = "builtin"

//go:embed all:builtin
var builtinFS embed.FS

// InstallBuiltins writes the actions shipped with this build into dir. They land on disk
// because running an action is an exec of a path, and the directory is emptied first so a
// script an older build shipped does not stay on offer.
func InstallBuiltins(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to clear the built-in actions directory %s: %w", dir, err)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create the built-in actions directory %s: %w", dir, err)
	}

	entries, err := builtinFS.ReadDir(builtinRoot)
	if err != nil {
		return fmt.Errorf("failed to read the built-in actions: %w", err)
	}

	for _, entry := range entries {
		content, err := builtinFS.ReadFile(path.Join(builtinRoot, entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read the built-in action %s: %w", entry.Name(), err)
		}

		written := filepath.Join(dir, entry.Name())
		if err := os.WriteFile(written, content, 0o755); err != nil {
			return fmt.Errorf("failed to write the built-in action %s: %w", written, err)
		}
	}

	return nil
}
