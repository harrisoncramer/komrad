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
// Explainer: the shadow guard reads git with -z everywhere. core.quotePath is on by default, so
// a non-ASCII path comes back C-quoted, and that spelling matches nothing when it goes back as a
// pathspec: the refusal would be silently absent for exactly the paths whose loss cannot be
// undone. It also settles a filename holding a newline, which a line split would tear in two.

// Explainer: a reset removes tracked and untracked files and keeps ignored ones, which is a
// three-way choice rather than an oversight. A park is built with git add -A, so it holds
// untracked files and removing them is recoverable, while it cannot hold an ignored file and
// removing one would be the only step here with no way back. An abandoned attempt is mostly
// untracked files, so leaving them would hand the agent a tree that is neither what it had nor
// what a fresh checkout would give it, still compiling into the package it was told to abandon.

// Explainer: the two counts a plan carries are read from different places on purpose. Landed is
// counted off the graph, because it says how many commits leave the branch, and that has to
// include merges or a branch whose landed run is all merges reads as having landed nothing.
// Replay is read from git cherry, because it says how many commits a rebase would actually
// move, and git skips both merges and commits whose patch the base already holds.

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

// ErrBaseUnrelated says the base shares no history with the branch, so nothing relates them.
var ErrBaseUnrelated = errors.New("the base shares no history with this branch")

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
	// whole, which is where a replay of the rest starts, empty when the base holds none of it.
	Cutoff string
	// Landed is how many of the branch's own commits the cutoff covers, and Replay how many
	// would actually be moved.
	Landed int
	Replay int
	// Merges is how many merge commits a rebase would discard, since it flattens history.
	Merges int
	// ConflictPaths are the paths a three-way merge of the base into the branch cannot settle.
	ConflictPaths []string
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

	shallow, err := isShallow(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.Shallow = shallow

	hidden, err := readHiddenPaths(ctx, dir)
	if err != nil {
		return TreeState{}, err
	}
	state.HiddenPaths = hidden

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

// PredictRebase reads what a rebase onto ref would run into, without touching the checkout.
func PredictRebase(ctx context.Context, dir, ref string) (Prediction, error) {
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
	contained, err := isAncestor(ctx, dir, ref, "HEAD")
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

	// Counted off the graph rather than off a list position, since a side branch merged in sits
	// before the cutoff in topological order while not being reachable from it.
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
func RebaseOnto(ctx context.Context, dir, ref, cutoff string) error {
	return runRebase(ctx, dir, ref, "rebase", "--onto", ref, cutoff)
}

// ReleaseRefsUnder deletes every ref beneath prefix except keep, which is how a park that has
// been superseded stops pinning a whole worktree in the shared clone.
func ReleaseRefsUnder(ctx context.Context, dir, prefix, keep string) error {
	out, err := Run(ctx, dir, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return fmt.Errorf("failed to list the refs under %q: %w", prefix, err)
	}

	for _, ref := range strings.Split(out, "\n") {
		if ref == "" || ref == keep {
			continue
		}
		if err := ReleaseRef(ctx, dir, ref); err != nil {
			return err
		}
	}

	return nil
}

// shadowChunk is how many pathspecs one ls-files is given, so a branch far behind its base
// cannot build an argument list the kernel refuses.
const shadowChunk = 500

// FindShadowedPaths are the paths a reset onto ref would write that currently hold ignored
// content, which is content a park cannot hold and so cannot give back.
func FindShadowedPaths(ctx context.Context, dir, ref string) ([]string, error) {
	// -z throughout, since core.quotePath is on by default and a C-quoted name goes back to git
	// as a pathspec that matches nothing.
	changed, err := Run(ctx, dir, "diff", "--name-only", "-z", "HEAD", ref)
	if err != nil {
		return nil, fmt.Errorf("failed to read what a reset onto %s would write in %q: %w", ref, dir, err)
	}

	paths := splitNUL(changed)
	if len(paths) == 0 {
		return nil, nil
	}

	// Asked only of the paths the reset would actually write, since a worktree's ignored files
	// run to whole dependency trees and every one of them would otherwise be a question.
	shadowed := []string{}
	for start := 0; start < len(paths); start += shadowChunk {
		end := min(start+shadowChunk, len(paths))

		args := append([]string{"ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--"}, paths[start:end]...)
		out, err := Run(ctx, dir, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to read the ignored files of %q: %w", dir, err)
		}

		shadowed = append(shadowed, splitNUL(out)...)
	}

	return shadowed, nil
}

// splitNUL reads a NUL-separated list from git, dropping the trailing empty element.
func splitNUL(out string) []string {
	fields := []string{}
	for _, field := range strings.Split(out, "\x00") {
		if field != "" {
			fields = append(fields, field)
		}
	}

	return fields
}

// FindNestedRepositories are the repositories checked out inside dir whose files a clean would
// otherwise remove, since a park records such a repository as one gitlink and holds no file of it.
func FindNestedRepositories(ctx context.Context, dir string) ([]string, error) {
	root, err := GetRoot(ctx, dir)
	if err != nil {
		return nil, err
	}

	// Read as the files a clean would take rather than as untracked directories, because git
	// only collapses a directory into its own name while nothing in it is tracked: a nested
	// repository under a tracked parent is listed file by file and its own name never appears.
	out, err := Run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("failed to read the untracked files of %q: %w", root, err)
	}

	seen := map[string]bool{}
	nested := []string{}
	for _, path := range splitNUL(out) {
		for parent := filepath.Dir(path); parent != "." && parent != string(filepath.Separator); parent = filepath.Dir(parent) {
			if seen[parent] {
				break
			}
			seen[parent] = true

			if _, err := os.Stat(filepath.Join(root, parent, ".git")); err == nil {
				nested = append(nested, parent)

				break
			}
		}
	}

	return nested, nil
}

// ResetToBaseParams says which checkout to reset, onto what, and what the clean must spare.
type ResetToBaseParams struct {
	// Dir is anywhere inside the checkout, since the root is resolved from it, and Ref what
	// the tracked files are made to match.
	Dir string
	Ref string
	// Keep are paths the clean leaves alone whether or not they are ignored, for the files a
	// workflow furnishes and only ever furnishes when it creates the tree.
	Keep []string
}

// ResetToBase moves the branch onto ref and makes the checkout match it, keeping ignored files.
func ResetToBase(ctx context.Context, params ResetToBaseParams) error {
	// Resolved rather than trusted, the way Park does, because git clean is measured from the
	// directory it runs in while reset --hard rewrites the whole worktree, so a path one level
	// down would leave the root's untracked files standing and half the tree reset.
	root, err := GetRoot(ctx, params.Dir)
	if err != nil {
		return err
	}

	if _, err := Run(ctx, root, "reset", "--hard", params.Ref); err != nil {
		return fmt.Errorf("failed to reset %q onto %s: %w", root, params.Ref, err)
	}

	// -fd without -x: the park holds untracked files, so removing them is recoverable, and an
	// abandoned attempt is mostly untracked files that would otherwise still compile.
	args := []string{"clean", "-fd"}
	for _, keep := range params.Keep {
		args = append(args, "-e", keep)
	}
	if _, err := Run(ctx, root, args...); err != nil {
		return fmt.Errorf("failed to clear the untracked files of %q: %w", root, err)
	}

	return nil
}

// Rebase moves a checkout's branch onto ref, leaving a stopped rebase where it is.
func Rebase(ctx context.Context, dir, ref string) error {
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

	// Walked newest first rather than halved, since a squash leaves the intermediate states
	// holding content the base never took and so reading as unlanded inside a landed run.
	for i := len(own) - 1; i >= 0; i-- {
		merged, _, err := readMergedTree(ctx, dir, ref, own[i])
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
	_, paths, err := readMergedTree(ctx, dir, ref, "HEAD")

	return paths, err
}

// readMergedTree merges two commits in memory and answers with the tree that would result and
// the paths that would not settle, without touching the checkout.
func readMergedTree(ctx context.Context, dir, left, right string) (tree string, conflicts []string, err error) {
	// A conflict is reported by exiting 1 with the paths on stdout, so that code is an answer.
	out, err := runAllowing(ctx, dir, 1, "merge-tree", "--write-tree", "--name-only", left, right)
	if err != nil {
		return "", nil, fmt.Errorf("failed to test-merge %s into %s in %q: %w", left, right, dir, err)
	}

	// The first line is the tree the merge wrote, the paths follow it, and a blank line ends
	// them before the messages git wrote about each one.
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return "", nil, fmt.Errorf("failed to read what merging %s into %s would do in %q", left, right, dir)
	}
	tree = lines[0]

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

	// No common commit is reported by exiting 1, which is an answer rather than a failure.
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("failed to test what %s and %s share in %q: %w", left, right, dir, err)
}

// isAncestor reports whether one commit is reachable from another.
func isAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error) {
	_, err := Run(ctx, dir, "merge-base", "--is-ancestor", maybeAncestor, of)
	if err == nil {
		return true, nil
	}

	// Unreachable is reported by exiting 1, which is an answer rather than a failure.
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
	// merge commits, which is exactly what a rebase replays.
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

// readHiddenPaths reads the tracked paths marked assume-unchanged or skip-worktree, whose
// working copy git is deliberately not reading.
func readHiddenPaths(ctx context.Context, dir string) ([]string, error) {
	out, err := Run(ctx, dir, "ls-files", "-v")
	if err != nil {
		return nil, fmt.Errorf("failed to read the index flags of %q: %w", dir, err)
	}

	paths := []string{}
	for _, line := range strings.Split(out, "\n") {
		// Any lowercase tag is assume-unchanged and S is skip-worktree, which is the same rule
		// hasAssumeUnchanged applies, since a path that is both marked and modified reads as c.
		if len(line) < 3 || (line[0] != 'S' && (line[0] < 'a' || line[0] > 'z')) {
			continue
		}
		paths = append(paths, line[2:])
	}

	return paths, nil
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
