package agents

import (
	"errors"
	"testing"

	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	agentstore "komrad/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSettleLeavesAPaneItsCLIHasLeft covers an agent with nothing running being at rest without its frame being read.
func TestSettleLeavesAPaneItsCLIHasLeft(t *testing.T) {
	tests := []struct {
		name     string
		observer *harnesstest.Observer
	}{
		{
			name:     "a pane that is gone",
			observer: &harnesstest.Observer{Exited: true, Err: errors.New("the pane is gone")},
		},
		{
			name: "a pane back at its shell, whose last frame still shows a turn",
			observer: func() *harnesstest.Observer {
				observer := harnesstest.NewActivityObserver(harness.ActivityWorking)
				observer.Exited = true

				return observer
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &harnesstest.Channel{}
			agent := agentstore.Agent{ID: "a1", Name: "telescope", Pane: "%1", Harness: "claude"}

			interrupted, err := settlerOver(channel, tt.observer).Settle(t.Context(), agent)
			require.NoError(t, err)

			assert.False(t, interrupted)
			assert.Zero(t, channel.Interrupts, "no keys are typed into a shell")
			assert.Zero(t, tt.observer.Reads)
		})
	}
}

// TestSettleRefusesAPaneItCannotAskAbout covers a failure to ask what the pane runs being no evidence the CLI has left.
func TestSettleRefusesAPaneItCannotAskAbout(t *testing.T) {
	channel := &harnesstest.Channel{}
	observer := &harnesstest.Observer{RunningErr: errors.New("tmux gave up waiting")}
	agent := agentstore.Agent{ID: "a1", Name: "telescope", Pane: "%1", Harness: "claude"}

	_, err := settlerOver(channel, observer).Settle(t.Context(), agent)
	require.ErrorIs(t, err, ErrUnsettled)
	assert.Contains(t, err.Error(), "could not be read")
	assert.Zero(t, channel.Interrupts)
}
