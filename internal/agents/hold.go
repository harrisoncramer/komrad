package agents

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/fail"
	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	"weave/internal/watch"
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

// NewHolderParams is what a holder is built over.
type NewHolderParams struct {
	Agents  holdStore
	Channel harness.Channel
	Settler *Settler
	Clock   func() time.Time
	// Watch is told which rows moved, watch.Discard when unset.
	Watch watch.Publisher
}

// Holder stops an agent on a person's behalf and lets it go again.
type Holder struct {
	agents  holdStore
	channel harness.Channel
	settler *Settler
	now     func() time.Time
	watch   watch.Publisher
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
		watch:   cmp.Or(params.Watch, watch.Discard),
	}
}

// Hold stops an agent and records that the person has it, refusing unless the agent is observed to
// come to rest.
func (h *Holder) Hold(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := h.get(ctx, agentID)
	if err != nil {
		return nil, err
	}

	if agent.HeldAt != nil {
		return nil, ErrAlreadyHeld.Saying("%q is already paused", agent.Name)
	}

	if agent.Pane == "" {
		return nil, ErrUnsettled.Saying("%q is not in a pane", agent.Name)
	}

	interrupted, err := h.settler.Settle(ctx, *agent)
	if err != nil {
		return nil, err
	}

	held, err := h.agents.Hold(ctx, agentstore.HoldParams{
		ID:      agent.ID,
		At:      h.now(),
		MidTurn: interrupted,
	})
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, ErrAlreadyHeld.Saying("%q is already paused", agent.Name)
	}
	h.watch.TouchAgents(agent.ID)

	return h.get(ctx, agentID)
}

// ReleaseParams is the agent being let go and what to tell it happened while it was held.
type ReleaseParams struct {
	AgentID string
	// Said is typed into the agent as it starts again, empty to let it go in silence.
	Said string
}

// Release lets go of an agent, typing what happened into it only when the hold cut a turn short,
// and keeping the hold when the agent's input box would not take the words.
func (h *Holder) Release(ctx context.Context, params ReleaseParams) (*agentstore.Agent, error) {
	agent, err := h.get(ctx, params.AgentID)
	if err != nil {
		return nil, err
	}

	if agent.HeldAt == nil {
		return nil, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}

	if agent.HeldMidTurn && params.Said != "" && agent.Pane != "" {
		said, err := h.channel.Send(ctx, buildTarget(*agent), params.Said)
		if err != nil {
			return nil, fmt.Errorf("failed to say what changed to %q: %w", agent.Name, err)
		}
		if !said {
			return nil, ErrReleaseRefused.Saying(
				"%q could not be told to carry on, since its input box would not take the words, so unpause it in silence to let it go regardless", agent.Name)
		}
	}

	released, err := h.agents.Release(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	if !released {
		return nil, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}
	h.watch.TouchAgents(agent.ID)

	return h.get(ctx, params.AgentID)
}

func (h *Holder) get(ctx context.Context, agentID string) (*agentstore.Agent, error) {
	agent, err := h.agents.GetByID(ctx, agentID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrAgentNotFound, agentID)
	}
	if err != nil {
		return nil, err
	}

	return agent, nil
}
