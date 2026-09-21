package git

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"weave/internal/fail"
)

// ErrRebaseConflicted says a rebase stopped part-way on paths it could not settle.
var ErrRebaseConflicted = fail.Precondition("the rebase stopped on conflicts")

// ErrBaseUnresolved says the base to rebase onto is not a ref this repository can reach.
var ErrBaseUnresolved = fail.Precondition("the base branch does not resolve to a remote-tracking ref")

// ErrBaseUnrelated says the base shares no history with the branch, so nothing relates them.
var ErrBaseUnrelated = fail.Precondition("the base shares no history with this branch")

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
	// Shallow is whether the clone is cut off from its own history, which makes every question
	// about what the branch and the base share answer wrongly rather than fail.
	Shallow bool
	// HiddenPaths are the paths whose content the index is holding back on purpose, which
	// nothing can read out of the checkout and so nothing can put back.
	HiddenPaths []string
}

// Prediction is what a rebase onto a base would run into, read without touching the checkout.
type Prediction struct {
	// Cutoff is the newest of the branch's own commits whose content the base already holds
	// whole, empty when the base holds none of it.
	Cutoff string
	// Landed is how many of the branch's own commits the cutoff covers, and Replay how many
	// would actually be moved.
	Landed int
	Replay int
	// Merges is how many merge commits a rebase would discard.
	Merges int
	// ConflictPaths are the paths a three-way merge of the base into the branch cannot settle.
	ConflictPaths []string
}

