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
	if head, ok := WorktreeHead(ctx, at); ok {
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

// WorktreeHead is the commit a materialised checkout stands on, and false for anything that is not
// the top of one, which is what says whether a directory left behind is a copy of the round being
// read or the remains of an earlier one.
func WorktreeHead(ctx context.Context, at string) (string, bool) {
	if !isWorktreeAt(ctx, at) {
		return "", false
	}

	head, err := Run(ctx, at, "rev-parse", "HEAD")
	if err != nil {
		return "", false
	}

	return head, true
}

// IsDirty reports whether a checkout holds anything the commit it stands on does not, untracked
// files included, which is what says there is work in it somebody would miss.
func IsDirty(ctx context.Context, at string) (bool, error) {
	out, err := Run(ctx, at, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("failed to read what is in the checkout at %q: %w", at, err)
	}

	return out != "", nil
}

// isWorktreeAt reports whether a path is the top of a checkout rather than somewhere inside one,
// since git answers about the repository it finds walking up and a force checkout of the enclosing
// repository would throw away whatever that tree held.
func isWorktreeAt(ctx context.Context, at string) bool {
	top, err := Run(ctx, at, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}

	here, err := filepath.EvalSymlinks(at)
	if err != nil {
		return false
	}

	return top == here
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
