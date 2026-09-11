package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Worktree is one checkout of a repository, and the branch it holds when it is on one.
type Worktree struct {
	Dir    string
	Branch string
}

// ListWorktrees reports every checkout of a repository, the main one first.
func (c *Client) ListWorktrees(ctx context.Context, repo string) ([]Worktree, error) {
	out, err := Run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list the checkouts of %q: %w", repo, err)
	}

	return parseWorktrees(out), nil
}

// parseWorktrees reads the porcelain listing, where each checkout is a block opened by its
// directory.
func parseWorktrees(out string) []Worktree {
	var listed []Worktree
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			listed = append(listed, Worktree{Dir: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch refs/heads/") && len(listed) > 0:
			listed[len(listed)-1].Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}

	return listed
}

// Materialise checks a commit out as a worktree of its own, detached so nothing can be pushed
// from it by accident.
func (c *Client) Materialise(ctx context.Context, dir, sha, at string) error {
	// A checkout already on the commit is left exactly as it is, holding whatever the reviewer has
	// typed into it.
	if head, ok := c.WorktreeHead(ctx, at); ok {
		if head == sha {
			return nil
		}

		if _, err := Run(ctx, at, "checkout", "--detach", "--force", sha); err == nil {
			return nil
		}
	}

	if err := c.Reap(ctx, dir, at); err != nil {
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

// WorktreeHead is the commit a materialised checkout stands on, and false for anything that is
// not the top of one.
func (c *Client) WorktreeHead(ctx context.Context, at string) (string, bool) {
	if !isWorktreeAt(ctx, at) {
		return "", false
	}

	head, err := Run(ctx, at, "rev-parse", "HEAD")
	if err != nil {
		return "", false
	}

	return head, true
}

// IsDirty reports whether a checkout holds anything the commit it stands on does not, counting
// untracked files but not ignored ones.
func (c *Client) IsDirty(ctx context.Context, at string) (bool, error) {
	out, err := Run(ctx, at, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("failed to read what is in the checkout at %q: %w", at, err)
	}

	return out != "", nil
}

// isWorktreeAt reports whether a path is the top of a checkout rather than somewhere inside one.
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

// Reap takes a materialised worktree away along with the record git keeps of it, and says
// nothing about a directory that was never there.
func (c *Client) Reap(ctx context.Context, dir, at string) error {
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
