package agents

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/harness"
	"weave/internal/store"
)

type NewInterrupterParams struct {
	Agents  agentGetter
	Channel harness.Channel
}

// Interrupter stops an agent where it stands.
type Interrupter struct {
	agents  agentGetter
	channel harness.Channel
}

// NewInterrupter builds the interrupter over the store and the channel reaching the agent.
func NewInterrupter(params NewInterrupterParams) *Interrupter {
	return &Interrupter{
		agents:  params.Agents,
		channel: params.Channel,
	}
}

// Interrupt asks the agent's channel to stop it.
func (i *Interrupter) Interrupt(ctx context.Context, agentID string) error {
	agent, err := i.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}
	if err != nil {
		return err
	}

	return i.channel.Interrupt(ctx, buildTarget(*agent))
}
