package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Explainer: a rebase that stops on conflicts is reported and left in place rather than
// aborted. The agent sitting in that directory is the one that can settle it, and aborting on
// its behalf would throw away the only part of the operation that needed judgement. The same
// reasoning is why an untracked file does not block a rebase here: git completes one with
// untracked files in place, and an agent's worktree nearly always holds some.

// ErrRebaseConflicted says a rebase stopped part-way on paths it could not settle.
var ErrRebaseConflicted = errors.New("the rebase stopped on conflicts")

// TreeState is what a checkout holds that decides whether a rebase may start.
type TreeState struct {
	// Branch is the branch the checkout stands on, empty for a detached head.
	Branch string
	// TrackedChanges is whether anything git already knows about differs from HEAD.
	TrackedChanges bool
	// InProgress is whether a merge, a rebase or a cherry-pick is already part-way through.
	InProgress bool
}

// ReadTreeState reads the three things about a checkout a rebase has to decide on.
func ReadTreeState(ctx context.Context, dir string) (TreeState, error) {
	state := TreeState{}

	if branch := GetCurrentBranch(ctx, dir); branch != nil {
		state.Branch = *branch
	}

	// The test git itself applies before it will start a rebase.
	if _, err := Run(ctx, dir, "diff-index", "--quiet", "HEAD", "--"); err != nil {
		state.TrackedChanges = true
	}

	inProgress, err := isSequenceInProgress(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.InProgress = inProgress

	return state, nil
}

// sequenceMarkers are the paths git keeps while something else owns the index.
var sequenceMarkers = []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"}

// isSequenceInProgress reports whether git is mid-merge, mid-rebase or mid-cherry-pick.
func isSequenceInProgress(ctx context.Context, dir string) (bool, error) {
	gitDir, err := Run(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return false, fmt.Errorf("failed to read the git directory of %q: %w", dir, err)
	}

	for _, marker := range sequenceMarkers {
		if _, err := os.Stat(filepath.Join(gitDir, marker)); err == nil {
			return true, nil
		}
	}

	return false, nil
}

// FetchBase brings the base up to date and answers with the ref to rebase onto and its commit.
func FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error) {
	root, err := GetRoot(ctx, dir)
	if err != nil {
		return "", "", err
	}

	trimmed := strings.TrimSpace(base)
	if trimmed != "" {
		if _, err := Run(ctx, root, "fetch", "--quiet", "origin", trimmed); err != nil {
			return "", "", fmt.Errorf("failed to fetch %q from origin: %w", trimmed, err)
		}
	}

	ref = resolveBase(ctx, root, trimmed)

	sha, err = Run(ctx, dir, "rev-parse", "--verify", ref)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve the base %q: %w", ref, err)
	}

	return ref, sha, nil
}

// CountCommitsToReplay is how many of the checkout's own commits a rebase onto ref would move.
func CountCommitsToReplay(ctx context.Context, dir, ref string) (int, error) {
	out, err := Run(ctx, dir, "rev-list", "--count", ref+"..HEAD")
	if err != nil {
		return 0, fmt.Errorf("failed to count the commits ahead of %q: %w", ref, err)
	}

	count := 0
	if _, err := fmt.Sscanf(out, "%d", &count); err != nil {
		return 0, fmt.Errorf("failed to read the commit count against %q: %w", ref, err)
	}

	return count, nil
}

// Rebase moves a checkout's branch onto ref, leaving a stopped rebase where it is.
func Rebase(ctx context.Context, dir, ref string) error {
	if _, err := Run(ctx, dir, "rebase", ref); err == nil {
		return nil
	}

	stopped, stateErr := isSequenceInProgress(ctx, dir)
	if stateErr == nil && stopped {
		return fmt.Errorf("%w onto %s", ErrRebaseConflicted, ref)
	}

	return fmt.Errorf("failed to rebase onto %s in %q", ref, dir)
}
