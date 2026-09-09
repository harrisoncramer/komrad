package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Explainer: a rebase that stops on conflicts is reported and left in place rather than
// aborted. The agent sitting in that directory is the one that can settle it, and aborting on
// its behalf would throw away the only part of the operation that needed judgement. The same
// reasoning is why an untracked file does not block a rebase here: git completes one with
// untracked files in place, and an agent's worktree nearly always holds some.
//
// Explainer: two orderings in here are load-bearing rather than incidental. The index is
// refreshed before it is compared against HEAD, because diff-index reads cached stat data and a
// file rewritten with identical content otherwise reads as changed. And the base is fetched
// before it is resolved, because resolving first reads whatever the clone last cached, which is
// how a stale base gets reported as the latest one; the unqualified fetch also puts a missing
// origin/HEAD back, which is what would otherwise make the base fall through to literal HEAD.

// ErrRebaseConflicted says a rebase stopped part-way on paths it could not settle.
var ErrRebaseConflicted = errors.New("the rebase stopped on conflicts")

// ErrBaseUnresolved says the base to rebase onto is not a ref this repository can reach.
var ErrBaseUnresolved = errors.New("the base branch does not resolve to a remote-tracking ref")

// sequenceMarkers are the paths git keeps while something else owns the index.
var sequenceMarkers = []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply"}

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

	// Read before the index is touched, so an unmerged index is reported as the sequence it
	// belongs to rather than as a refresh that would not settle it.
	inProgress, err := isSequenceInProgress(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.InProgress = inProgress

	refreshIndex(ctx, dir)

	dirty, err := isTrackedDirty(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.TrackedChanges = dirty

	return state, nil
}

// FetchBase brings the base up to date and answers with the ref to rebase onto and its commit.
func FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error) {
	root, err := GetRoot(ctx, dir)
	if err != nil {
		return "", "", err
	}

	// Fetched before the base is resolved, and unqualified when nothing was named.
	args := []string{"fetch", "--quiet", "origin"}
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		args = append(args, trimmed)
	}
	if _, err := Run(ctx, root, args...); err != nil {
		return "", "", fmt.Errorf("failed to fetch the base from origin: %w", err)
	}

	ref = resolveBase(ctx, root, base)

	// Rebasing onto the literal HEAD succeeds trivially, reporting a no-op as a success.
	if !strings.HasPrefix(ref, "origin/") {
		return "", "", fmt.Errorf("%w: resolved to %q", ErrBaseUnresolved, ref)
	}

	sha, err = Run(ctx, dir, "rev-parse", "--verify", ref)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve the base %q: %w", ref, err)
	}

	return ref, sha, nil
}

// CountCommitsToReplay is how many of the checkout's own commits a rebase onto ref would move.
func CountCommitsToReplay(ctx context.Context, dir, ref string) (int, error) {
	return countRevList(ctx, dir, ref, "--count", ref+"..HEAD")
}

// Prediction is what a rebase onto a base would run into, read without touching the checkout.
type Prediction struct {
	// SameTree is whether the base and the branch name one tree, so every commit a rebase
	// replayed would land on content the base already holds.
	SameTree bool
	// Merges is how many merge commits a rebase would discard, since it flattens history.
	Merges int
	// ConflictPaths are the paths a three-way merge of the base into the branch cannot settle.
	ConflictPaths []string
}

// PredictRebase reads what a rebase onto ref would run into, without touching the checkout.
func PredictRebase(ctx context.Context, dir, ref string) (Prediction, error) {
	sameTree, err := namesOneTree(ctx, dir, ref)
	if err != nil {
		return Prediction{}, err
	}

	merges, err := countRevList(ctx, dir, ref, "--count", "--merges", ref+"..HEAD")
	if err != nil {
		return Prediction{}, err
	}

	paths, err := conflictPaths(ctx, dir, ref)
	if err != nil {
		return Prediction{}, err
	}

	return Prediction{SameTree: sameTree, Merges: merges, ConflictPaths: paths}, nil
}

// FastForward moves the branch onto ref outright, which is a rebase's answer when replaying
// would only collide with content the base already holds.
func FastForward(ctx context.Context, dir, ref string) error {
	if _, err := Run(ctx, dir, "reset", "--hard", ref); err != nil {
		return fmt.Errorf("failed to move %q onto %s: %w", dir, ref, err)
	}

	return nil
}

// namesOneTree reports whether ref and the checkout's head name the same tree.
func namesOneTree(ctx context.Context, dir, ref string) (bool, error) {
	base, err := Run(ctx, dir, "rev-parse", "--verify", ref+"^{tree}")
	if err != nil {
		return false, fmt.Errorf("failed to read the tree of %q: %w", ref, err)
	}

	head, err := Run(ctx, dir, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil {
		return false, fmt.Errorf("failed to read the tree of the head of %q: %w", dir, err)
	}

	return base == head, nil
}

// conflictPaths test-merges ref into the checkout's head and answers with what would not settle.
func conflictPaths(ctx context.Context, dir, ref string) ([]string, error) {
	// A conflict is reported by exiting 1 with the paths on stdout, so that code is an answer.
	out, err := runAllowing(ctx, dir, 1, "merge-tree", "--write-tree", "--name-only", ref, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to test-merge %s into %q: %w", ref, dir, err)
	}

	// The first line is the tree the merge wrote, the paths follow it, and a blank line ends
	// them before the messages git wrote about each one.
	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		return nil, nil
	}

	paths := []string{}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		paths = append(paths, line)
	}

	return paths, nil
}

// countRevList reads a rev-list count, naming ref in whatever it could not do.
func countRevList(ctx context.Context, dir, ref string, args ...string) (int, error) {
	out, err := Run(ctx, dir, append([]string{"rev-list"}, args...)...)
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
	_, err := Run(ctx, dir, "rebase", ref)
	if err == nil {
		return nil
	}

	stopped, stateErr := isSequenceInProgress(ctx, dir)
	if stateErr != nil {
		return fmt.Errorf("failed to rebase onto %s in %q: %w (and its state could not be read: %w)", ref, dir, err, stateErr)
	}
	if stopped {
		return fmt.Errorf("%w onto %s", ErrRebaseConflicted, ref)
	}

	return fmt.Errorf("failed to rebase onto %s in %q: %w", ref, dir, err)
}

// isTrackedDirty reports whether anything git tracks differs from HEAD.
func isTrackedDirty(ctx context.Context, dir string) (bool, error) {
	_, err := Run(ctx, dir, "diff-index", "--quiet", "HEAD", "--")
	if err == nil {
		return false, nil
	}

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return true, nil
	}

	return false, fmt.Errorf("failed to read what %q has changed: %w", dir, err)
}

// refreshIndex updates the index's cached stat data before it is compared against HEAD.
func refreshIndex(ctx context.Context, dir string) {
	// The exit code is ignored the way git's own require_clean_work_tree ignores it, being
	// non-zero for the unmerged entries the sequence check already owns.
	if _, err := Run(ctx, dir, "update-index", "-q", "--ignore-submodules", "--refresh"); err != nil {
		return
	}
}

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
