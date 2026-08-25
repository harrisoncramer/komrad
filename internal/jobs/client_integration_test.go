package jobs

import (
	"context"
	"testing"
	"time"

	"weave/db"
	"weave/internal/agents"
	"weave/internal/jobs/internaljobs"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	"weave/internal/storetest"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRiverClient(t *testing.T) *river.Client[pgx.Tx] {
	t.Helper()

	stores := storetest.Open(t)

	require.NoError(t, db.MigrateRiver(t.Context(), stores.Pool.GetRawPool()))

	schedules := &stubSchedules{}

	client, err := NewClient(ClientParams{
		Pool:      stores.Pool.GetRawPool(),
		Schedules: schedules,
		Results:   schedules,
		Agents:    &stubAgents{},
		Archived:  &stubArchive{},
		Running:   &stubRunning{},
	})
	require.NoError(t, err)

	return client
}

// TestFiringIsQueuedOnce covers River's side of the guarantee. A firing is
// identified by its arguments, so two inserts naming the same occurrence of the
// same schedule have to collapse into one job while everything else stays its
// own.
func TestFiringIsQueuedOnce(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string

		first  internaljobs.LaunchAgentArgs
		second internaljobs.LaunchAgentArgs

		wantDuplicate bool
	}{
		{
			name:   "another schedule firing at the same moment is its own job",
			first:  internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			second: internaljobs.LaunchAgentArgs{ScheduleID: "s2", ScheduledFor: nine},
		},
		{
			name:   "tomorrow's run of the same schedule is its own job",
			first:  internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			second: internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine.Add(24 * time.Hour)},
		},
		{
			name:          "the same occurrence inserted twice is one job",
			first:         internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			second:        internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			wantDuplicate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newRiverClient(t)

			first, err := client.Insert(t.Context(), tt.first, nil)
			require.NoError(t, err)
			require.False(t, first.UniqueSkippedAsDuplicate, "the first insert of a firing is never a duplicate")

			second, err := client.Insert(t.Context(), tt.second, nil)
			require.NoError(t, err)

			assert.Equal(t, tt.wantDuplicate, second.UniqueSkippedAsDuplicate)
		})
	}
}

// TestFiringIsQueuedOnceAfterAnAttemptEnded covers the same guarantee once the
// first attempt is off the queue. Every way a firing declines to launch ends the
// job rather than failing it, so if reaching one of those states frees the
// occurrence to be queued again, the whole scheme only holds while a job is
// still in flight.
func TestFiringIsQueuedOnceAfterAnAttemptEnded(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string

		cancel bool
	}{
		{
			name:   "a cancelled attempt does not free the occurrence",
			cancel: true,
		},
		{
			name: "and neither does one still sitting on the queue",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newRiverClient(t)
			firing := internaljobs.LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine}

			first, err := client.Insert(t.Context(), firing, nil)
			require.NoError(t, err)
			require.False(t, first.UniqueSkippedAsDuplicate)

			if tt.cancel {
				_, err := client.JobCancel(t.Context(), first.Job.ID)
				require.NoError(t, err)
			}

			second, err := client.Insert(t.Context(), firing, nil)
			require.NoError(t, err)

			assert.True(t, second.UniqueSkippedAsDuplicate, "the same occurrence must not be queued a second time")
		})
	}
}

// TestEmptyArchiveIsQueuedOnce covers River's side of it. The sweep takes as long as tearing
// down that many agents takes, so a second press while one is under way has to collapse into
// it rather than starting a second sweep over the same archive.
func TestEmptyArchiveIsQueuedOnce(t *testing.T) {
	client := newRiverClient(t)

	first, err := client.Insert(t.Context(), internaljobs.EmptyArchiveArgs{}, nil)
	require.NoError(t, err)
	require.False(t, first.UniqueSkippedAsDuplicate)

	second, err := client.Insert(t.Context(), internaljobs.EmptyArchiveArgs{}, nil)
	require.NoError(t, err)

	assert.True(t, second.UniqueSkippedAsDuplicate, "a sweep already in flight is the one that runs")
}

