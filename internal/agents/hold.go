package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/fail"
	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

// ErrReleaseRefused is a release that could not tell the agent to carry on, so the hold stands.
var ErrReleaseRefused = fail.Precondition("the agent could not be told to carry on")

// ErrNotHeld is a release of an agent nobody was holding.
var ErrNotHeld = fail.Precondition("nobody is holding this agent")

// ErrAlreadyHeld is a hold on an agent somebody is already holding.
var ErrAlreadyHeld = fail.AlreadyExists("this agent is already held")

// ErrHoldSelf is an agent holding itself, which would stop it with nobody who knows to let it go.
var ErrHoldSelf = fail.InvalidArgument("an agent cannot hold itself")

// ErrAgentHeld is anything else weave was asked to do to an agent a person is holding.
var ErrAgentHeld = fail.Precondition("this agent is held")

type holdStore interface {
	agentGetter
	Hold(ctx context.Context, params agentstore.HoldParams) (bool, error)
	Release(ctx context.Context, id string) (bool, error)
}

type NewHolderParams struct {
	Agents  holdStore
	Channel harness.Channel
	Settler *Settler
	Clock   func() time.Time
}

// Holder stops an agent on a person's behalf and lets it go again.
type Holder struct {
	agents  holdStore
	channel harness.Channel
	settler *Settler
	now     func() time.Time
}

// NewHolder builds the holder over the store, the channel that reaches an agent and the settler
// that stops it.
func NewHolder(params NewHolderParams) *Holder {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	return &Holder{
		agents:  params.Agents,
		channel: params.Channel,
		settler: params.Settler,
		now:     now,
	}
}

// Hold stops an agent and records that the person has it, refusing unless the agent is observed to
// come to rest.
func (h *Holder) Hold(ctx context.Context, agentID string) (agentstore.Agent, error) {
	agent, err := h.get(ctx, agentID)
	if err != nil {
		return agentstore.Agent{}, err
	}

	if agent.HeldAt != nil {
		return agentstore.Agent{}, ErrAlreadyHeld.Saying("%q is already paused", agent.Name)
	}

	if agent.Pane == "" {
		return agentstore.Agent{}, ErrUnsettled.Saying("%q is not in a pane", agent.Name)
	}

	interrupted, err := h.settler.Settle(ctx, agent)
	if err != nil {
		return agentstore.Agent{}, err
	}

	held, err := h.agents.Hold(ctx, agentstore.HoldParams{
		ID:      agent.ID,
		At:      h.now(),
		MidTurn: interrupted,
	})
	if err != nil {
		return agentstore.Agent{}, err
	}
	if !held {
		return agentstore.Agent{}, ErrAlreadyHeld.Saying("%q is already paused", agent.Name)
	}

	return h.get(ctx, agentID)
}

// Release lets go of an agent, typing it back into motion when the hold cut a turn short and
// saying nothing at all when it had already finished.
func (h *Holder) Release(ctx context.Context, params ReleaseParams) (agentstore.Agent, error) {
	agent, err := h.get(ctx, params.AgentID)
	if err != nil {
		return agentstore.Agent{}, err
	}

	if agent.HeldAt == nil {
		return agentstore.Agent{}, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}

	// Explainer: an agent that finished its own turn before the hold landed has nothing to carry
	// on with, so typing at it would start work nobody asked for. Only a turn this hold cut short
	// is one the agent was in the middle of.
	if agent.HeldMidTurn && params.Said != "" && agent.Pane != "" {
		said, err := h.channel.Send(ctx, buildTarget(agent), params.Said)
		if err != nil {
			return agentstore.Agent{}, fmt.Errorf("failed to say what changed to %q: %w", agent.Name, err)
		}

		// Explainer: a box that would not take the words leaves an agent that was interrupted with
		// nothing to carry on from, so letting go here would stop it for good and call that
		// success. The hold is kept instead, and a box that will never take them, one drawing more
		// than a single line, is left to a release that asks for silence.
		if !said {
			return agentstore.Agent{}, ErrReleaseRefused.Saying(
				"%q could not be told to carry on, since its input box would not take the words, so unpause it in silence to let it go regardless", agent.Name)
		}
	}

	released, err := h.agents.Release(ctx, agent.ID)
	if err != nil {
		return agentstore.Agent{}, err
	}
	if !released {
		return agentstore.Agent{}, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}

	return h.get(ctx, params.AgentID)
}

// ReleaseParams is the agent being let go and what to tell it happened while it was held.
type ReleaseParams struct {
	AgentID string
	// Said is typed into the agent as it starts again, empty to let it go in silence.
	Said string
}

func (h *Holder) get(ctx context.Context, agentID string) (agentstore.Agent, error) {
	agent, err := h.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return agentstore.Agent{}, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}
	if err != nil {
		return agentstore.Agent{}, err
	}

	return *agent, nil
}
