package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"weave/internal/fail"
)

// snapshotMessage is what the round's commit says.
const snapshotMessage = "weave review round"

// parkMessage is what the commit standing behind an undo says.
const parkMessage = "weave parked worktree"

// snapshotIdentity is who the round's commit is by and when, both pinned so that the same
// worktree taken twice hashes to the same commit and reads as one round.
var snapshotIdentity = []string{
	"GIT_AUTHOR_NAME=weave",
	"GIT_AUTHOR_EMAIL=weave@localhost",
	"GIT_AUTHOR_DATE=@0 +0000",
	"GIT_COMMITTER_NAME=weave",
	"GIT_COMMITTER_EMAIL=weave@localhost",
	"GIT_COMMITTER_DATE=@0 +0000",
}

// TakeSnapshotParams says which worktree to freeze, what to measure it against, and where to hold
// the result.
type TakeSnapshotParams struct {
	// Dir is anywhere inside the agent's worktree.
	Dir string
	// Base is the branch the worktree was cut from, or empty to ask the repository for its
	// default branch.
	Base string
	// Ref is the full ref name the snapshot is held by, so nothing collects it.
	Ref string
}

// Snapshot is one frozen picture of a worktree and the base it is measured against.
type Snapshot struct {
	Root    string
	Base    string
	BaseSHA string
	SHA     string
}

// ProbeTree writes an agent's worktree out as a tree and nothing else.
func (c *Client) ProbeTree(ctx context.Context, dir string) (string, error) {
	root, err := c.GetRoot(ctx, dir)
	if err != nil {
		return "", err
	}

	return writeTree(ctx, root)
}

// seed fills a temporary index for the worktree to be staged into.
type seed func(ctx context.Context, root, index string, env []string) error

// writeTree stages the whole worktree into an index of its own and hashes it, leaving the
// agent's index, worktree and branch exactly as they were.
func writeTree(ctx context.Context, root string) (string, error) {
	tree, err := writeTreeSeeded(ctx, root, seedIndexFromAgent)
	if errors.Is(err, errIndexUnreadable) {
		return writeTreeSeeded(ctx, root, seedIndexFromHead)
	}

	return tree, err
}

// writeTreeSeeded is one attempt at the tree, from an index filled the given way.
func writeTreeSeeded(ctx context.Context, root string, fill seed) (string, error) {
	index, err := tempIndexPath()
	if err != nil {
		return "", err
	}
	defer removeIndex(index)

	env := append(append(os.Environ(), snapshotIdentity...), "GIT_INDEX_FILE="+index)

	if err := fill(ctx, root, index, env); err != nil {
		return "", err
	}

	if _, err := runEnv(ctx, root, env, "add", "-A"); err != nil {
		return "", fmt.Errorf("failed to stage %q into a snapshot: %w", root, err)
	}

	tree, err := runEnv(ctx, root, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("failed to write a snapshot tree for %q: %w", root, err)
	}

	return tree, nil
}

// TakeSnapshot writes an agent's whole worktree, tracked and untracked alike, as a commit off
// the point its branch left the base, building the tree in an index of its own so the agent's
// own index, worktree and branch are left exactly as they were.
func (c *Client) TakeSnapshot(ctx context.Context, params TakeSnapshotParams) (*Snapshot, error) {
	root, err := c.GetRoot(ctx, params.Dir)
	if err != nil {
		return nil, err
	}

	base := resolveBase(ctx, root, params.Base)

	baseSHA, err := Run(ctx, root, "merge-base", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to find where %q left %q: %w", root, base, err)
	}

	tree, err := writeTree(ctx, root)
	if err != nil {
		return nil, err
	}

	env := append(os.Environ(), snapshotIdentity...)

	sha, err := runEnv(ctx, root, env, "commit-tree", tree, "-p", baseSHA, "-m", snapshotMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to commit a snapshot of %q: %w", root, err)
	}

	if params.Ref != "" {
		if _, err := runEnv(ctx, root, env, "update-ref", params.Ref, sha); err != nil {
			return nil, fmt.Errorf("failed to hold the snapshot of %q at %q: %w", root, params.Ref, err)
		}
	}

	return &Snapshot{
		Root:    root,
		Base:    base,
		BaseSHA: baseSHA,
		SHA:     sha,
	}, nil
}

// HoldRef points a ref at a snapshot already written.
func (c *Client) HoldRef(ctx context.Context, dir, ref, sha string) error {
	if ref == "" {
		return nil
	}

	env := append(os.Environ(), snapshotIdentity...)
	if _, err := runEnv(ctx, dir, env, "update-ref", ref, sha); err != nil {
		return fmt.Errorf("failed to hold the snapshot %q at %q: %w", sha, ref, err)
	}

	return nil
}

// ReleaseRef drops the ref holding a snapshot, leaving the commit to be collected.
func (c *Client) ReleaseRef(ctx context.Context, dir, ref string) error {
	if ref == "" {
		return nil
	}

	if _, err := Run(ctx, dir, "update-ref", "-d", ref); err != nil {
		return fmt.Errorf("failed to release the snapshot at %q: %w", ref, err)
	}

	return nil
}