// TestPeriodicRegistrationIsKeyedByItsId covers the id River files a registration
// under. The scheduler takes one off by the schedule's id and nothing else, so a
// job filed under anything but the id it was added with could never be removed and
// the schedule would fire forever.
func TestPeriodicRegistrationIsKeyedByItsId(t *testing.T) {
	cron, err := agents.ParseCron("0 9 * * *", "")
	require.NoError(t, err)

	tests := []struct {
		name string

		added []string

		wantAddErr bool
	}{
		{
			name:       "the same id twice is refused",
			added:      []string{"s1", "s1"},
			wantAddErr: true,
		},
		{
			name:  "a registration comes off by the id it was added under",
			added: []string{"s1"},
		},
		{
			name:  "and each of several does, under its own",
			added: []string{"s1", "s2", "s3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bundle := newRiverClient(t).PeriodicJobs()
			periodic := NewRiverPeriodic(bundle)

			for i, id := range tt.added {
				err := periodic.Add(id, cron)
				if tt.wantAddErr && i == len(tt.added)-1 {
					require.Error(t, err)
					return
				}

				require.NoError(t, err)
			}

			for _, id := range tt.added {
				assert.True(t, bundle.RemoveByID(id), "a registration has to come off by the id it was added under")
			}
		})
	}
}

type blockingLauncher struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingLauncher) NewAgent(ctx context.Context, p agents.NewAgentParams) (*agentstore.Agent, error) {
	close(b.started)

	select {
	case <-b.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	return &agentstore.Agent{ID: "a1", Name: p.Title}, nil
}

func (b *blockingLauncher) DeleteAgents(context.Context, []string) error {
	return nil
}

// TestStopWaitsForALaunchInFlight is the guarantee the shutdown path is built on. A
// process killed partway through a launch leaves the firing marked running, and River's
// rescuer does not come back for it for over an hour, so the stop has to wait rather
// than walk away.
func TestStopWaitsForALaunchInFlight(t *testing.T) {
	stores := storetest.Open(t)
	require.NoError(t, db.MigrateRiver(t.Context(), stores.Pool.GetRawPool()))

	launcher := &blockingLauncher{started: make(chan struct{}), release: make(chan struct{})}
	schedules := &stubSchedules{schedule: &schedulestore.Schedule{ID: "s1", Name: "nightly", Enabled: true}}

	client, err := NewClient(ClientParams{
		Pool:      stores.Pool.GetRawPool(),
		Schedules: schedules,
		Results:   schedules,
		Agents:    launcher,
		Archived:  &stubArchive{},
		Running:   &stubRunning{},
	})
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))

	inserted, err := client.Insert(t.Context(), internaljobs.LaunchAgentArgs{
		ScheduleID:   "s1",
		ScheduledFor: time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC),
	}, nil)
	require.NoError(t, err)

	select {
	case <-launcher.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the job never started")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- client.Stop(t.Context()) }()

	select {
	case <-stopped:
		t.Fatal("the stop did not wait for the launch")
	case <-time.After(250 * time.Millisecond):
	}

	close(launcher.release)

	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("the stop did not return once the launch finished")
	}

	job, err := client.JobGet(t.Context(), inserted.Job.ID)
	require.NoError(t, err)
	assert.Equal(t, rivertype.JobStateCompleted, job.State, "the firing is finished rather than left stuck as running")
}

type stubSchedules struct {
	schedule *schedulestore.Schedule
}

func (s *stubSchedules) Get(context.Context, string) (*schedulestore.Schedule, error) {
	return s.schedule, nil
}

func (s *stubSchedules) List(context.Context) ([]schedulestore.Schedule, error) {
	if s.schedule == nil {
		return nil, nil
	}

	return []schedulestore.Schedule{*s.schedule}, nil
}

func (s *stubSchedules) RecordLaunchResult(context.Context, string, schedulestore.LaunchResultStatus, string) error {
	return nil
}

type stubAgents struct{}

func (stubAgents) NewAgent(_ context.Context, p agents.NewAgentParams) (*agentstore.Agent, error) {
	return &agentstore.Agent{ID: "a1", Name: p.Title}, nil
}

func (stubAgents) DeleteAgents(context.Context, []string) error { return nil }

type stubArchive struct{}

func (stubArchive) ListPage(context.Context, int64, *agentstore.Cursor, bool) ([]agentstore.Agent, error) {
	return nil, nil
}

type stubRunning struct{}

func (stubRunning) CountLiveForSchedule(context.Context, string) (int64, error) { return 0, nil }
