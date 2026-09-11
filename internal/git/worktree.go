package git

import (
	"context"
	"fmt"
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
