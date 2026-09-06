package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Materialise checks a commit out as a worktree of its own, detached so nothing can be pushed
// from it by accident.
func Materialise(ctx context.Context, dir, sha, at string) error {
	// A checkout already on the commit is left exactly as it is, since what the reviewer has
	// typed into it is the suggestion they are still writing.
	if head, err := Run(ctx, at, "rev-parse", "HEAD"); err == nil {
		if head == sha {
			return nil
		}

		if _, err := Run(ctx, at, "checkout", "--detach", "--force", sha); err == nil {
			return nil
		}
	}

	if err := Reap(ctx, dir, at); err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(at), 0o755); err != nil {
		return fmt.Errorf("failed to make room for a checkout at %q: %w", at, err)
	}

	if _, err := Run(ctx, dir, "worktree", "add", "--detach", at, sha); err != nil {
		return fmt.Errorf("failed to check %q out at %q: %w", sha, at, err)
	}

	return nil
}

// Reap takes a materialised worktree away along with the record git keeps of it, and says nothing
// about a directory that was never there.
func Reap(ctx context.Context, dir, at string) error {
	if _, err := os.Stat(at); err != nil {
		return nil
	}

	if _, err := Run(ctx, dir, "worktree", "remove", "--force", at); err != nil {
		if err := os.RemoveAll(at); err != nil {
			return fmt.Errorf("failed to take the checkout at %q away: %w", at, err)
		}
	}

	if _, err := Run(ctx, dir, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to forget the checkout at %q: %w", at, err)
	}

	return nil
}
