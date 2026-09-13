package agents

import (
	"context"
	"errors"
	"fmt"
	"time"

	"weave/internal/harness"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
)

// ErrHoldRefused is an agent that would not settle, so nobody may claim its worktree.
var ErrHoldRefused = errors.New("the agent did not stop")

// ErrNotHeld is a release of an agent nobody was holding.
var ErrNotHeld = errors.New("nobody is holding this agent")

// ErrAlreadyHeld is a hold on an agent somebody is already holding.
var ErrAlreadyHeld = errors.New("this agent is already held")

// ErrAgentHeld is anything else weave was asked to do to an agent a person is holding.
var ErrAgentHeld = errors.New("this agent is held")

// holdSettleBudget is how long weave waits for an interrupted agent to come to rest before it
// gives the hold up.
const holdSettleBudget = 8 * time.Second

// holdSettleInterval is how often the pane is read while waiting.
const holdSettleInterval = 250 * time.Millisecond

type holdStore interface {
	agentGetter
	Hold(ctx context.Context, params agentstore.HoldParams) (bool, error)
	Release(ctx context.Context, id string) (bool, error)
}

type NewHolderParams struct {
	Agents   holdStore
	Channel  harness.Channel
	Observer harness.Observer
	Clock    func() time.Time
}

// Holder stops an agent on a person's behalf and lets it go again.
type Holder struct {
	agents   holdStore
	channel  harness.Channel
	observer harness.Observer
	now      func() time.Time
}

// NewHolder builds the holder over the store, the channel that reaches an agent and the observer
// that reads it.
func NewHolder(params NewHolderParams) *Holder {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	return &Holder{
		agents:   params.Agents,
		channel:  params.Channel,
		observer: params.Observer,
		now:      now,
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
		return agentstore.Agent{}, fmt.Errorf("%w: %q", ErrAlreadyHeld, agent.Name)
	}

	if agent.Pane == "" {
		return agentstore.Agent{}, fmt.Errorf("%w: %q is not in a pane", ErrHoldRefused, agent.Name)
	}

	target := buildTarget(agent)
	midTurn := agent.State.IsTurnUnfinished()

	if midTurn {
		if err := h.channel.Interrupt(ctx, target); err != nil {
			return agentstore.Agent{}, fmt.Errorf("failed to stop %q: %w", agent.Name, err)
		}
	}

	// Explainer: the interrupt is keys typed into a terminal, so nothing about sending it says the
	// agent read it. What may claim the worktree is an agent observed to have come to rest, which
	// is why this waits and why a pane that cannot be read refuses rather than waits longer.
	if err := h.settle(ctx, target); err != nil {
		return agentstore.Agent{}, fmt.Errorf("%w: %q", err, agent.Name)
	}

	held, err := h.agents.Hold(ctx, agentstore.HoldParams{
		ID:      agent.ID,
		At:      h.now(),
		MidTurn: midTurn,
	})
	if err != nil {
		return agentstore.Agent{}, err
	}
	if !held {
		return agentstore.Agent{}, fmt.Errorf("%w: %q", ErrAlreadyHeld, agent.Name)
	}

	return h.get(ctx, agentID)
}

// settle waits for the agent to read as at rest, giving up once the budget is spent and saying
// which of the two things it gave up on.
func (h *Holder) settle(ctx context.Context, target harness.Target) error {
	deadline := h.now().Add(holdSettleBudget)
	last := harness.ActivityUnknown

	for {
		observed, err := h.observer.Observe(ctx, target)
		if err == nil {
			if isAtRest(observed.Activity) {
				return nil
			}

			last = observed.Activity
		}

		if !h.now().Before(deadline) {
			return fmt.Errorf("%w: %s", ErrHoldRefused, describeUnsettled(last))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(holdSettleInterval):
		}
	}
}

// describeUnsettled says why the wait gave up, since a pane nobody can read and an agent that
// kept working are different answers to the person holding the key down.
func describeUnsettled(last harness.Activity) string {
	if last == harness.ActivityWorking {
		return "it was still working after " + holdSettleBudget.String()
	}

	return "its pane could not be read, so nothing here can say it stopped"
}

// Release lets go of an agent, typing it back into motion when the hold cut a turn short and
// saying nothing at all when it had already finished.
func (h *Holder) Release(ctx context.Context, params ReleaseParams) (agentstore.Agent, error) {
	agent, err := h.get(ctx, params.AgentID)
	if err != nil {
		return agentstore.Agent{}, err
	}

	if agent.HeldAt == nil {
		return agentstore.Agent{}, fmt.Errorf("%w: %q", ErrNotHeld, agent.Name)
	}

	// Explainer: an agent that finished its own turn before the hold landed has nothing to carry
	// on with, so typing at it would start work nobody asked for. Only a turn this hold cut short
	// is one the agent was in the middle of.
	if agent.HeldMidTurn && params.Said != "" && agent.Pane != "" {
		if _, err := h.channel.Send(ctx, buildTarget(agent), params.Said); err != nil {
			return agentstore.Agent{}, fmt.Errorf("failed to say what changed to %q: %w", agent.Name, err)
		}
	}

	released, err := h.agents.Release(ctx, agent.ID)
	if err != nil {
		return agentstore.Agent{}, err
	}
	if !released {
		return agentstore.Agent{}, fmt.Errorf("%w: %q", ErrNotHeld, agent.Name)
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

// isAtRest reports whether a reading says the agent has stopped writing, which a box holding
// somebody's unsent text and a prompt waiting on an answer both do.
func isAtRest(activity harness.Activity) bool {
	switch activity {
	case harness.ActivityIdle, harness.ActivityComposing, harness.ActivityWaiting:
		return true
	case harness.ActivityWorking, harness.ActivityUnrecognized, harness.ActivityUnknown:
		return false
	}

	return false
}
