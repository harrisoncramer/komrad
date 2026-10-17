package git

import (
	"context"
	"fmt"

	"komrad/internal/fail"
)

// ErrBranchNotOnOrigin says origin holds no copy of a branch.
var ErrBranchNotOnOrigin = fail.Precondition("origin has no copy of this branch, so push it first")

// FetchBranchTip brings the clone's copy of origin's branch up to date and answers the commit it names.
func (c *Client) FetchBranchTip(ctx context.Context, dir, branch string) (string, error) {
	if err := c.RefreshRemoteBranch(ctx, dir, branch); err != nil {
		return "", err
	}

	tip, err := c.ReadRemoteTip(ctx, dir, branch)
	if err != nil {
		return "", err
	}
	if tip == "" {
		return "", fmt.Errorf("%w: %s", ErrBranchNotOnOrigin, branch)
	}

	return tip, nil
}

// ReadRemoteTip is the commit origin's branch named when the clone last fetched it, empty when it holds no copy.
func (c *Client) ReadRemoteTip(ctx context.Context, dir, branch string) (string, error) {
	tip, err := runAllowing(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to read origin's copy of %s: %w", branch, err)
	}

	return tip, nil
}

// errCommitUnknown says the clone holds no commit by that name.
var errCommitUnknown = fail.InvalidArgument("the clone holds no commit by that name")

// ResolveCommit is the full name of the commit a revision names in the clone.
func (c *Client) ResolveCommit(ctx context.Context, dir, rev string) (string, error) {
	sha, err := runAllowing(ctx, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("failed to look for %s: %w", rev, err)
	}
	if sha == "" {
		return "", fmt.Errorf("%w: %s", errCommitUnknown, rev)
	}

	return sha, nil
}
