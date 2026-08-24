package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	agentstore "weave/internal/store/agent"
)

const worktreeStatTimeout = 5 * time.Second

type worktreeRemovedSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetWorktreeRemovedByID(ctx context.Context, id string, removed bool) error
}

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
