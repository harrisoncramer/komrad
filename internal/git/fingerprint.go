package git

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// HiddenPaths is the paths an index tells git not to read, as of the index it was read from.
type HiddenPaths struct {
	// Index is the stamp of the index the paths were read from.
	Index string
	Paths []string
}

// Fingerprint stands for a worktree as git status sees it.
type Fingerprint struct {
	Key    string
	Hidden HiddenPaths
}

// ReadFingerprintParams names the worktree to fingerprint and what the last fingerprint of it read.
type ReadFingerprintParams struct {
	Root   string
	GitDir string
	// Hidden is the hidden paths the last fingerprint read, read again only when the index has
	// changed since.
	Hidden *HiddenPaths
}

// ReadFingerprint hashes the index's stamp, what git status lists, and the stamp of each listed
// and hidden path, without taking the index lock.
func (c *Client) ReadFingerprint(ctx context.Context, params ReadFingerprintParams) (*Fingerprint, error) {
	index, err := stampPath(filepath.Join(params.GitDir, "index"))
	if err != nil {
		return nil, fmt.Errorf("failed to read the index of %q: %w", params.Root, err)
	}

	hidden := params.Hidden
	if hidden == nil || hidden.Index != index {
		paths, err := hiddenPaths(ctx, params.Root)
		if err != nil {
			return nil, err
		}
		hidden = &HiddenPaths{Index: index, Paths: paths}
	}

	status, err := runBytes(ctx, params.Root, "--no-optional-locks", "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("failed to read what %q has changed: %w", params.Root, err)
	}

	hash := sha256.New()
	hash.Write([]byte(index))
	hash.Write([]byte{0})
	hash.Write(status)

	paths := slices.Clone(hidden.Paths)
	for entry := range bytes.SplitSeq(status, []byte{0}) {
		if len(entry) > 3 {
			paths = append(paths, string(entry[3:]))
		}
	}

	for _, path := range paths {
		stamp, err := stampPath(filepath.Join(params.Root, filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("failed to read %q in %q: %w", path, params.Root, err)
		}
		hash.Write([]byte(path + "\x00" + stamp + "\x00"))
	}

	return &Fingerprint{Key: hex.EncodeToString(hash.Sum(nil)), Hidden: *hidden}, nil
}

// stampPath is a path's size, mode and modification time, or "absent" when nothing is there.
func stampPath(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d:%o:%d", info.Size(), info.Mode(), info.ModTime().UnixNano()), nil
}
