package postgres

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/fail"
)

var errNotBundled = fail.Precondition("this build of komrad carries no Postgres, so install a release binary or build with mise run go:build")

// programDir is where the programs of one Postgres version live under dir.
func programDir(dir, version string) string {
	return filepath.Join(dir, version)
}

func program(dir, name string) string {
	return filepath.Join(programDir(dir, Version), "bin", name)
}

// unpack writes the bundled Postgres programs under dir, once for each version.
func unpack(dir string) error {
	target := programDir(dir, Version)
	if _, err := os.Stat(target); err == nil {
		return nil
	}

	if !Bundled() {
		return errNotBundled
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	staging, err := os.MkdirTemp(dir, ".unpack-")
	if err != nil {
		return fmt.Errorf("failed to create a directory to unpack Postgres into: %w", err)
	}
	defer os.RemoveAll(staging) //nolint:errcheck // gone once renamed, and a leftover is only space

	if err := extract(bytes.NewReader(bundle), staging); err != nil {
		return fmt.Errorf("failed to unpack Postgres %s: %w", Version, err)
	}

	if err := os.Rename(staging, target); err != nil { //nolint:forbidigo // a directory, which xfile.WriteAtomic does not move
		return fmt.Errorf("failed to move Postgres %s into %s: %w", Version, target, err)
	}

	return nil
}

// extract writes a gzipped tar into dest, dropping the one directory every entry sits under.
func extract(archive io.Reader, dest string) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("failed to read the archive as gzip: %w", err)
	}
	defer gz.Close() //nolint:errcheck // read only

	reader := tar.NewReader(gz)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read the next entry of the archive: %w", err)
		}

		name, err := entryPath(header.Name)
		if err != nil {
			return fmt.Errorf("failed to place %s: %w", header.Name, err)
		}
		if name == "" {
			continue
		}
		path := filepath.Join(dest, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("failed to create %s: %w", path, err)
			}
		case tar.TypeReg:
			if err := writeEntry(path, reader, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("failed to write %s: %w", path, err)
			}
		case tar.TypeSymlink:
			if strings.ContainsRune(header.Linkname, '/') {
				return fmt.Errorf("%s links outside its own directory, to %s", header.Name, header.Linkname)
			}
			if err := os.Symlink(header.Linkname, path); err != nil {
				return fmt.Errorf("failed to link %s to %s: %w", path, header.Linkname, err)
			}
		default:
			return fmt.Errorf("%s is neither a file, a directory nor a link", header.Name)
		}
	}
}

// entryPath is an entry's path below the archive's top directory, refusing one that climbs out.
func entryPath(name string) (string, error) {
	_, rest, _ := strings.Cut(filepath.ToSlash(name), "/")
	cleaned := filepath.Clean(rest)
	if cleaned == "." {
		return "", nil
	}
	if !filepath.IsLocal(cleaned) {
		return "", errors.New("it is not a path inside the archive")
	}

	return cleaned, nil
}

func writeEntry(path string, contents io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}

	if _, err := io.Copy(file, contents); err != nil { //nolint:gosec // the archive is the one embedded in this binary
		return errors.Join(fmt.Errorf("failed to copy %s out of the archive: %w", path, err), file.Close())
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", path, err)
	}

	return nil
}
