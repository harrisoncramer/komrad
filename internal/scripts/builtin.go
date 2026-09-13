package scripts

import (
	"weave/internal/fail"

	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

const builtinRoot = "builtin"

const markerName = ".weave-builtins"

var ErrDirNotAbsolute = fail.Internal("the directory is not an absolute path")

var ErrDirNotOurs = fail.Internal("the directory holds files weave did not write")

//go:embed all:builtin
var builtinFS embed.FS

// InstallBuiltins writes the scripts shipped with this build into dir, emptying it first so a
// script an older build shipped does not stay on offer.
func InstallBuiltins(dir string) error {
	// A relative path names a different directory every time the service starts somewhere new.
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("failed to install the built-in scripts into %s: %w", dir, ErrDirNotAbsolute)
	}

	// Emptying is a recursive delete, so the directory has to be empty or carry the marker a
	// previous install left.
	if err := clearOwned(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create the built-in scripts directory %s: %w", dir, err)
	}

	marker := filepath.Join(dir, markerName)
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", marker, err)
	}

	entries, err := builtinFS.ReadDir(builtinRoot)
	if err != nil {
		return fmt.Errorf("failed to read the built-in scripts: %w", err)
	}

	for _, entry := range entries {
		content, err := builtinFS.ReadFile(path.Join(builtinRoot, entry.Name()))
		if err != nil {
			return fmt.Errorf("failed to read the built-in script %s: %w", entry.Name(), err)
		}

		written := filepath.Join(dir, entry.Name())
		if err := os.WriteFile(written, content, 0o755); err != nil {
			return fmt.Errorf("failed to write the built-in script %s: %w", written, err)
		}
	}

	return nil
}

func clearOwned(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to read the built-in scripts directory %s: %w", dir, err)
	}

	if len(entries) > 0 && !hasMarker(entries) {
		return fmt.Errorf("failed to clear the built-in scripts directory %s: %w", dir, ErrDirNotOurs)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to clear the built-in scripts directory %s: %w", dir, err)
	}

	return nil
}

func hasMarker(entries []fs.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() == markerName {
			return true
		}
	}

	return false
}
