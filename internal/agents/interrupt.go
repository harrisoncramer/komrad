package agents

import (
	"context"
	"fmt"
	"time"

	"weave/internal/harness"
)

// interruptSettleFor is how long the agent is given to act on the interrupt keys before
// anything else is typed at it, so what follows lands in a composer rather than in whatever
// was still on screen.
const interruptSettleFor = 200 * time.Millisecond

// interruptible is the harnesses that can be stopped where they stand. It is asked of a
// harness rather than required of every one, since a harness with no interrupt of its own is a
// harness the panel simply does not interrupt.
type interruptible interface {
	GetInterruptKeys() []string
}

// PaneInterrupter stops an agent by typing its harness's interrupt keys into its pane. It is
// the adapter between wanting an agent to stop and the three lookups that takes, so a service
// that only needs the wanting depends on Interrupt alone.
type PaneInterrupter struct {
	agents    agentGetter
	harnesses harnessGetter
	panes     paneTyper
}

type PaneInterrupterParams struct {
	Agents    agentGetter
	Harnesses harnessGetter
	Panes     paneTyper
}

func NewPaneInterrupter(params PaneInterrupterParams) *PaneInterrupter {
	return &PaneInterrupter{agents: params.Agents, harnesses: params.Harnesses, panes: params.Panes}
}

// Interrupt sends whatever the agent's CLI reads as an interrupt, and waits for it to settle.
// A dormant agent, or a harness with nothing to send, is not an error: there is nothing
// running to stop.
func (p *PaneInterrupter) Interrupt(ctx context.Context, agentID string) error {
	agent, err := p.agents.GetByID(ctx, agentID)
	if err != nil {
		return err
	}
	if agent == nil || agent.Pane == "" {
		return nil
	}

	running, err := p.harnesses.GetByID(agent.Harness)
	if err != nil {
		return err
	}

	keys := interruptKeysFor(running)
	if len(keys) == 0 {
		return nil
	}

	if err := p.panes.SendKeys(agent.Pane, keys...); err != nil {
		return fmt.Errorf("failed to interrupt agent %q: %w", agentID, err)
	}

	time.Sleep(interruptSettleFor)

	return nil
}

func interruptKeysFor(running harness.Harness) []string {
	stoppable, ok := running.(interruptible)
	if !ok {
		return nil
	}

	return stoppable.GetInterruptKeys()
}
