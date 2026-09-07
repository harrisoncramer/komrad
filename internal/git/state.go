package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrDetachedHead is a worktree whose HEAD is not on a branch, refused rather than frozen since a
// round taken there is a picture of whatever commit somebody parked on rather than of the agent's
// own work.
var ErrDetachedHead = errors.New("the worktree is on a detached head rather than on a branch")

// ErrOperationInProgress is a worktree part-way through a rebase, merge, cherry-pick, revert or
// bisect, whose files are that operation's working state rather than anything the agent means to
// show anybody.
var ErrOperationInProgress = errors.New("the worktree is part-way through a git operation")

// interruptedOperations are the files git leaves in its directory while an operation that can be
// resumed is still open, each with the name the reader is told about.
var interruptedOperations = []struct {
	path string
	name string
}{
	{path: "rebase-merge", name: "a rebase"},
	{path: "rebase-apply", name: "a rebase or an am"},
	{path: "MERGE_HEAD", name: "a merge"},
	{path: "CHERRY_PICK_HEAD", name: "a cherry-pick"},
	{path: "REVERT_HEAD", name: "a revert"},
	{path: "BISECT_LOG", name: "a bisect"},
}

// requireSnapshottable refuses a worktree that is not sitting on a branch with nothing in flight,
// since both of those hash to a tree that says nothing about what the agent has written.
func requireSnapshottable(ctx context.Context, root string) error {
	if operation := findInterruptedOperation(ctx, root); operation != "" {
		return fmt.Errorf("%w: %s is still open in %q", ErrOperationInProgress, operation, root)
	}

	if GetCurrentBranch(ctx, root) == nil {
		return fmt.Errorf("%w: %q", ErrDetachedHead, root)
	}

	return nil
}

// findInterruptedOperation names the operation a worktree is part-way through, and nothing for one
// with nothing in flight, reading the git directory of this worktree rather than the common one
// since a linked worktree keeps its own operation state.
func findInterruptedOperation(ctx context.Context, root string) string {
	gitDir, err := Run(ctx, root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return ""
	}

	for _, operation := range interruptedOperations {
		if _, err := os.Stat(filepath.Join(gitDir, operation.path)); err == nil {
			return operation.name
		}
	}

	return ""
}
