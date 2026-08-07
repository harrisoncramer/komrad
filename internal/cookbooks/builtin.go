package cookbooks

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// builtinRoot is the embedded directory the shipped cookbooks are read out of. The all:
// prefix carries a manifest's action scripts through even when one starts with an underscore.
const builtinRoot = "builtin"

// markerName is the file an install leaves behind to say the directory is weave's, and is
// a dotfile so Discover passes over it rather than offering it as a cookbook.
const markerName = ".weave-builtins"

// ErrDirNotAbsolute is what installing into a relative path is refused with.
var ErrDirNotAbsolute = errors.New("the directory is not an absolute path")

// ErrDirNotOurs is what installing into a directory weave did not write is refused with.
var ErrDirNotOurs = errors.New("the directory holds files weave did not write")

//go:embed all:builtin
var builtinFS embed.FS

// InstallBuiltins writes the cookbooks shipped with this build into dir, mirroring how
// internal/actions installs its own builtins: emptied and rewritten on every call so a
// cookbook an older build shipped does not stay on offer.
//
// dir has to be absolute, since a relative one names a different directory every time the
// caller happens to be started somewhere new, and it has to be either empty or carrying the
// marker a previous install left, for the same reason internal/actions requires it.
func InstallBuiltins(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("failed to install the built-in cookbooks into %s: %w", dir, ErrDirNotAbsolute)
	}

	if err := clearOwned(dir); err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create the built-in cookbooks directory %s: %w", dir, err)
	}

	marker := filepath.Join(dir, markerName)
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", marker, err)
	}

	entries, err := builtinFS.ReadDir(builtinRoot)
	if err != nil {
		return fmt.Errorf("failed to read the built-in cookbooks: %w", err)
	}

	for _, entry := range entries {
		if err := installCookbookDir(dir, entry.Name()); err != nil {
			return err
		}
	}

	return nil
}

// installCookbookDir copies one embedded cookbook's manifest and action files into dir/name.
func installCookbookDir(dir, name string) error {
	src := path.Join(builtinRoot, name)

	files, err := builtinFS.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read the built-in cookbook %s: %w", name, err)
	}

	dest := filepath.Join(dir, name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dest, err)
	}

	for _, file := range files {
		content, err := builtinFS.ReadFile(path.Join(src, file.Name()))
		if err != nil {
			return fmt.Errorf("failed to read the built-in cookbook file %s/%s: %w", name, file.Name(), err)
		}

		written := filepath.Join(dest, file.Name())
		if err := os.WriteFile(written, content, 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", written, err)
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
		return fmt.Errorf("failed to read the built-in cookbooks directory %s: %w", dir, err)
	}

	if len(entries) > 0 && !hasMarker(entries) {
		return fmt.Errorf("failed to clear the built-in cookbooks directory %s: %w", dir, ErrDirNotOurs)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to clear the built-in cookbooks directory %s: %w", dir, err)
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
