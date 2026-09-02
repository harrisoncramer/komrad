package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// attempts is how many tries a job in these tests is given, which the worker treats the same
// whatever number River hands it.
const attempts = 5

// TestLaunchAgentWorker covers a fired schedule cancelling when it is gone or paused, and
// returning an error when the failure is one that might pass.
func TestLaunchAgentWorker(t *testing.T) {
	occurrence := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name             string
		schedule         *schedulestore.Schedule
		launchErr        error
		wantCancel       bool
		wantErr          bool
		running          int64
		attempt          int
		maxAttempts      int
		wantLaunched     bool
		wantResultStatus schedulestore.LaunchResultStatus
	}{
		{
			name: "an armed schedule launches its agent",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "morning-ci",
				Prompt:  "check CI",
				Enabled: true,
			},
			wantLaunched:     true,
			wantResultStatus: schedulestore.LaunchResultLaunched,
		},
		{
			// The launcher hands this back only for an agent that actually came up, since a create
			// that failed has its key taken off it and is launched again.
			name: "an occurrence whose agent is already up records that rather than retrying",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "morning-ci",
				Prompt:  "check CI",
				Enabled: true,
			},
			launchErr:        agents.ErrAlreadyLaunched,
			wantLaunched:     true,
			wantResultStatus: schedulestore.LaunchResultLaunched,
		},
		{
			name:             "a schedule deleted since the job was queued cancels",
			wantCancel:       true,
			wantResultStatus: schedulestore.LaunchResultScheduleNotFound,
		},
		{
			name: "a schedule paused since the job was queued cancels",
			schedule: &schedulestore.Schedule{
				ID:     "s1",
				Name:   "morning-ci",
				Prompt: "check CI",
			},
			wantCancel:       true,
			wantResultStatus: schedulestore.LaunchResultSchedulePaused,
		},
		{
			name: "a directory that cannot be opened cancels",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "morning-ci",
				Prompt:  "go",
				Enabled: true,
			},
			launchErr:        agents.ErrBadDirectory,
			wantCancel:       true,
			wantLaunched:     true,
			wantResultStatus: schedulestore.LaunchResultCannotLaunch,
		},
		{
			name: "a setup action that refuses cancels",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "morning-ci",
				Prompt:  "go",
				Enabled: true,
			},
			launchErr:        agents.ErrProvisionFailed,
			wantCancel:       true,
			wantLaunched:     true,
			wantResultStatus: schedulestore.LaunchResultCannotLaunch,
		},
		{
			name: "a schedule whose last agent is still running skips this tick",
			schedule: &schedulestore.Schedule{
				ID:         "s1",
				Name:       "morning-ci",
				Prompt:     "go",
				WorkflowID: "w1",
				Enabled:    true,
			},
			running:          1,
			wantCancel:       true,
			wantResultStatus: schedulestore.LaunchResultAlreadyRunning,
		},
		{
			name: "a transient failure with attempts left is recorded as retrying, not terminal",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "morning-ci",
				Prompt:  "go",
				Enabled: true,
			},
			launchErr:        errors.New("no server running"),
			attempt:          1,
			maxAttempts:      attempts,
			wantErr:          true,
			wantLaunched:     true,
			wantResultStatus: schedulestore.LaunchResultRetrying,
		},
		{
			name: "the same transient failure on the last attempt is recorded as terminal, not retrying",
			schedule: &schedulestore.Schedule{
				ID:      "s1",
				Name:    "morning-ci",
				Prompt:  "go",
				Enabled: true,
			},
			launchErr:        errors.New("no server running"),
			attempt:          attempts,
			maxAttempts:      attempts,
			wantErr:          true,
			wantLaunched:     true,
			wantResultStatus: schedulestore.LaunchResultCannotLaunch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedules := &fakeSchedules{schedule: tt.schedule}
			launcher := &fakeLauncher{err: tt.launchErr}
			worker := NewLaunchAgentWorker(schedules, launcher, &fakeRunning{count: tt.running}, schedules, nil)

			job := &river.Job[LaunchAgentArgs]{
				Args: LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: occurrence},
			}
			if tt.maxAttempts != 0 {
				job.JobRow = &rivertype.JobRow{Attempt: tt.attempt, MaxAttempts: tt.maxAttempts}
			}

			err := worker.Work(t.Context(), job)

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

			require.Len(t, schedules.results, 1, "every finished firing records exactly one result")
			assert.Equal(t, "s1", schedules.results[0].scheduleID)
			assert.Equal(t, tt.wantResultStatus, schedules.results[0].status, "which of retrying or terminal this attempt count should record")
		})
	}
}

// TestLaunchAgentParams covers everything the agent starts with coming off the schedule,
// with the idempotency key making a retry safe.
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
			name: "and nothing about the environment is passed with them",
			schedule: schedulestore.Schedule{
				ID: "s1", Name: "scoped", Prompt: "go", WorkflowID: "w2", Enabled: true,
			},
			want: agents.NewAgentParams{
				Title: "scoped", Prompt: "go", WorkflowID: "w2",
				IdempotencyKey: "schedule:s1:2026-07-28T09:00:00Z",
			},
		},
		{
			name: "a schedule hiding its runs launches a hidden agent",
			schedule: schedulestore.Schedule{
				ID: "s1", Name: "sweeper", Prompt: "sweep", WorkflowID: "w1",
				Enabled: true, HideAgents: true,
			},
			want: agents.NewAgentParams{
				Title: "sweeper", Prompt: "sweep", WorkflowID: "w1", Hidden: true,
				IdempotencyKey: "schedule:s1:2026-07-28T09:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			launcher := &fakeLauncher{}
			schedules := &fakeSchedules{schedule: &tt.schedule}
			worker := NewLaunchAgentWorker(schedules, launcher, &fakeRunning{}, schedules, nil)

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
			assert.Equal(t, tt.want.Hidden, got.Hidden, "whether the run starts out of the list is the schedule's to say")

			require.NotNil(t, got.ScheduleID, "a launched agent records the schedule it came from")
			assert.Equal(t, tt.schedule.ID, *got.ScheduleID)
		})
	}
}

// TestLaunchKey covers a retry of one occurrence producing the same key and a different
// occurrence a different one, which is the whole of what stops a duplicate agent.
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

func isCancel(err error) bool {
	var cancel *river.JobCancelError

	return errors.As(err, &cancel)
}

type fakeSchedules struct {
	schedule *schedulestore.Schedule
	results  []recordedResult
}

type recordedResult struct {
	scheduleID string
	status     schedulestore.LaunchResultStatus
	message    string
}

func (f *fakeSchedules) Get(_ context.Context, id string) (*schedulestore.Schedule, error) {
	if f.schedule == nil {
		return nil, fmt.Errorf("%w: schedule %q", store.ErrNotFound, id)
	}

	return f.schedule, nil
}

func (f *fakeSchedules) List(context.Context) ([]schedulestore.Schedule, error) {
	if f.schedule == nil {
		return nil, nil
	}

	return []schedulestore.Schedule{*f.schedule}, nil
}

func (f *fakeSchedules) RecordLaunchResult(_ context.Context, scheduleID string, status schedulestore.LaunchResultStatus, message string) error {
	f.results = append(f.results, recordedResult{
		scheduleID: scheduleID,
		status:     status,
		message:    message,
	})

	return nil
}

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

type fakeRunning struct {
	count int64
	err   error
}

func (f *fakeRunning) CountLiveForSchedule(context.Context, string) (int64, error) {
	return f.count, f.err
}
