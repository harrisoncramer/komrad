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

	if i.isAtRest(ctx, target) {
		return nil
	}

	return i.channel.Interrupt(ctx, target)
}

// Explainer: the keys that stop a CLI mid-turn are keys its input box reads for something else
// when no turn is running, and Claude's are its checkpoint shortcut. Sending them to an agent at
// rest puts a dialog on screen that nobody is at the pane to dismiss, and every later delivery is
// refused for as long as it stands. A pane that could not be read is taken to be working, since
// a rebase moves files under whatever is there.

// isAtRest reports whether the pane says there is no turn to cut.
func (i *Interrupter) isAtRest(ctx context.Context, target harness.Target) bool {
	if i.observer == nil {
		return false
	}

	observed, err := i.observer.Observe(ctx, target)
	if err != nil {
		return false
	}

	return isAtRest(observed.Activity)
}
