package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// rebaseDirs are the sequences that leave HEAD detached for as long as they are unfinished.
var rebaseDirs = []string{"rebase-merge", "rebase-apply"}

// HeadState is what a checkout's HEAD stands on and what is holding it there.
type HeadState struct {
	// Branch is the branch the work belongs to, which a stopped rebase still names even though
	// its HEAD is detached.
	Branch string
	// SequenceInProgress is whether a merge, a rebase or a cherry-pick is part-way through.
	SequenceInProgress bool
	// RepositoryURL is the canonical address of the repository origin points at, empty when
	// there is no origin or it names no forge.
	RepositoryURL string
}

// ReadHeadState reads a checkout's HEAD, answering empty for a directory git will not speak for.
func (c *Client) ReadHeadState(ctx context.Context, dir string) HeadState {
	out, err := Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD", "--absolute-git-dir")
	if err != nil {
		return HeadState{}
	}

	branch, gitDir, ok := strings.Cut(out, "\n")
	if !ok {
		return HeadState{}
	}

	branch, gitDir = strings.TrimSpace(branch), strings.TrimSpace(gitDir)
	if branch == "HEAD" {
		branch = rebasingBranch(gitDir)
	}

	return HeadState{
		Branch:             branch,
		SequenceInProgress: hasSequenceMarker(gitDir),
		RepositoryURL:      readRepositoryURL(ctx, dir),
	}
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
