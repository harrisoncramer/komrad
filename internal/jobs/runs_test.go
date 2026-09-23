package jobs

import (
	"testing"
	"time"

	"weave/internal/jobs/internaljobs"

	schedulestore "weave/internal/store/schedule"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunFromJob covers what a launch job reads as, a launched run carrying no error an earlier
// attempt left on the job.
func TestRunFromJob(t *testing.T) {
	scheduledFor := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)
	attemptErrors := []rivertype.AttemptError{
		{
			Attempt: 1,
			Error:   "first",
		},
		{
			Attempt: 2,
			Error:   "second",
		},
	}

	tests := []struct {
		name        string
		args        string
		state       rivertype.JobState
		errors      []rivertype.AttemptError
		wantErr     bool
		wantMessage string
	}{
		{
			name:    "arguments that are not a launch are refused",
			args:    "not json",
			state:   rivertype.JobStateCompleted,
			wantErr: true,
		},
		{
			name:   "a completed run drops the errors of the attempts before it",
			state:  rivertype.JobStateCompleted,
			errors: attemptErrors,
		},
		{
			name:  "a skipped run gives its reason without River's wrapping",
			state: rivertype.JobStateCancelled,
			errors: []rivertype.AttemptError{
				{
					Attempt: 1,
					Error:   "JobCancelError: schedule \"nightly\" is paused",
				},
			},
			wantMessage: "schedule \"nightly\" is paused",
		},
		{
			name:        "a run still retrying says what its last attempt hit",
			state:       rivertype.JobStateRetryable,
			errors:      attemptErrors,
			wantMessage: "second",
		},
		{
			name:        "a discarded run says what its last attempt hit",
			state:       rivertype.JobStateDiscarded,
			errors:      attemptErrors,
			wantMessage: "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args
			if args == "" {
				args = `{"schedule_id":"s1","scheduled_for":"2026-07-28T09:00:00Z"}`
			}

			run, err := runFromJob(&rivertype.JobRow{
				ID:          7,
				EncodedArgs: []byte(args),
				State:       tt.state,
				Attempt:     2,
				MaxAttempts: 5,
				Errors:      tt.errors,
			})
			if tt.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, int64(7), run.ID)
			assert.True(t, run.ScheduledFor.Equal(scheduledFor))
			assert.Equal(t, schedulestore.RunState(tt.state), run.State)
			assert.Equal(t, 2, run.Attempt)
			assert.Equal(t, 5, run.MaxAttempts)
			assert.Equal(t, tt.wantMessage, run.Message)
		})
	}
}

// TestNotifyFinishedRun covers the watch being told only about launch jobs.
func TestNotifyFinishedRun(t *testing.T) {
	tests := []struct {
		name  string
		event *river.Event
		want  []string
	}{
		{
			name:  "an event with no job is ignored",
			event: &river.Event{Kind: river.EventKindJobCompleted},
		},
		{
			name: "another kind of job is ignored",
			event: &river.Event{Kind: river.EventKindJobCompleted, Job: &rivertype.JobRow{
				Kind:        "empty_archive",
				EncodedArgs: []byte(`{}`),
			}},
		},
		{
			name: "a launch job names its schedule",
			event: &river.Event{Kind: river.EventKindJobCancelled, Job: &rivertype.JobRow{
				Kind:        internaljobs.LaunchAgentArgs{}.Kind(),
				EncodedArgs: []byte(`{"schedule_id":"s1"}`),
			}},
			want: []string{"s1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := &fakeNotifier{notified: make(chan string, 1)}
			notifyFinishedRun(t.Context(), tt.event, notifier, nil)
			close(notifier.notified)

			var got []string
			for id := range notifier.notified {
				got = append(got, id)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}