// ReadTreeState reads the three things about a checkout a rebase has to decide on.
func (c *Client) ReadTreeState(ctx context.Context, dir string) (TreeState, error) {
	state := TreeState{}

	if branch := c.GetCurrentBranch(ctx, dir); branch != nil {
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

	shallow, err := isShallow(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.Shallow = shallow

	hidden, err := hiddenPaths(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.HiddenPaths = hidden

	return state, nil
}

// FetchBase brings the base up to date and answers with the ref to rebase onto and its commit.
func (c *Client) FetchBase(ctx context.Context, dir, base string) (ref, sha string, err error) {
	root, err := c.GetRoot(ctx, dir)
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
func (c *Client) CountCommitsToReplay(ctx context.Context, dir, ref string) (int, error) {
	return countRevList(ctx, dir, ref, "--count", ref+"..HEAD")
}

// PredictRebase reads what a rebase onto ref would run into, without touching the checkout.
func (c *Client) PredictRebase(ctx context.Context, dir, ref string) (Prediction, error) {
	prediction := Prediction{}

	related, err := doesShareHistory(ctx, dir, ref, "HEAD")
	if err != nil {
		return Prediction{}, err
	}
	if !related {
		return Prediction{}, fmt.Errorf("%w: %s", ErrBaseUnrelated, ref)
	}

	own, err := listOwnCommits(ctx, dir, ref)
	if err != nil {
		return Prediction{}, err
	}

	prediction.Replay, err = countUnlanded(ctx, dir, ref, "")
	if err != nil {
		return Prediction{}, err
	}

	prediction.Merges, err = countRevList(ctx, dir, ref, "--count", "--merges", ref+"..HEAD")
	if err != nil {
		return Prediction{}, err
	}

	prediction.ConflictPaths, err = readConflictPaths(ctx, dir, ref)
	if err != nil {
		return Prediction{}, err
	}

	// A base the branch already contains is not one to catch up to, whatever its content says:
	// a branch that committed something and then reverted it holds the base's own tree, and
	// replaying from its tip would drop both commits over a rebase git calls a no-op.
	contained, err := isAncestor(ctx, local, dir, ref, "HEAD")
	if err != nil || contained {
		return prediction, err
	}

	at, err := findLandedCutoff(ctx, dir, ref, own)
	if err != nil {
		return Prediction{}, err
	}
	if at < 0 {
		return prediction, nil
	}

	prediction.Cutoff = own[at]

	// Counted off the graph rather than off a list position.
	prediction.Landed, err = countRevList(ctx, dir, ref, "--count", ref+".."+prediction.Cutoff)
	if err != nil {
		return Prediction{}, err
	}

	prediction.Replay, err = countUnlanded(ctx, dir, ref, prediction.Cutoff)
	if err != nil {
		return Prediction{}, err
	}

	return prediction, nil
}

// RebaseOnto replays the checkout's commits after cutoff onto ref, leaving a stopped rebase.
func (c *Client) RebaseOnto(ctx context.Context, dir, ref, cutoff string) error {
	return runRebase(ctx, dir, ref, "rebase", "--onto", ref, cutoff)
}

// ReleaseRefsUnder deletes every ref beneath prefix except keep.
func (c *Client) ReleaseRefsUnder(ctx context.Context, dir, prefix, keep string) error {
	out, err := Run(ctx, dir, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return fmt.Errorf("failed to list the refs under %q: %w", prefix, err)
	}

	for _, ref := range strings.Split(out, "\n") {
		if ref == "" || ref == keep {
			continue
		}
		if err := c.ReleaseRef(ctx, dir, ref); err != nil {
			return err
		}
	}

	return nil
}

// Rebase moves a checkout's branch onto ref, leaving a stopped rebase where it is.
func (c *Client) Rebase(ctx context.Context, dir, ref string) error {
	return runRebase(ctx, dir, ref, "rebase", ref)
}

// listOwnCommits lists the branch's own commits against ref, oldest first.
func listOwnCommits(ctx context.Context, dir, ref string) ([]string, error) {
	out, err := Run(ctx, dir, "rev-list", "--reverse", ref+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to list what %q has of its own against %s: %w", dir, ref, err)
	}

	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	return strings.Split(out, "\n"), nil
}

// findLandedCutoff finds the index in own of the newest commit whose content the base already
// holds whole, or -1 when the base holds none of them.
func findLandedCutoff(ctx context.Context, dir, ref string, own []string) (int, error) {
	if len(own) == 0 {
		return -1, nil
	}

	base, err := Run(ctx, dir, "rev-parse", "--verify", ref+"^{tree}")
	if err != nil {
		return -1, fmt.Errorf("failed to read the tree of %q: %w", ref, err)
	}

	// Walked newest first rather than halved.
	for i := len(own) - 1; i >= 0; i-- {
		merged, _, err := readMergedTree(ctx, local, readMergedTreeParams{
			Dir:   dir,
			Left:  ref,
			Right: own[i],
		})
		if err != nil {
			return -1, err
		}
		if merged == base {
			return i, nil
		}
	}

	return -1, nil
}

// readConflictPaths test-merges ref into the head and answers with what would not settle.
func readConflictPaths(ctx context.Context, dir, ref string) ([]string, error) {
	_, paths, err := readMergedTree(ctx, local, readMergedTreeParams{
		Dir:   dir,
		Left:  ref,
		Right: "HEAD",
	})

	return paths, err
}

// readMergedTreeParams is one merge done in memory: the two sides and, when it is not to be worked
// out from their history, the base to merge them from.
type readMergedTreeParams struct {
	Dir       string
	Left      string
	Right     string
	MergeBase string
}

// readMergedTree merges two commits in memory and answers with the tree that would result and
// the paths that would not settle, without touching the checkout.
func readMergedTree(ctx context.Context, on runner, params readMergedTreeParams) (tree string, conflicts []string, err error) {
	dir, left, right := params.Dir, params.Left, params.Right

	args := []string{"merge-tree", "--write-tree", "--name-only"}
	if params.MergeBase != "" {
		args = append(args, "--merge-base="+params.MergeBase)
	}
	args = append(args, left, right)

	// A conflict is reported by exiting 1 with the paths on stdout.
	out, err := runAllowing(ctx, on, dir, 1, args...)
	if err != nil {
		return "", nil, fmt.Errorf("failed to test-merge %s into %s in %q: %w", left, right, dir, err)
	}

	// The first line is the tree the merge wrote, the paths follow it, and a blank line ends
	// them before the messages git wrote about each one.
	lines := strings.Split(out, "\n")
	tree = lines[0]
	if tree == "" {
		return "", nil, fmt.Errorf("failed to read what merging %s into %s would do in %q", left, right, dir)
	}

	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		conflicts = append(conflicts, line)
	}

	return tree, conflicts, nil
}

// doesShareHistory reports whether two commits have any commit in common.
func doesShareHistory(ctx context.Context, dir, left, right string) (bool, error) {
	_, err := Run(ctx, dir, "merge-base", left, right)
	if err == nil {
		return true, nil
	}

	// No common commit is reported by exiting 1.
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("failed to test what %s and %s share in %q: %w", left, right, dir, err)
}

// isAncestor reports whether one commit is reachable from another.
func isAncestor(ctx context.Context, on runner, dir, maybeAncestor, of string) (bool, error) { //nolint:revive // a params struct is the follow-up
	_, err := runOn(ctx, on, dir, "merge-base", "--is-ancestor", maybeAncestor, of)
	if err == nil {
		return true, nil
	}

	// Unreachable is reported by exiting 1.
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("failed to test whether %s is contained in %s: %w", maybeAncestor, of, err)
}

// countUnlanded counts the commits after cutoff that a rebase would actually move, which is
// those whose patch the base does not already hold.
func countUnlanded(ctx context.Context, dir, ref, cutoff string) (int, error) {
	// git cherry marks a commit + when its patch is not upstream and - when it is, and skips
	// merge commits.
	args := []string{"cherry", ref, "HEAD"}
	if cutoff != "" {
		args = append(args, cutoff)
	}

	out, err := Run(ctx, dir, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to read what %q still owes %s: %w", dir, ref, err)
	}

	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+") {
			count++
		}
	}

	return count, nil
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

// runRebase runs one spelling of rebase and tells a stopped one from a failed one.
func runRebase(ctx context.Context, dir, ref string, args ...string) error {
	_, err := Run(ctx, dir, args...)
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

// isShallow reports whether the clone has been cut off from its own history.
func isShallow(ctx context.Context, dir string) (bool, error) {
	// Asked of git rather than read off disk: the marker lives in the common directory, not in
	// the one a linked worktree reports, and every agent runs in a linked worktree.
	out, err := Run(ctx, dir, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, fmt.Errorf("failed to read whether %q is shallow: %w", dir, err)
	}

	return out == "true", nil
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

	return hasSequenceMarker(gitDir), nil
}
