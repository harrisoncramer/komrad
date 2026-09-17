package agents

import (
	"context"
	"errors"

	"weave/internal/harness"
	"weave/internal/store"
)

type NewInterrupterParams struct {
	Agents   agentGetter
	Channel  harness.Channel
	Observer harness.Observer
}

// Interrupter stops an agent where it stands.
type Interrupter struct {
	agents   agentGetter
	channel  harness.Channel
	observer harness.Observer
}

// NewInterrupter builds the interrupter over the store, the channel reaching the agent and the
// observer that reads it.
func NewInterrupter(params NewInterrupterParams) *Interrupter {
	return &Interrupter{
		agents:   params.Agents,
		channel:  params.Channel,
		observer: params.Observer,
	}
}

// Interrupt asks the agent's channel to stop it.
func (i *Interrupter) Interrupt(ctx context.Context, agentID string) error {
	agent, err := i.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrAgentNotFound.Saying("no agent with id %q", agentID)
	}
	if err != nil {
		return err
	}

	target := buildTarget(*agent)

	if !i.hasTurnToCut(ctx, target) {
		return nil
	}

	return i.channel.Interrupt(ctx, target)
}

// Explainer: a capture that failed outright is no evidence either way, and a rebase is about to
// move files under whatever is on screen, so that one case sends the keys anyway.

// hasTurnToCut reports whether the pane says a turn is running.
func (i *Interrupter) hasTurnToCut(ctx context.Context, target harness.Target) bool {
	observed, err := i.observer.Observe(ctx, target)
	if err != nil {
		return true
	}

	return isMidTurn(observed.Activity)
}
