package agents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"komrad/internal/fail"
	"komrad/internal/harness"
	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	"komrad/lib/xptr"
)

// ErrNotHeld is a release of an agent nobody was holding.
var ErrNotHeld = fail.Precondition("nobody is holding this agent")

// ErrAlreadyHeld is a hold on an agent somebody is already holding.
var ErrAlreadyHeld = fail.AlreadyExists("this agent is already held")

// ErrHoldSelf is an agent holding itself, which would stop it with nobody who knows to let it go.
var ErrHoldSelf = fail.InvalidArgument("an agent cannot hold itself")

// ErrAgentHeld is anything else komrad was asked to do to an agent a person is holding.
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

// HoldParams is the agent being held and the agent holding it.
type HoldParams struct {
	AgentID string
	// ByAgentID is the agent taking the hold, empty for the panel.
	ByAgentID string
}

// Hold stops an agent and records who has it, refusing unless the agent is observed to come to rest.
func (h *Holder) Hold(ctx context.Context, params HoldParams) (*agentstore.Agent, error) {
	agent, err := h.get(ctx, params.AgentID)
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
		ID:        agent.ID,
		At:        h.now(),
		MidTurn:   interrupted,
		ByAgentID: xptr.NonEmpty(params.ByAgentID),
	})
	if err != nil {
		return nil, err
	}
	if !held {
		return nil, ErrAlreadyHeld.Saying("%q is already paused", agent.Name)
	}

	return h.get(ctx, params.AgentID)
}

// ReleaseParams is the agent being let go and what to tell it happened while it was held.
type ReleaseParams struct {
	AgentID string
	// Said is typed into the agent as it starts again, empty to let it go in silence.
	Said string
}

// Release lets go of an agent, typing what happened into it only when the hold cut a turn short.
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
		if err != nil || !said {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "released a held agent without telling it what changed",
				append([]slog.Attr{slog.String(logging.AgentID, agent.ID)}, logging.Err(err)...)...)
		}
	}

	released, err := h.agents.Release(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	if !released {
		return nil, ErrNotHeld.Saying("nobody is holding %q", agent.Name)
	}

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