// tempIndexPath names a file git may write an index to, leaving nothing there.
func tempIndexPath() (string, error) {
	file, err := os.CreateTemp("", "weave-round-*.index")
	if err != nil {
		return "", fmt.Errorf("failed to make room for a snapshot index: %w", err)
	}

	path := file.Name()
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to make room for a snapshot index: %w", err)
	}

	if err := os.Remove(path); err != nil {
		return "", fmt.Errorf("failed to make room for a snapshot index: %w", err)
	}

	return path, nil
}

// seedIndexFromHead fills the temporary index from the last commit, and does nothing in a
// repository with no HEAD.
func seedIndexFromHead(ctx context.Context, root, index string, env []string) error {
	if _, err := runEnv(ctx, root, env, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil
	}

	if _, err := runEnv(ctx, root, env, "read-tree", "HEAD"); err != nil {
		return fmt.Errorf("failed to seed a snapshot index from HEAD in %q: %w", root, err)
	}

	return nil
}

// copyIndex writes the agent's index out as the temporary one, modification time included.
func copyIndex(ctx context.Context, root, index string, _ []string) error {
	dir, err := runEnv(ctx, root, agentEnv(), "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("failed to find the index of %q: %w", root, err)
	}

	from, err := os.Open(filepath.Join(dir, "index"))
	if err != nil {
		return fmt.Errorf("%w: %w", errIndexUnreadable, err)
	}
	defer from.Close()

	stat, err := from.Stat()
	if err != nil {
		return fmt.Errorf("failed to read the index of %q: %w", root, err)
	}

	to, err := os.OpenFile(index, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("failed to write a snapshot index for %q: %w", root, err)
	}

	if _, err := io.Copy(to, from); err != nil {
		to.Close()

		return fmt.Errorf("failed to write a snapshot index for %q: %w", root, err)
	}

	if err := to.Close(); err != nil {
		return fmt.Errorf("failed to write a snapshot index for %q: %w", root, err)
	}

	if err := os.Chtimes(index, stat.ModTime(), stat.ModTime()); err != nil {
		return fmt.Errorf("failed to write a snapshot index for %q: %w", root, err)
	}

	return nil
}

// errIndexUnreadable is the one failure the fallback stands for, so a snapshot seeded from the
// last commit means the agent's index could not be read rather than that anything at all failed.
var errIndexUnreadable = fail.Internal("the agent's index cannot be read")

// seedIndexFromAgent copies the agent's index over and clears the flags on the hidden paths the
// worktree still holds.
func seedIndexFromAgent(ctx context.Context, root, index string, env []string) error {
	hidden, err := hiddenPaths(ctx, root)
	if err != nil {
		return fmt.Errorf("%w: %w", errIndexUnreadable, err)
	}

	if err := copyIndex(ctx, root, index, env); err != nil {
		return err
	}

	return revealPaths(ctx, root, index, env, presentPaths(root, hidden))
}

// agentEnv is the environment for the commands that read the agent's own index, with the variable
// naming an index of somebody else's taken out of it.
func agentEnv() []string {
	kept := []string{}

	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_INDEX_FILE=") {
			continue
		}

		kept = append(kept, entry)
	}

	return kept
}

// hiddenPaths are the tracked paths whose working copy git is told not to read.
func hiddenPaths(ctx context.Context, root string) ([]string, error) {
	out, err := runWithEnv(ctx, root, agentEnv(), "ls-files", "-v", "-z")
	if err != nil {
		return nil, fmt.Errorf("failed to read the index flags of %q: %w", root, err)
	}

	paths := []string{}

	for entry := range bytes.SplitSeq(out, []byte{0}) {
		// A lowercase tag is assume-unchanged and S is skip-worktree.
		if len(entry) < 3 || (entry[0] != 'S' && (entry[0] < 'a' || entry[0] > 'z')) {
			continue
		}

		paths = append(paths, string(entry[2:]))
	}

	return paths, nil
}

// presentPaths are the given paths the worktree still has a file for.
func presentPaths(root string, paths []string) []string {
	present := make([]string, 0, len(paths))

	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			present = append(present, path)
		}
	}

	return present
}

// revealPaths clears both flags in the temporary index, one command each.
func revealPaths(ctx context.Context, root, index string, env []string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}

	stat, err := os.Stat(index)
	if err != nil {
		return fmt.Errorf("failed to read a snapshot index for %q: %w", root, err)
	}

	var input bytes.Buffer
	for _, path := range paths {
		input.WriteString(path)
		input.WriteByte(0)
	}

	for _, flag := range []string{"--no-skip-worktree", "--no-assume-unchanged"} {
		args := []string{"update-index", flag, "-z", "--stdin"}
		if _, err := runInput(ctx, root, env, input.Bytes(), args...); err != nil {
			return fmt.Errorf("failed to unflag the files of %q for a snapshot: %w", root, err)
		}
	}

	// Writing the index stamps it with the current time, which would leave every entry in it
	// older than the index and so trusted on its recorded stat alone.
	if err := os.Chtimes(index, stat.ModTime(), stat.ModTime()); err != nil {
		return fmt.Errorf("failed to write a snapshot index for %q: %w", root, err)
	}

	return nil
}

