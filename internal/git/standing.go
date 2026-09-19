package git

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Changes is what a checkout holds that is not committed.
type Changes struct {
	// Modified are the paths git tracks that differ from HEAD, in the index or the tree.
	Modified []string
	// Untracked are the paths git neither tracks nor ignores.
	Untracked []string
}

// Commit is one commit as a listing names it.
type Commit struct {
	SHA         string
	Subject     string
	CommittedAt time.Time
}

// ReadChanges is every uncommitted path in a checkout, split by whether git tracks it.
func (c *Client) ReadChanges(ctx context.Context, dir string) (Changes, error) {
	refreshIndex(ctx, dir)

	modified, err := pathList(ctx, dir, "diff-index", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return Changes{}, fmt.Errorf("failed to read what %q has changed: %w", dir, err)
	}

	untracked, err := pathList(ctx, dir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return Changes{}, fmt.Errorf("failed to read what %q has added: %w", dir, err)
	}

	return Changes{Modified: modified, Untracked: untracked}, nil
}

// pathList runs a git command that prints NUL-separated paths.
func pathList(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := RunBytes(ctx, dir, args...)
	if err != nil {
		return nil, err
	}

	var paths []string
	for path := range bytes.SplitSeq(bytes.TrimSuffix(out, []byte{0}), []byte{0}) {
		if len(path) > 0 {
			paths = append(paths, string(path))
		}
	}

	return paths, nil
}

// CountAheadBehind is how many commits HEAD holds that ref does not, and the other way round.
func (c *Client) CountAheadBehind(ctx context.Context, dir, ref string) (ahead, behind int, err error) {
	out, err := Run(ctx, dir, "rev-list", "--left-right", "--count", ref+"...HEAD")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to count %q against %s: %w", dir, ref, err)
	}

	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("failed to read the count of %q against %s: %q", dir, ref, out)
	}

	behind, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read the count of %q against %s: %w", dir, ref, err)
	}

	ahead, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read the count of %q against %s: %w", dir, ref, err)
	}

	return ahead, behind, nil
}

// IsBranchHeldElsewhere reports whether some other ref reaches the branch's tip.
func (c *Client) IsBranchHeldElsewhere(ctx context.Context, dir, branch string) (bool, error) {
	own := "refs/heads/" + branch
	out, err := Run(ctx, dir, "for-each-ref", "--format=%(refname)", "--contains", own)
	if err != nil {
		return false, fmt.Errorf("failed to read which refs hold %s: %w", branch, err)
	}

	for ref := range strings.SplitSeq(out, "\n") {
		if ref != "" && ref != own {
			return true, nil
		}
	}

	return false, nil
}

// ListCommitsSince is the newest commits HEAD holds that ref does not, at most limit of them.
func (c *Client) ListCommitsSince(ctx context.Context, dir, ref string, limit int) ([]Commit, error) {
	out, err := RunBytes(ctx, dir, "log", "--format=%H%x00%s%x00%cI", "-n", strconv.Itoa(limit), ref+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to list the commits of %q since %s: %w", dir, ref, err)
	}

	return parseCommits(string(out))
}

func parseCommits(out string) ([]Commit, error) {
	var commits []Commit
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}

		fields := strings.SplitN(line, "\x00", 3)
		if len(fields) != 3 {
			return nil, fmt.Errorf("failed to read a commit line: %q", line)
		}

		at, err := time.Parse(time.RFC3339, fields[2])
		if err != nil {
			return nil, fmt.Errorf("failed to read when %s was committed: %w", fields[0], err)
		}

		commits = append(commits, Commit{SHA: fields[0], Subject: fields[1], CommittedAt: at})
	}

	return commits, nil
}

// ResolveBase is the ref a checkout is measured against, origin's copy of the base when it has
// one and origin's default branch when no base is named, and false when it has neither.
func (c *Client) ResolveBase(ctx context.Context, dir, base string) (string, bool) {
	ref := resolveBase(ctx, dir, base)

	return ref, ref != "HEAD"
}

// PruneWorktrees drops the registrations of checkouts whose directories are gone.
func (c *Client) PruneWorktrees(ctx context.Context, repo string) error {
	if _, err := Run(ctx, repo, "worktree", "prune"); err != nil {
		return fmt.Errorf("failed to prune the worktrees of %q: %w", repo, err)
	}

	return nil
}
