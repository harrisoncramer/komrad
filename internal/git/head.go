package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// rebaseDirs are the sequences that leave HEAD detached for as long as they are unfinished.
var rebaseDirs = []string{"rebase-merge", "rebase-apply"}

// HeadState is what a checkout's HEAD stands on and what is holding it there.
type HeadState struct {
	// Branch is the branch the work belongs to, which a stopped rebase still names even though
	// its HEAD is detached.
	Branch string
	// SHA is the commit HEAD points at.
	SHA string
	// Sequence is what git has part-way through in the checkout.
	Sequence Sequence
	// RepositoryURL is the canonical address of the repository origin points at, empty when
	// there is no origin or it names no forge.
	RepositoryURL string
	// Root is the top of the checkout.
	Root string
	// SnapshotTree is the tree the named snapshot holds, empty when none was named or git cannot
	// read it.
	SnapshotTree string
}

// ReadHeadStateParams names the checkout and a snapshot whose tree is read beside its HEAD.
type ReadHeadStateParams struct {
	Dir         string
	SnapshotSHA string
}

// ReadHeadState reads a checkout's HEAD, answering empty for a directory git will not speak for.
func (c *Client) ReadHeadState(ctx context.Context, params ReadHeadStateParams) HeadState {
	args := []string{"rev-parse", "HEAD"}
	if params.SnapshotSHA != "" {
		args = append(args, params.SnapshotSHA+"^{tree}")
	}

	out, err := Run(ctx, params.Dir, append(args, "--symbolic-full-name", "HEAD", "--absolute-git-dir", "--show-toplevel")...)
	if err != nil && params.SnapshotSHA != "" {
		return c.ReadHeadState(ctx, ReadHeadStateParams{Dir: params.Dir})
	}
	if err != nil {
		return HeadState{}
	}

	lines := strings.Split(out, "\n")
	if len(lines) != len(args)+2 {
		return HeadState{}
	}

	var snapshotTree string
	if params.SnapshotSHA != "" {
		snapshotTree = strings.TrimSpace(lines[1])
		lines = slices.Delete(lines, 1, 2)
	}

	sha, branch := strings.TrimSpace(lines[0]), branchOf(lines[1])
	gitDir, root := strings.TrimSpace(lines[2]), strings.TrimSpace(lines[3])
	if branch == "" {
		branch = rebasingBranch(gitDir)
	}

	return HeadState{
		Branch:        branch,
		SHA:           sha,
		Sequence:      readSequence(gitDir),
		RepositoryURL: c.readRepositoryURL(ctx, params.Dir),
		Root:          root,
		SnapshotTree:  snapshotTree,
	}
}

// ReadRepositoryURL is the canonical address of the repository a checkout's origin points at, empty when it has none.
func (c *Client) ReadRepositoryURL(ctx context.Context, dir string) string {
	return c.readRepositoryURL(ctx, dir)
}

func (c *Client) readRepositoryURL(ctx context.Context, dir string) string {
	remote := c.readOrigin(ctx, dir)
	if remote == nil {
		return ""
	}

	return remote.Canonical()
}

// ReadOriginHost is the web host a checkout's origin is on, empty when it has none komrad can read.
func (c *Client) ReadOriginHost(ctx context.Context, dir string) string {
	remote := c.readOrigin(ctx, dir)
	if remote == nil {
		return ""
	}

	return remote.Host
}

// readOrigin is a checkout's origin remote, nil when it has none komrad can read.
func (c *Client) readOrigin(ctx context.Context, dir string) *RemoteURL {
	raw, err := Run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return nil
	}

	remote, err := c.ParseRemoteURL(ctx, raw)
	if err != nil {
		return nil
	}

	return &remote
}

// rebasingBranch is the branch a stopped rebase belongs to.
func rebasingBranch(gitDir string) string {
	for _, dir := range rebaseDirs {
		name, err := os.ReadFile(filepath.Join(gitDir, dir, "head-name"))
		if err != nil {
			continue
		}

		return branchOf(string(name))
	}

	return ""
}

// branchOf is the branch a full ref name points at, empty for anything that is not a branch.
func branchOf(ref string) string {
	branch, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
	if !ok {
		return ""
	}

	return branch
}
