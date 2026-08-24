package agents

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	agentstore "weave/internal/store/agent"
)

// branchReadTimeout bounds one git rev-parse per agent, since this runs cooperatively with
// every other agent's own pass and a single hung process must not stall all of them.
const branchReadTimeout = 5 * time.Second

// branchSetter is the agent store as BranchSyncer needs it: every live agent to walk, and
// where it writes back what it found.
type branchSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetBranchByID(ctx context.Context, id string, branch *string) error
}

// BranchSyncer keeps each live agent's branch column in step with whatever git says its cwd
// is on. It runs as its own slow pass rather than inside the two-second reconcile loop,
// since discovering a branch is a shell-out and that loop's own cost is proportional to
// tmux captures rather than to processes it starts.
//
// Nothing here needs a generated worktree: any cwd git recognises gets a branch, a
// hand-written setup action or none at all.
type BranchSyncer struct {
	agents branchSetter
}

// NewBranchSyncer builds a syncer over the agent store.
func NewBranchSyncer(agents branchSetter) *BranchSyncer {
	return &BranchSyncer{agents: agents}
}

// Sync reads the branch at every live agent's cwd and writes what changed. An agent whose
// cwd names no git checkout is not an error, since most workflows are not worktrees; its
// branch is simply cleared if it carried one from a cwd that has since changed.
func (bs *BranchSyncer) Sync(ctx context.Context) error {
	agents, err := bs.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for branch sync: %w", err)
	}

	var failures []error
	for _, agent := range agents {
		branch := readBranch(ctx, agent.Cwd)
		if equalBranch(agent.Branch, branch) {
			continue
		}

		if err := bs.agents.SetBranchByID(ctx, agent.ID, branch); err != nil {
			failures = append(failures, fmt.Errorf("failed to set branch for %q: %w", agent.Name, err))
		}
	}

	return errors.Join(failures...)
}

// readBranch is the branch checked out at dir, nil when dir names no directory, is not a
// git checkout, or is on no named branch at all, a detached HEAD.
func readBranch(ctx context.Context, dir string) *string {
	if dir == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, branchReadTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return nil
	}

	branch := strings.TrimSpace(string(out))
	if branch == "" || branch == "HEAD" {
		return nil
	}

	return &branch
}

// equalBranch compares two branch pointers by value, both nil counting as equal, so a pass
// that found nothing new does not write for its own sake.
func equalBranch(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}
