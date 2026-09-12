package agents

import (
	"context"
	"errors"
	"fmt"

	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

// targetOf names an agent the way a channel or an observer reaches it.
func targetOf(agent agentstore.Agent) harness.Target {
	return harness.Target{
		AgentID:   agent.ID,
		Pane:      agent.Pane,
		HarnessID: agent.Harness,
	}
}

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

	return i.channel.Interrupt(ctx, targetOf(*agent))
}
