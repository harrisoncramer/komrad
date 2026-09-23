package jobs

import (
	"context"
	"testing"
	"time"

	"weave/db"
	"weave/internal/jobs/internaljobs"
	"weave/internal/storetest"

	schedulestore "weave/internal/store/schedule"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFinishedRunIsListedAndNotified covers a firing worked end to end: River finishes the job,
// the lister reads it back with the reason it was skipped, and the watch is told which schedule
// moved.
func TestFinishedRunIsListedAndNotified(t *testing.T) {
	tests := []struct {
		name        string
		schedule    *schedulestore.Schedule
		wantState   schedulestore.RunState
		wantMessage string
	}{
		{
			name: "a paused schedule's firing is cancelled with its reason",
			schedule: &schedulestore.Schedule{
				ID:   "s1",
				Name: "nightly",
			},
			wantState:   schedulestore.RunStateCancelled,
			wantMessage: "is paused",
		},
		{
			name: "an armed schedule's firing completes with no message",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "nightly",
				Enabled: true,
			},
			wantState: schedulestore.RunStateCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			pool, schema := stores.Pool.GetRawPool(), stores.Pool.GetRiverSchema()
			require.NoError(t, db.MigrateRiver(t.Context(), pool, schema))

			client, err := NewClient(NewClientParams{
				Schema:    schema,
				Pool:      pool,
				Schedules: &fakeSchedules{schedule: tt.schedule},
				Launcher:  &fakeAgents{},
				Deleter:   &fakeAgents{},
				Archived:  &fakeArchive{},
				Running:   &fakeRunning{},
			})
			require.NoError(t, err)

			notifier := &fakeNotifier{notified: make(chan string, 4)}
			NotifyFinishedRuns(t.Context(), client, notifier, nil)

			require.NoError(t, client.Start(t.Context()))
			t.Cleanup(func() { _ = client.Stop(context.WithoutCancel(t.Context())) })

			scheduledFor := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)
			_, err = client.Insert(t.Context(), internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: scheduledFor}, nil)
			require.NoError(t, err)

			select {
			case id := <-notifier.notified:
				assert.Equal(t, "s1", id, "the watch is told which schedule's run finished")
			case <-time.After(10 * time.Second):
				t.Fatal("the finished run was never notified")
			}

			runs, err := NewRunLister(pool, schema)
			require.NoError(t, err)

			listed, err := runs.List(t.Context(), "s1", 20)
			require.NoError(t, err)
			require.Len(t, listed, 1)
			assert.Equal(t, tt.wantState, listed[0].State)
			assert.True(t, listed[0].ScheduledFor.Equal(scheduledFor), "the occurrence is read back off the job's args")
			assert.NotNil(t, listed[0].FinalizedAt)
			if tt.wantMessage == "" {
				assert.Empty(t, listed[0].Message)
			} else {
				assert.Contains(t, listed[0].Message, tt.wantMessage)
				assert.NotContains(t, listed[0].Message, cancelPrefix, "the reason reads as the worker gave it")
			}

			other, err := runs.List(t.Context(), "s2", 20)
			require.NoError(t, err)
			assert.Empty(t, other, "another schedule's runs are its own")
		})
	}
}

type fakeNotifier struct {
	notified chan string
}

func (f *fakeNotifier) SendScheduleChange(_ context.Context, scheduleID string) error {
	f.notified <- scheduleID

	return nil
}
