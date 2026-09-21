package checkouts

import (
	"context"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/harness"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func seedWorkflow(t *testing.T, st *storetest.Stores) string {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{Name: "workflow-" + uuid.NewString(), Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

// settlingObserver answers with a run of readings, holding the last one once they are spent, which
// is how a pane that takes a moment to come to rest is written down.
type settlingObserver struct {
	readings []harness.Activity
	err      error
	reads    int
}

func (o *settlingObserver) Observe(_ context.Context, _ harness.Target) (harness.Observation, error) {
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

type fakeChannel struct {
	interrupts int
}

func (f *fakeChannel) Ready(context.Context, harness.Target) (bool, error) { return true, nil }

func (f *fakeChannel) Send(context.Context, harness.Target, string) (bool, error) { return true, nil }

func (f *fakeChannel) Interrupt(context.Context, harness.Target) error {
	f.interrupts++

	return nil
}

func (f *fakeChannel) Forget(string) {}