// removeIndex drops the temporary index and the lock git leaves beside it when a write is cut
// short.
func removeIndex(path string) {
	for _, leftover := range []string{path, path + ".lock"} {
		if err := os.Remove(leftover); err != nil {
			continue
		}
	}
}

// GetTree is the tree one snapshot commit holds, which a probe of the worktree is compared
// against to say whether the agent has written anything since.
func (c *Client) GetTree(ctx context.Context, dir, sha string) (string, error) {
	tree, err := Run(ctx, dir, "rev-parse", sha+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("failed to read the tree of %q: %w", sha, err)
	}

	return tree, nil
}

// Standing is where a worktree stands against the round a verdict was given on.
type Standing int

const (
	// StandingSame is a worktree that still hashes to the round.
	StandingSame Standing = iota
	// StandingMoved is a worktree that differs from a round the base does not hold.
	StandingMoved
	// StandingLanded is a worktree carrying nothing beyond a base that holds the round whole.
	StandingLanded
	// StandingLandedMoved is a worktree carrying new work over a base that holds the round whole.
	StandingLandedMoved
)

// ReadStandingParams names the worktree, the base it is measured against, and the round.
type ReadStandingParams struct {
	Dir         string
	Base        string
	SnapshotSHA string
}

// ReadStanding is where a worktree stands against the round a verdict was given on.
func (c *Client) ReadStanding(ctx context.Context, params ReadStandingParams) (Standing, error) {
	root, err := c.GetRoot(ctx, params.Dir)
	if err != nil {
		return StandingSame, err
	}

	probe, err := writeTree(ctx, root)
	if err != nil {
		return StandingSame, err
	}

	held, err := c.GetTree(ctx, root, params.SnapshotSHA)
	if err != nil {
		return StandingSame, err
	}
	if probe == held {
		return StandingSame, nil
	}

	base := resolveBase(ctx, root, params.Base)
	if isOwnHead(ctx, root, base) {
		return StandingMoved, nil
	}

	baseTree, err := Run(ctx, root, "rev-parse", "--verify", "--quiet", base+"^{tree}")
	if err != nil {
		return StandingMoved, nil
	}

	landed, err := doesHoldWhole(ctx, root, base, baseTree, params.SnapshotSHA)
	if err != nil || !landed {
		return StandingMoved, err
	}

	clean, err := isCleanOnto(ctx, root, base, baseTree, probe)
	if err != nil || !clean {
		return StandingLandedMoved, err
	}

	return StandingLanded, nil
}

// isOwnHead reports whether a base names the very branch the worktree is on, which can hold
// nothing the worktree does not.
func isOwnHead(ctx context.Context, root, base string) bool {
	if base == "HEAD" {
		return true
	}

	branch, err := Run(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")

	return err == nil && branch == base
}

// doesHoldWhole reports whether merging a commit into the base would change nothing of it.
func doesHoldWhole(ctx context.Context, root, base, baseTree, sha string) (bool, error) {
	merged, _, err := readMergedTree(ctx, readMergedTreeParams{Dir: root, Left: base, Right: sha})
	if err != nil {
		return false, err
	}

	return merged == baseTree, nil
}

// isCleanOnto reports whether a worktree's tree brings nothing to the base beyond what the base
// already holds, measured from where the worktree's branch left it.
func isCleanOnto(ctx context.Context, root, base, baseTree, probe string) (bool, error) {
	mergeBase, err := Run(ctx, root, "merge-base", base, "HEAD")
	if err != nil {
		return false, fmt.Errorf("failed to find where %q left %q: %w", root, base, err)
	}

	merged, _, err := readMergedTree(ctx, readMergedTreeParams{Dir: root, Left: base, Right: probe, MergeBase: mergeBase})
	if err != nil {
		return false, err
	}

	return merged == baseTree, nil
}

// Park records a checkout as it stands, tracked and untracked alike, so a move can be undone.
func (c *Client) Park(ctx context.Context, dir, ref string) (string, error) {
	root, err := c.GetRoot(ctx, dir)
	if err != nil {
		return "", err
	}

	head, err := Run(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("failed to read the head of %q to park it: %w", root, err)
	}

	tree, err := writeTree(ctx, root)
	if err != nil {
		return "", err
	}

	env := append(os.Environ(), snapshotIdentity...)
	sha, err := runEnv(ctx, root, env, "commit-tree", tree, "-p", head, "-m", parkMessage)
	if err != nil {
		return "", fmt.Errorf("failed to park %q: %w", root, err)
	}

	if err := c.HoldRef(ctx, root, ref, sha); err != nil {
		return "", err
	}

	return sha, nil
}
