package actions

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// builtinRoot is the embedded directory the shipped actions are read out of. The all:
// prefix is what carries the underscored names, which embed leaves out otherwise.
const builtinRoot = "builtin"

// markerName is the file an install leaves behind to say the directory is weave's, and is
// a dotfile so Discover passes over it rather than offering it as an action.
const markerName = ".weave-builtins"

// ErrDirNotAbsolute is what installing into a relative path is refused with.
var ErrDirNotAbsolute = errors.New("the directory is not an absolute path")

// ErrDirNotOurs is what installing into a directory weave did not write is refused with.
var ErrDirNotOurs = errors.New("the directory holds files weave did not write")

//go:embed all:builtin
var builtinFS embed.FS

// InstallBuiltins writes the actions shipped with this build into dir. They land on disk
// because running an action is an exec of a path, and the directory is emptied first so a
// script an older build shipped does not stay on offer.
//
// Emptying it is a recursive delete of a path that comes from the environment, so dir has
// to be one weave owns. It has to be absolute, since a relative one names a different
// directory every time the service is started somewhere new. It also has to be either
// empty or carrying the marker a previous install left.
func InstallBuiltins(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("failed to install the built-in actions into %s: %w", dir, ErrDirNotAbsolute)
	}

	if err := clearOwned(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create the built-in actions directory %s: %w", dir, err)
	}

	// The marker goes down before the scripts, so an install that dies partway through still
	// leaves the directory claimed for the next one to clear.
	marker := filepath.Join(dir, markerName)
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", marker, err)
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

// clearOwned empties dir, and refuses one holding anything a previous install did not put
// there. A directory that is not there yet, or that is empty, is weave's to take.
func clearOwned(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to read the built-in actions directory %s: %w", dir, err)
	}

	if len(entries) > 0 && !hasMarker(entries) {
		return fmt.Errorf("failed to clear the built-in actions directory %s: %w", dir, ErrDirNotOurs)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to clear the built-in actions directory %s: %w", dir, err)
	}

	return nil
}

// hasMarker says whether a previous install claimed the directory these entries came from.
func hasMarker(entries []fs.DirEntry) bool {
	for _, entry := range entries {
		if entry.Name() == markerName {
			return true
		}
	}

	return false
}
