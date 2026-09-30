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
	// SequenceInProgress is whether a merge, a rebase or a cherry-pick is part-way through.
	SequenceInProgress bool
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

	out, err := Run(ctx, params.Dir, append(args, "--abbrev-ref", "HEAD", "--absolute-git-dir", "--show-toplevel")...)
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

	sha, branch := strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1])
	gitDir, root := strings.TrimSpace(lines[2]), strings.TrimSpace(lines[3])
	if branch == "HEAD" {
		branch = rebasingBranch(gitDir)
	}

	return HeadState{
		Branch:             branch,
		SHA:                sha,
		SequenceInProgress: hasSequenceMarker(gitDir),
		RepositoryURL:      readRepositoryURL(ctx, params.Dir),
		Root:               root,
		SnapshotTree:       snapshotTree,
	}
}

// ReadRepositoryURL is the canonical address of the repository a checkout's origin points at, empty when it has none.
func (c *Client) ReadRepositoryURL(ctx context.Context, dir string) string {
	return readRepositoryURL(ctx, dir)
}

func readRepositoryURL(ctx context.Context, dir string) string {
	raw, err := Run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}

	remote, err := ParseRemoteURL(raw)
	if err != nil {
		return ""
	}

	return remote.Canonical()
}

// rebasingBranch is the branch a stopped rebase belongs to.
func rebasingBranch(gitDir string) string {
	for _, dir := range rebaseDirs {
		name, err := os.ReadFile(filepath.Join(gitDir, dir, "head-name"))
		if err != nil {
			continue
		}

		branch, ok := strings.CutPrefix(strings.TrimSpace(string(name)), "refs/heads/")
		if !ok {
			return ""
		}

		return branch
	}

	return ""
}

func hasSequenceMarker(gitDir string) bool {
	for _, marker := range sequenceMarkers {
		if _, err := os.Stat(filepath.Join(gitDir, marker)); err == nil {
			return true
		}
	}

	return false
}
