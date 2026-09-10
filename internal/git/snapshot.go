package git

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Explainer: a park is a commit parented on the head it was taken at, holding the whole
// worktree as its tree. The parent keeps the commits an operation drops reachable and the tree
// keeps the files it never committed, neither of which the reflog covers: that holds refs
// rather than content, and expires. It is built through writeTree, so the agent's own index,
// worktree and branch are left as they were and nothing is rehashed.

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

// ProbeTree writes an agent's worktree out as a tree and nothing else, which is the cheap way
// to ask whether it has moved since a round was taken.
func ProbeTree(ctx context.Context, dir string) (string, error) {
	root, err := GetRoot(ctx, dir)
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
	if !hasAssumeUnchanged(ctx, root) {
		if tree, err := writeTreeSeeded(ctx, root, copyIndex); err == nil {
			return tree, nil
		}
	}

	return writeTreeSeeded(ctx, root, seedIndexFromHead)
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
func TakeSnapshot(ctx context.Context, params TakeSnapshotParams) (*Snapshot, error) {
	root, err := GetRoot(ctx, params.Dir)
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
func HoldRef(ctx context.Context, dir, ref, sha string) error {
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
func ReleaseRef(ctx context.Context, dir, ref string) error {
	if ref == "" {
		return nil
	}

	if _, err := Run(ctx, dir, "update-ref", "-d", ref); err != nil {
		return fmt.Errorf("failed to release the snapshot at %q: %w", ref, err)
	}

	return nil
}

// tempIndexPath names a file git may write an index to, leaving nothing there, since git reads
// an existing empty file as a corrupt index.
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

// Explainer: an index seeded from a commit carries no file times, sizes or inodes, so staging the
// worktree into it makes git hash every tracked file again. Seeding it from the agent's own index
// carries those times over, and git then hashes only the files whose times or sizes have moved,
// which measured 0.12s against 2.2s on a repository of fourteen thousand files. The agent's index
// is not always the better answer, so the two ways of filling one both stay.

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
	// The git directory is asked for rather than the index path, which an inherited
	// GIT_INDEX_FILE would answer with instead.
	dir, err := Run(ctx, root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return fmt.Errorf("failed to find the index of %q: %w", root, err)
	}

	from, err := os.Open(filepath.Join(dir, "index"))
	if err != nil {
		return fmt.Errorf("failed to read the index of %q: %w", root, err)
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

// hasAssumeUnchanged says whether the agent's index promises git that some file will not be
// edited, and says so of an index it cannot read.
func hasAssumeUnchanged(ctx context.Context, root string) bool {
	out, err := RunBytes(ctx, root, "ls-files", "-v")
	if err != nil {
		return true
	}

	for line := range bytes.SplitSeq(out, []byte("\n")) {
		if len(line) > 0 && line[0] >= 'a' && line[0] <= 'z' {
			return true
		}
	}

	return false
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
func GetTree(ctx context.Context, dir, sha string) (string, error) {
	tree, err := Run(ctx, dir, "rev-parse", sha+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("failed to read the tree of %q: %w", sha, err)
	}

	return tree, nil
}

// Park records a checkout as it stands, tracked and untracked alike, so a move can be undone.
func Park(ctx context.Context, dir, ref string) (string, error) {
	root, err := GetRoot(ctx, dir)
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

	if err := HoldRef(ctx, root, ref, sha); err != nil {
		return "", err
	}

	return sha, nil
}
