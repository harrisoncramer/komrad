package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	agentstore "weave/internal/store/agent"
)

// worktreeStatTimeout bounds one os.Stat per agent, since this runs cooperatively with
// every other agent's own pass and a stale NFS or FUSE mount must not stall all of them.
const worktreeStatTimeout = 5 * time.Second

// worktreeRemovedSetter is the agent store as WorktreeSyncer needs it: every live agent to
// walk, and where it writes back what it found.
type worktreeRemovedSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetWorktreeRemovedByID(ctx context.Context, id string, removed bool) error
}

// WorktreeSyncer keeps each live agent's worktree_removed column in step with whether its
// cwd still exists on disk. It runs as its own slow pass rather than inside the two-second
// reconcile loop, alongside the branch syncer, since a directory can vanish under a running
// agent at any time and the panel otherwise has no way to tell that apart from one that was
// simply never given a worktree.
type WorktreeSyncer struct {
	agents worktreeRemovedSetter
}

// NewWorktreeSyncer builds a syncer over the agent store.
func NewWorktreeSyncer(agents worktreeRemovedSetter) *WorktreeSyncer {
	return &WorktreeSyncer{agents: agents}
}

// Sync stats every live agent's cwd and writes what changed. A blank cwd is not reported
// removed, since most workflows never set one.
func (ws *WorktreeSyncer) Sync(ctx context.Context) error {
	agents, err := ws.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for worktree sync: %w", err)
	}

	var failures []error
	for _, agent := range agents {
		removed := isCwdRemoved(ctx, agent.Cwd)
		if removed == agent.WorktreeRemoved {
			continue
		}

		if err := ws.agents.SetWorktreeRemovedByID(ctx, agent.ID, removed); err != nil {
			failures = append(failures, fmt.Errorf("failed to set worktree_removed for %q: %w", agent.Name, err))
		}
	}

	return errors.Join(failures...)
}

// isCwdRemoved reports whether dir names a directory that used to exist but no longer does.
// A blank dir is not removed, since no worktree was ever set, and an error other than "not
// exist" is not treated as removed either, since it says nothing about whether dir is gone.
// The stat runs in its own goroutine so a mount that never answers times out rather than
// blocking the rest of the pass.
func isCwdRemoved(ctx context.Context, dir string) bool {
	if dir == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, worktreeStatTimeout)
	defer cancel()

	statErr := make(chan error, 1)
	go func() {
		_, err := os.Stat(dir)
		statErr <- err
	}()

	select {
	case err := <-statErr:
		return errors.Is(err, os.ErrNotExist)
	case <-ctx.Done():
		return false
	}
}
