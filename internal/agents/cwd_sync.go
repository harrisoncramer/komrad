package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	agentstore "weave/internal/store/agent"
)

// cwdLookTimeout bounds each look at a directory, since a dead network mount hangs rather than
// failing.
const cwdLookTimeout = 5 * time.Second

type cwdSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetBranchByID(ctx context.Context, id string, branch *string) error
	SetWorktreeRemovedByID(ctx context.Context, id string, removed bool) error
}

// branchGetter is the branch a checkout is on.
type branchGetter interface {
	GetCurrentBranch(ctx context.Context, dir string) *string
}

// CwdSyncer keeps what the panel shows of an agent's working directory in step with the
// directory itself.
type CwdSyncer struct {
	agents   cwdSetter
	branches branchGetter
}

// NewCwdSyncer builds a syncer over the agent store.
func NewCwdSyncer(agents cwdSetter, branches branchGetter) *CwdSyncer {
	return &CwdSyncer{agents: agents, branches: branches}
}

// Sync looks at every live agent's cwd once and writes what changed, clearing the branch when
// the cwd names no git checkout and leaving an agent with no cwd alone.
func (cs *CwdSyncer) Sync(ctx context.Context) error {
	agents, err := cs.agents.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list agents for cwd sync: %w", err)
	}

	var failures []error
	for _, agent := range agents {
		removed := isCwdRemoved(ctx, agent.Cwd)

		// A directory that is gone has no branch, so the git call is skipped rather than
		// run against it and waited out.
		var branch *string
		if !removed {
			branch = cs.readBranch(ctx, agent.Cwd)
		}

		if removed != agent.WorktreeRemoved {
			if err := cs.agents.SetWorktreeRemovedByID(ctx, agent.ID, removed); err != nil {
				failures = append(failures, fmt.Errorf("failed to set worktree_removed for %q: %w", agent.Name, err))
			}
		}

		if !isSameBranch(agent.Branch, branch) {
			if err := cs.agents.SetBranchByID(ctx, agent.ID, branch); err != nil {
				failures = append(failures, fmt.Errorf("failed to set branch for %q: %w", agent.Name, err))
			}
		}
	}

	return errors.Join(failures...)
}

func isCwdRemoved(ctx context.Context, dir string) bool {
	if dir == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
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

func (cs *CwdSyncer) readBranch(ctx context.Context, dir string) *string {
	if dir == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, cwdLookTimeout)
	defer cancel()

	return cs.branches.GetCurrentBranch(ctx, dir)
}

func isSameBranch(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}
