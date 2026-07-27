package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"claude-agents/internal/agents"
	agentstore "claude-agents/internal/store/agent"
	schedulestore "claude-agents/internal/store/schedule"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLaunchAgentWorker covers what a fired schedule does, and the two ways a
// firing stops. A schedule that is gone or paused cancels, since retrying finds
// the same answer, while a launch that failed for a reason that might pass is
// returned so River tries again.
func TestLaunchAgentWorker(t *testing.T) {
	occurrence := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		// schedule is what the store answers with, nil for one that has been
		// deleted since the job was queued.
		schedule *schedulestore.Schedule
		// launchErr is what the agent service refuses the launch with.
		launchErr error
		// wantCancel says the job should stop rather than be retried.
		wantCancel bool
		wantErr    bool
		// wantLaunched says the agent service should have been asked at all.
		wantLaunched bool
	}{
		{
			name:         "an armed schedule launches its agent",
			schedule:     &schedulestore.Schedule{ID: "s1", Name: "morning-ci", Prompt: "check CI", Enabled: true},
			wantLaunched: true,
		},
		{
			name:       "a schedule deleted since the job was queued cancels",
			wantCancel: true,
		},
		{
			name:       "a schedule paused since the job was queued cancels",
			schedule:   &schedulestore.Schedule{ID: "s1", Name: "morning-ci", Prompt: "check CI"},
			wantCancel: true,
		},
		{
			// The directory will not become openable by trying again, so this stops
			// rather than spending the retry budget on it.
			name:         "a directory that cannot be opened cancels",
			schedule:     &schedulestore.Schedule{ID: "s1", Name: "morning-ci", Prompt: "go", Enabled: true},
			launchErr:    agents.ErrBadDirectory,
			wantCancel:   true,
			wantLaunched: true,
		},
		{
			// A tmux server that is briefly gone is the common case, and it comes
			// back, so this is left for River to retry.
			name:         "any other failure is retried",
			schedule:     &schedulestore.Schedule{ID: "s1", Name: "morning-ci", Prompt: "go", Enabled: true},
			launchErr:    errors.New("no server running"),
			wantErr:      true,
			wantLaunched: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedules := &fakeSchedules{schedule: tt.schedule}
			launcher := &fakeLauncher{err: tt.launchErr}
			worker := NewLaunchAgentWorker(schedules, launcher)

			err := worker.Work(t.Context(), &river.Job[LaunchAgentArgs]{
				Args: LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: occurrence},
			})

			assert.Equal(t, tt.wantLaunched, launcher.calls == 1, "whether the launch was attempted")

			switch {
			case tt.wantCancel:
				require.Error(t, err)
				assert.True(t, isCancel(err), "a job that cannot succeed cancels rather than being retried")
			case tt.wantErr:
				require.Error(t, err)
				assert.False(t, isCancel(err), "a failure that might pass is left for River to retry")
			default:
				require.NoError(t, err)
			}
		})
	}
}

// TestLaunchAgentParams covers what the worker passes to the agent service. The
// schedule is a blueprint, so everything the agent starts with comes off it,
// and the idempotency key is what makes a retry safe.
func TestLaunchAgentParams(t *testing.T) {
	occurrence := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		schedule schedulestore.Schedule
		want     agents.NewAgentParams
	}{
		{
			name: "the schedule's prompt and workflow become the agent's",
			schedule: schedulestore.Schedule{
				ID: "s1", Name: "morning-ci", Prompt: "check CI", WorkflowID: "w1", Enabled: true,
			},
			want: agents.NewAgentParams{
				Title: "morning-ci", Prompt: "check CI", WorkflowID: "w1",
				IdempotencyKey: "schedule:s1:2026-07-28T09:00:00Z",
			},
		},
		{
			// Nothing about where or how the agent runs is passed. The service reads
			// that off the workflow, which is the whole reason the schedule names one.
			name: "and nothing about the environment is passed with them",
			schedule: schedulestore.Schedule{
				ID: "s1", Name: "scoped", Prompt: "go", WorkflowID: "w2", Enabled: true,
			},
			want: agents.NewAgentParams{
				Title: "scoped", Prompt: "go", WorkflowID: "w2",
				IdempotencyKey: "schedule:s1:2026-07-28T09:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := &fakeLauncher{}
			worker := NewLaunchAgentWorker(&fakeSchedules{schedule: &tt.schedule}, launcher)

			require.NoError(t, worker.Work(t.Context(), &river.Job[LaunchAgentArgs]{
				Args: LaunchAgentArgs{ScheduleID: tt.schedule.ID, ScheduledFor: occurrence},
			}))

			got := launcher.got
			assert.Equal(t, tt.want.Title, got.Title)
			assert.Equal(t, tt.want.Prompt, got.Prompt)
			assert.Equal(t, tt.want.WorkflowID, got.WorkflowID)
			assert.Empty(t, got.Dir, "the directory is the workflow's to supply")
			assert.Empty(t, got.Flags, "and so are the flags")
			assert.Equal(t, tt.want.IdempotencyKey, got.IdempotencyKey)

			require.NotNil(t, got.ScheduleID, "a launched agent records the schedule it came from")
			assert.Equal(t, tt.schedule.ID, *got.ScheduleID)
		})
	}
}

// TestLaunchKey covers the identity of one firing. A retry of the same
// occurrence has to produce the same key, and a different occurrence or a
// different schedule a different one, since that is the whole of what stops a
// duplicate agent.
func TestLaunchKey(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		schedule string
		at       time.Time
		other    string
		otherAt  time.Time
		wantSame bool
	}{
		{
			name:     "the same schedule and occurrence",
			schedule: "s1",
			at:       nine,
			other:    "s1",
			otherAt:  nine,
			wantSame: true,
		},
		{
			name:     "the same instant written in another zone is the same occurrence",
			schedule: "s1",
			at:       nine,
			other:    "s1",
			otherAt:  nine.In(time.FixedZone("elsewhere", 5*60*60)),
			wantSame: true,
		},
		{
			name:     "tomorrow's run of the same schedule",
			schedule: "s1",
			at:       nine,
			other:    "s1",
			otherAt:  nine.Add(24 * time.Hour),
		},
		{
			name:     "another schedule at the same moment",
			schedule: "s1",
			at:       nine,
			other:    "s2",
			otherAt:  nine,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantSame, LaunchKey(tt.schedule, tt.at) == LaunchKey(tt.other, tt.otherAt))
		})
	}
}

// isCancel reports whether an error is River's cancellation rather than an
// ordinary failure, which is the difference between a job that stops and one
// that is tried again.
func isCancel(err error) bool {
	var cancel *river.JobCancelError

	return errors.As(err, &cancel)
}

// fakeSchedules answers the one read the worker makes.
type fakeSchedules struct {
	schedule *schedulestore.Schedule
}

func (f *fakeSchedules) Get(context.Context, string) (*schedulestore.Schedule, error) {
	return f.schedule, nil
}

func (f *fakeSchedules) List(context.Context) ([]schedulestore.Schedule, error) {
	if f.schedule == nil {
		return nil, nil
	}

	return []schedulestore.Schedule{*f.schedule}, nil
}

// fakeLauncher records what the worker asked for rather than launching
// anything, so a test can check the parameters a schedule turned into.
type fakeLauncher struct {
	got   agents.NewAgentParams
	calls int
	err   error
}

func (f *fakeLauncher) NewAgent(_ context.Context, p agents.NewAgentParams) (*agentstore.Agent, error) {
	f.calls++
	f.got = p
	if f.err != nil {
		return nil, f.err
	}

	return &agentstore.Agent{ID: "a1", Name: p.Title}, nil
}
