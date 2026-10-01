package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"komrad/internal/fail"
)

// ErrIndexLocked says another git command holds the checkout's index.
var ErrIndexLocked = fail.Precondition("another git command is running in this worktree")

// AbortSequence undoes the sequence part-way through a checkout, putting the branch back where
// it stood before the sequence began.
func (c *Client) AbortSequence(ctx context.Context, dir string, sequence Sequence) error {
	verb := sequence.GetCommand()
	if verb == "" {
		return fmt.Errorf("there is no way to abort a sequence of kind %q", sequence)
	}

	gitDir, err := Run(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("failed to read the git directory of %q: %w", dir, err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "index.lock")); err == nil {
		return fmt.Errorf("%w: %s", ErrIndexLocked, dir)
	}

	if _, err := runReporting(ctx, dir, os.Environ(), verb, "--abort"); err != nil {
		return fmt.Errorf("failed to abort the %s in %q: %w", sequence, dir, err)
	}

	return nil
}

// ReadRebaseOnto is the commit a rebase part-way through a checkout is replaying onto, empty
// when no rebase is.
func (c *Client) ReadRebaseOnto(ctx context.Context, dir string) (string, error) {
	gitDir, err := Run(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", fmt.Errorf("failed to read the git directory of %q: %w", dir, err)
	}

	for _, backend := range []string{"rebase-merge", "rebase-apply"} {
		onto, err := os.ReadFile(filepath.Join(gitDir, backend, "onto"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to read what the rebase in %q is replaying onto: %w", dir, err)
		}

		return strings.TrimSpace(string(onto)), nil
	}

	return "", nil
}
