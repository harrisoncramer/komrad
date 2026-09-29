package agents

import (
	"context"
	"fmt"
	"time"

	"komrad/internal/fail"
	"komrad/internal/harness"
	agentstore "komrad/internal/store/agent"
)

// settleBudget is how long komrad waits for an interrupted agent to come to rest before it gives
// up.
const settleBudget = 8 * time.Second

// settleInterval is how often the pane is read while waiting.
const settleInterval = 250 * time.Millisecond

// ErrUnsettled is an agent that could not be seen to come to rest, so nothing may touch its
// worktree.
var ErrUnsettled = fail.Precondition("the agent did not stop")

// NewSettlerParams is what a settler is built over.
type NewSettlerParams struct {
	Channel  harness.Channel
	Observer harness.Observer
	Clock    func() time.Time
	// Wait is how the settle rests between readings, time.After when nothing else is given.
	Wait func(time.Duration) <-chan time.Time
}

// Settler stops an agent and says so only once its pane confirms it.
type Settler struct {
	channel  harness.Channel
	observer harness.Observer
	now      func() time.Time
	wait     func(time.Duration) <-chan time.Time
}

// NewSettler builds the settler over the channel that reaches an agent and the observer that
// reads it.
func NewSettler(params NewSettlerParams) *Settler {
	now := params.Clock
	if now == nil {
		now = time.Now
	}

	wait := params.Wait
	if wait == nil {
		wait = time.After
	}

	return &Settler{
		channel:  params.Channel,
		observer: params.Observer,
		now:      now,
		wait:     wait,
	}
}

// Settle reads the agent's pane as it is now, cuts a turn in flight, and waits for the agent to
// come to rest, reporting whether a turn was cut.
func (s *Settler) Settle(ctx context.Context, agent agentstore.Agent) (bool, error) {
	target := buildTarget(agent)

	observed, err := s.observer.Observe(ctx, target)
	read := err == nil && (isMidTurn(observed.Activity) || isAtRest(observed.Activity))
	if !read {
		return false, ErrUnsettled.Saying("%q could not be read, so nothing here can say it stopped", agent.Name)
	}

	if !isMidTurn(observed.Activity) {
		return false, nil
	}

	if err := s.channel.Interrupt(ctx, target); err != nil {
		return false, fmt.Errorf("failed to stop %q: %w", agent.Name, err)
	}

	if err := s.waitForRest(ctx, target, agent.Name); err != nil {
		return false, err
	}

	return true, nil
}

// waitForRest reads the pane until it says the agent has stopped, giving up once the budget is
// spent and saying which of the two things it gave up on.
func (s *Settler) waitForRest(ctx context.Context, target harness.Target, name string) error {
	deadline := s.now().Add(settleBudget)
	last := harness.ActivityUnknown

	for {
		observed, err := s.observer.Observe(ctx, target)
		if err == nil {
			if isAtRest(observed.Activity) {
				return nil
			}

			last = observed.Activity
		}

		if !s.now().Before(deadline) {
			return ErrUnsettled.Saying("%q %s", name, describeUnsettled(last))
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.wait(settleInterval):
		}
	}
}

// describeUnsettled says why the wait gave up.
func describeUnsettled(last harness.Activity) string {
	if last == harness.ActivityWorking || last == harness.ActivityWaiting {
		return "had not stopped after " + settleBudget.String()
	}

	return "could not be read, so nothing here can say it stopped"
}
