package controlplane

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"komrad/internal/forge"
	"komrad/lib/xfile"
)

// FileStore keeps each engine's GitHub login in a file of its own, mode 0600.
type FileStore struct {
	dir string
}

// NewFileStore builds a store in dir.
func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: dir}
}

// Get is the engine's login, or forge.ErrNoCredential.
func (f *FileStore) Get(_ context.Context, engine string) (forge.Secret, error) {
	raw, err := os.ReadFile(filepath.Join(f.dir, engine))
	if errors.Is(err, fs.ErrNotExist) {
		return forge.Secret{}, forge.ErrNoCredential.Saying("no GitHub login for engine %s", engine)
	}
	if err != nil {
		return forge.Secret{}, fmt.Errorf("failed to read the GitHub login for engine %s: %w", engine, err)
	}

	login := forge.NewSecret(string(raw))
	if login.IsZero() {
		return forge.Secret{}, forge.ErrNoCredential.Saying("the GitHub login for engine %s is empty", engine)
	}

	return login, nil
}

// Set puts the engine's login on disk where only komrad's user can read it.
func (f *FileStore) Set(_ context.Context, engine string, login forge.Secret) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return fmt.Errorf("failed to make the GitHub login directory: %w", err)
	}

	if err := xfile.WriteAtomic(filepath.Join(f.dir, engine), []byte(login.Expose()+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write the GitHub login for engine %s: %w", engine, err)
	}

	return nil
}
