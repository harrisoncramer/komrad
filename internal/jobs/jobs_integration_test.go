package jobs

import (
	"context"
	"fmt"
	"testing"
	"time"

	"weave/db"
	"weave/internal/agents"
	"weave/internal/harness"
	"weave/internal/harness/claude"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/internal/tmux"

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

	schedules := &fakeSchedules{}

	client, err := NewClient(ClientParams{
		Pool:      stores.Pool.GetRawPool(),
		Schedules: schedules,
		Results:   schedules,
		Agents:    &fakeLauncher{},
		Archived:  &fakeArchive{},
		Running:   &fakeRunning{},
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

		first  LaunchAgentArgs
		second LaunchAgentArgs

		wantDuplicate bool
	}{
		{
			name:   "another schedule firing at the same moment is its own job",
			first:  LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			second: LaunchAgentArgs{ScheduleID: "s2", ScheduledFor: nine},
		},
		{
			name:   "tomorrow's run of the same schedule is its own job",
			first:  LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			second: LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine.Add(24 * time.Hour)},
		},
		{
			name:          "the same occurrence inserted twice is one job",
			first:         LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
			second:        LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine},
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
			firing := LaunchAgentArgs{ScheduleID: "s1", ScheduledFor: nine}

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

// TestScheduledLaunchCreatesOneAgent covers the store's side of the guarantee,
// with the real agent service behind the worker rather than a recorder. River
// retries a failed attempt with the same arguments, so working the same firing
// twice is the ordinary case rather than an exotic one, and only the idempotency
// key stands between it and a second agent on the same prompt.
func TestScheduledLaunchCreatesOneAgent(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name string

		occurrences []time.Time

		promoteAfterFirst bool

		wantSkipped bool
		wantAgents  int
	}{
		{
			name:              "a later firing skips while the last agent is still working",
			occurrences:       []time.Time{nine, nine.Add(24 * time.Hour)},
			promoteAfterFirst: true,
			wantSkipped:       true,
			wantAgents:        1,
		},
		{
			name:        "working the same firing twice launches one agent",
			occurrences: []time.Time{nine, nine},
			wantAgents:  1,
		},
		{
			name:        "and a later firing launches once the last one is no longer working",
			occurrences: []time.Time{nine, nine.Add(24 * time.Hour)},
			wantAgents:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			workflow := stores.SeedWorkflow(t, workflowstore.Config{
				Name:    "nightly",
				Harness: claudeHarnessID,
				Cwd:     t.TempDir(),
			})
			schedule := stores.SeedSchedule(t, schedulestore.Config{
				Name:       "morning-ci",
				Cron:       "0 9 * * *",
				Prompt:     "check CI",
				Enabled:    true,
				WorkflowID: workflow.ID,
			})

			worker := NewLaunchAgentWorker(stores.Schedules, newAgentService(t, stores), stores.Agents, stores.Schedules, nil)

			for i, occurrence := range tt.occurrences {
				err := worker.Work(t.Context(), &river.Job[LaunchAgentArgs]{
					Args: LaunchAgentArgs{ScheduleID: schedule.ID, ScheduledFor: occurrence},
				})

				last := i == len(tt.occurrences)-1
				if last && tt.wantSkipped {
					require.Error(t, err)
					assert.True(t, isCancel(err), "a firing that declines to launch cancels rather than failing")
				} else {
					require.NoError(t, err)
				}

				if i == 0 && tt.promoteAfterFirst {
					promoteScheduleAgents(t, stores, schedule.ID)
				}
			}

			listed, err := stores.Agents.List(t.Context())
			require.NoError(t, err)

			assert.Len(t, listed, tt.wantAgents)
		})
	}
}

const claudeHarnessID = "claude"

func newAgentService(t *testing.T, stores *storetest.Stores) *agents.AgentService {
	t.Helper()

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()}))
	require.NoError(t, err)

	return agents.NewAgentService(agents.NewAgentServiceParams{
		Tmux:               &stubTmux{},
		Agents:             stores.Agents,
		Workflows:          stores.Workflows,
		Tx:                 stores.Pool,
		Launcher:           agents.NewLauncher(agents.LauncherParams{Tmux: &stubTmux{}, Addr: "127.0.0.1:8787"}),
		Harnesses:          registry,
		UserDefaultHarness: claudeHarnessID,
		FallbackResumeRoot: t.TempDir(),
	})
}

func promoteScheduleAgents(t *testing.T, stores *storetest.Stores, scheduleID string) {
	t.Helper()

	listed, err := stores.Agents.List(t.Context())
	require.NoError(t, err)

	for _, agent := range listed {
		if agent.ScheduleID == nil || *agent.ScheduleID != scheduleID {
			continue
		}

		require.NoError(t, stores.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateThinking))
	}
}

type stubTmux struct {
	panes int
}

func (s *stubTmux) NewWindow(_, name, _ string) (string, error) {
	s.panes++

	return fmt.Sprintf("%%%d-%s", s.panes, name), nil
}

func (s *stubTmux) KillPane(string) {}

func (s *stubTmux) SetPaneOwner(string, string) error { return nil }

func (s *stubTmux) GetPaneOwner(string) string { return "" }

func (s *stubTmux) ClearPaneOwner(string) error { return nil }

func (s *stubTmux) GetPanes() (map[string]tmux.PaneStatus, error) {
	return map[string]tmux.PaneStatus{}, nil
}

func (s *stubTmux) GetClientPath(string) string { return "" }

func (s *stubTmux) SendKeys(string, ...string) error { return nil }

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
	schedules := &fakeSchedules{schedule: &schedulestore.Schedule{ID: "s1", Name: "nightly", Enabled: true}}

	client, err := NewClient(ClientParams{
		Pool:      stores.Pool.GetRawPool(),
		Schedules: schedules,
		Results:   schedules,
		Agents:    launcher,
		Archived:  &fakeArchive{},
		Running:   &fakeRunning{},
	})
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))

	inserted, err := client.Insert(t.Context(), LaunchAgentArgs{
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
