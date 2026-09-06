package diff

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// snapshotMessage is what the round's commit says, which nobody reads but git insists on.
const snapshotMessage = "weave review round"

// snapshotIdentity is who the round's commit is by and when, the identity pinned because a
// repository with no user configured would refuse to write the commit at all, and the dates
// pinned because a commit carrying the clock would hash differently every time, so the same
// worktree taken twice would read as two rounds and lose every mark made against the first.
var snapshotIdentity = []string{
	"GIT_AUTHOR_NAME=weave",
	"GIT_AUTHOR_EMAIL=weave@localhost",
	"GIT_AUTHOR_DATE=@0 +0000",
	"GIT_COMMITTER_NAME=weave",
	"GIT_COMMITTER_EMAIL=weave@localhost",
	"GIT_COMMITTER_DATE=@0 +0000",
}

// SnapshotParams says which worktree to freeze, what to measure it against, and where to hold
// the result.
type SnapshotParams struct {
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

// ProbeTree writes an agent's worktree out as a tree and nothing else, which is the cheap way to
// ask whether it has moved since a round was taken, since the same working tree always hashes to
// the same tree and no commit object is left behind by the asking.
func ProbeTree(ctx context.Context, dir string) (string, error) {
	root, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("failed to find the repository at %q: %w", dir, err)
	}

	return writeTree(ctx, root)
}

// writeTree stages the whole worktree into an index of its own and hashes it, leaving the
// agent's index, worktree and branch exactly as they were.
func writeTree(ctx context.Context, root string) (string, error) {
	index, err := tempIndexPath()
	if err != nil {
		return "", err
	}
	defer removeIndex(index)

	env := append(append(os.Environ(), snapshotIdentity...), "GIT_INDEX_FILE="+index)

	if err := seedIndex(ctx, root, env); err != nil {
		return "", err
	}

	if _, err := runGitEnv(ctx, root, env, "add", "-A"); err != nil {
		return "", fmt.Errorf("failed to stage %q into a snapshot: %w", root, err)
	}

	tree, err := runGitEnv(ctx, root, env, "write-tree")
	if err != nil {
		return "", fmt.Errorf("failed to write a snapshot tree for %q: %w", root, err)
	}

	return tree, nil
}

// Take writes an agent's whole worktree, tracked and untracked alike, as a commit off the point
// its branch left the base, building the tree in an index of its own so that the agent's index,
// worktree and branch are all left exactly as they were.
func Take(ctx context.Context, params SnapshotParams) (*Snapshot, error) {
	root, err := runGit(ctx, params.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("failed to find the repository at %q: %w", params.Dir, err)
	}

	base := resolveBase(ctx, root, params.Base)

	baseSHA, err := runGit(ctx, root, "merge-base", base, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("failed to find where %q left %q: %w", root, base, err)
	}

	tree, err := writeTree(ctx, root)
	if err != nil {
		return nil, err
	}

	env := append(os.Environ(), snapshotIdentity...)

	sha, err := runGitEnv(ctx, root, env, "commit-tree", tree, "-p", baseSHA, "-m", snapshotMessage)
	if err != nil {
		return nil, fmt.Errorf("failed to commit a snapshot of %q: %w", root, err)
	}

	if params.Ref != "" {
		if _, err := runGitEnv(ctx, root, env, "update-ref", params.Ref, sha); err != nil {
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

// Hold points a ref at a snapshot already written, which is how a caller naming the ref after the
// commit takes hold of it without writing that commit twice.
func Hold(ctx context.Context, dir, ref, sha string) error {
	if ref == "" {
		return nil
	}

	env := append(os.Environ(), snapshotIdentity...)
	if _, err := runGitEnv(ctx, dir, env, "update-ref", ref, sha); err != nil {
		return fmt.Errorf("failed to hold the snapshot %q at %q: %w", sha, ref, err)
	}

	return nil
}

// Release drops the ref holding a snapshot, leaving the commit to be collected.
func Release(ctx context.Context, dir, ref string) error {
	if ref == "" {
		return nil
	}

	if _, err := runGit(ctx, dir, "update-ref", "-d", ref); err != nil {
		return fmt.Errorf("failed to release the snapshot at %q: %w", ref, err)
	}

	return nil
}

// tempIndexPath names a file git may write an index to, leaving nothing there, since git reads an
// existing empty file as a corrupt index rather than as a new one.
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

// seedIndex fills the temporary index from HEAD so only what the agent has touched has to be
// hashed, and is skipped in a repository with no HEAD to seed from, which is asked about rather
// than inferred from the failure, since a read-tree that fails for any other reason would leave
// every file HEAD holds staged as a deletion.
func seedIndex(ctx context.Context, root string, env []string) error {
	if _, err := runGitEnv(ctx, root, env, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return nil
	}

	if _, err := runGitEnv(ctx, root, env, "read-tree", "HEAD"); err != nil {
		return fmt.Errorf("failed to seed a snapshot index from HEAD in %q: %w", root, err)
	}

	return nil
}

// removeIndex drops the temporary index and the lock git leaves beside it when a write is cut
// short, neither of which the caller can act on a failure to remove.
func removeIndex(path string) {
	for _, leftover := range []string{path, path + ".lock"} {
		if err := os.Remove(leftover); err != nil {
			continue
		}
	}
}

func runGitEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = env

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(out)), nil
}

// FileAt is one file as a commit holds it, split into lines, which is what a released review
// quotes around each comment so the agent can see where it sits without opening anything, and
// which races nothing the agent is doing since the commit it reads never moves.
func FileAt(ctx context.Context, dir, sha, path string) ([]string, error) {
	out, err := runGitBytes(ctx, dir, "show", sha+":"+path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %q at %q: %w", path, sha, err)
	}
	if len(out) > maxFileBytes {
		return nil, nil
	}

	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), nil
}

// TreeOf is the tree one snapshot commit holds, which a probe of the worktree is compared against
// to say whether the agent has written anything since.
func TreeOf(ctx context.Context, dir, sha string) (string, error) {
	tree, err := runGit(ctx, dir, "rev-parse", sha+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("failed to read the tree of %q: %w", sha, err)
	}

	return tree, nil
}

// ChangedSince is how many files differ between two snapshots, which is what says how much of a
// round is new since the reader last gave a review.
func ChangedSince(ctx context.Context, dir, fromSHA, toSHA string, exclude []string) (int, error) {
	if fromSHA == "" || toSHA == "" || fromSHA == toSHA {
		return 0, nil
	}

	args := append([]string{
		"-c", "core.quotepath=false",
		"--no-pager", "diff", "--no-ext-diff", "--name-only",
		fromSHA, toSHA, "--", ".",
	}, excludePathspecs(exclude)...)

	out, err := runGit(ctx, dir, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to compare %q against %q: %w", fromSHA, toSHA, err)
	}
	if strings.TrimSpace(out) == "" {
		return 0, nil
	}

	return len(strings.Split(strings.TrimSpace(out), "\n")), nil
}
