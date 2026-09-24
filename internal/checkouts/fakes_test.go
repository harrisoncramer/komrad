package checkouts

import (
	"context"
	"time"

	"weave/internal/agents"
	"weave/internal/harness"
)

// settlingObserver answers with a run of readings, holding the last one once they are spent, which
// is how a pane that takes a moment to come to rest is written down.
type settlingObserver struct {
	readings []harness.Activity
	err      error
	reads    int
	// onFirst runs before the first reading.
	onFirst func()
}

func (o *settlingObserver) Observe(_ context.Context, _ harness.Target) (harness.Observation, error) {
	if o.reads == 0 && o.onFirst != nil {
		o.onFirst()
	}
	if o.err != nil {
		return harness.Observation{}, o.err
	}

	at := min(o.reads, len(o.readings)-1)
	o.reads++

	return harness.Observation{Activity: o.readings[at]}, nil
}

// settleStep is how far the fake clock moves between two readings of the pane.
const settleStep = 250 * time.Millisecond

// settlerOver builds a settler whose clock walks the budget by itself.
func settlerOver(channel harness.Channel, observer harness.Observer) *agents.Settler {
	at := time.Now()

	return agents.NewSettler(agents.NewSettlerParams{
		Channel:  channel,
		Observer: observer,
		Clock: func() time.Time {
			at = at.Add(settleStep)

			return at
		},
		Wait: func(time.Duration) <-chan time.Time {
			rested := make(chan time.Time, 1)
			rested <- at

			return rested
		},
	})
}
