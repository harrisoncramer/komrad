package agents

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/store"
)

type PaneInterrupterParams struct {
	Agents    agentGetter
	Harnesses harnessGetter
	Panes     paneWriter
}

// PaneInterrupter stops an agent where it stands by typing whatever its own CLI reads as an
// interrupt into its pane.
type PaneInterrupter struct {
	agents    agentGetter
	harnesses harnessGetter
	panes     paneWriter
}

// NewPaneInterrupter builds the interrupter over the registry and the tmux server.
func NewPaneInterrupter(params PaneInterrupterParams) *PaneInterrupter {
	return &PaneInterrupter{
		agents:    params.Agents,
		harnesses: params.Harnesses,
		panes:     params.Panes,
	}
}

// Interrupt sends the agent's harness the keys that stop it.
func (i *PaneInterrupter) Interrupt(ctx context.Context, agentID string) error {
	agent, err := i.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}
	if err != nil {
		return err
	}
	if agent.Pane == "" {
		return fmt.Errorf("agent %q is not in a pane to interrupt", agentID)
	}

	running, err := i.harnesses.GetByID(agent.Harness)
	if err != nil {
		return fmt.Errorf("failed to find the harness of agent %q: %w", agentID, err)
	}

	keys := running.GetInterruptKeys()
	if len(keys) == 0 {
		return fmt.Errorf("the %q harness has no keys that interrupt it", agent.Harness)
	}

	if err := i.panes.SendKeys(agent.Pane, keys...); err != nil {
		return fmt.Errorf("failed to interrupt agent %q: %w", agentID, err)
	}

	return nil
}
