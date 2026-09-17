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

// ErrHoldRefused is an agent that would not settle, so nobody may claim its worktree.
var ErrHoldRefused = fail.Precondition("the agent did not stop")

// ErrReleaseRefused is a release that could not tell the agent to carry on, so the hold stands.
var ErrReleaseRefused = fail.Precondition("the agent could not be told to carry on")

// ErrNotHeld is a release of an agent nobody was holding.
var ErrNotHeld = fail.Precondition("nobody is holding this agent")

// ErrAlreadyHeld is a hold on an agent somebody is already holding.
var ErrAlreadyHeld = fail.AlreadyExists("this agent is already held")

// ErrAgentHeld is anything else weave was asked to do to an agent a person is holding.
var ErrAgentHeld = fail.Precondition("this agent is held")

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
	// Wait is how the settle rests between readings, time.After when nothing else is given.
	Wait func(time.Duration) <-chan time.Time
}

// Holder stops an agent on a person's behalf and lets it go again.
type Holder struct {
	agents   holdStore
	channel  harness.Channel
	observer harness.Observer
	now      func() time.Time
	wait     func(time.Duration) <-chan time.Time
}

// NewHolder builds the holder over the store, the channel that reaches an agent and the observer
// that reads it.
func NewHolder(params NewHolderParams) *Holder {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	wait := params.Wait
	if wait == nil {
		wait = time.After
	}

	return &Holder{
		agents:   params.Agents,
		channel:  params.Channel,
		observer: params.Observer,
		now:      now,
		wait:     wait,
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
		return agentstore.Agent{}, ErrHoldRefused.Saying("%q is not in a pane", agent.Name)
	}

	target := buildTarget(agent)

	// Explainer: the stored state is a pane reading a few seconds old, so it says what the agent
	// was doing rather than what it is doing. Both the decision to interrupt and the reading that
	// confirms it have to come from the pane as it is now, or the hold is granted on the same
	// frame the interrupt was meant to clear.
	observed, err := h.observer.Observe(ctx, target)
	read := err == nil && (isMidTurn(observed.Activity) || isAtRest(observed.Activity))
	if !read {
		return agentstore.Agent{}, ErrHoldRefused.Saying("%q could not be read, so nothing here can say it stopped", agent.Name)
	}

	interrupted := isMidTurn(observed.Activity)
	if interrupted {
		if err := h.channel.Interrupt(ctx, target); err != nil {
			return agentstore.Agent{}, fmt.Errorf("failed to stop %q: %w", agent.Name, err)
		}

		// Explainer: the interrupt is keys typed into a terminal, so nothing about sending it says
		// the agent read it. What may claim the worktree is an agent observed to have come to rest
		// afterwards, which is why a prompt still on screen is not an answer.
		if err := h.settle(ctx, target, agent.Name); err != nil {
			return agentstore.Agent{}, err
		}
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

// settle waits for the agent to read as at rest, giving up once the budget is spent and saying
// which of the two things it gave up on.
func (h *Holder) settle(ctx context.Context, target harness.Target, name string) error {
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
			return ErrHoldRefused.Saying("%q %s", name, describeUnsettled(last))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.wait(holdSettleInterval):
		}
	}
}

// describeUnsettled says why the wait gave up.
func describeUnsettled(last harness.Activity) string {
	if last == harness.ActivityWorking || last == harness.ActivityWaiting {
		return "had not stopped after " + holdSettleBudget.String()
	}

	return "could not be read, so nothing here can say it stopped"
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
		// success. The hold is kept instead, which leaves the key meaning something and makes
		// pressing it again the whole recovery.
		if !said {
			return agentstore.Agent{}, ErrReleaseRefused.Saying(
				"%q could not be told to carry on, since its input box would not take the words", agent.Name)
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
