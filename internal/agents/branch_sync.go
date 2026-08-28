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

const branchReadTimeout = 5 * time.Second

type branchSetter interface {
	List(ctx context.Context) ([]agentstore.Agent, error)
	SetBranchByID(ctx context.Context, id string, branch *string) error
}

type BranchSyncer struct {
	agents branchSetter
}

// NewBranchSyncer builds a syncer over the agent store.
func NewBranchSyncer(agents branchSetter) *BranchSyncer {
	return &BranchSyncer{agents: agents}
}

// Sync reads the branch at every live agent's cwd and writes what changed, clearing the branch
// when the cwd names no git checkout.
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

func equalBranch(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}

	return *a == *b
}
