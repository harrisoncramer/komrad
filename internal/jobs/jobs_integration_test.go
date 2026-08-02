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

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The unit tests in this package fake River and fake the store, so they assert
// that the Go around the guarantees is right and say nothing about the
// guarantees themselves. Both of them live outside Go: River's unique index over
// a job's encoded arguments, and the partial unique index on an agent's
// idempotency key. These run against a real River client on a real database, so
// the claim that a firing cannot happen twice is checked rather than assumed.

// newRiverClient builds a River client over a throwaway database, migrated the
// way serve migrates it. The worker's dependencies are the package's fakes,
// since these tests are about what the queue does with a job rather than what
// the job does.
func newRiverClient(t *testing.T) *river.Client[pgx.Tx] {
	t.Helper()

	stores := storetest.Open(t)

	require.NoError(t, db.MigrateRiver(t.Context(), stores.Pool.Raw()))

	client, err := NewClient(ClientParams{
		Pool:      stores.Pool.Raw(),
		Schedules: &fakeSchedules{},
		Agents:    &fakeLauncher{},
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
		// first and second are the two firings inserted, in order.
		first  LaunchAgentArgs
		second LaunchAgentArgs
		// wantDuplicate says the second insert should have been swallowed.
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
		// cancel says the first attempt was cancelled, which is what the worker
		// does for a schedule that is paused, deleted, or still running.
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
		// added are the ids registered, in order.
		added []string
		// wantAddErr says the last registration should have been refused, since two
		// jobs under one id would leave the schedule firing after a removal.
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
		// occurrences are the firings worked, in order.
		occurrences []time.Time
		// promoteAfterFirst leaves the first launch's agent visibly working, which
		// is what makes the next firing skip rather than launch.
		promoteAfterFirst bool
		// wantSkipped says the last firing declined to launch, which the worker
		// reports as a cancellation rather than a failure.
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

			worker := NewLaunchAgentWorker(stores.Schedules, newAgentService(t, stores), stores.Agents)

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

// claudeHarnessID is the harness the seeded workflows run, named here so a test
// reads the same id the registry below is built from.
const claudeHarnessID = "claude"

// newAgentService builds the real agent service over the test's stores. Only
// tmux is stood in for, since a launch opens a window and types a command into
// it, and neither is what these tests are about.
func newAgentService(t *testing.T, stores *storetest.Stores) *agents.AgentService {
	t.Helper()

	registry, err := harness.NewRegistry(claude.NewHarness(claude.NewHarnessParams{ProjectsDir: t.TempDir()}))
	require.NoError(t, err)

	return agents.NewAgentService(agents.NewAgentServiceParams{
		Tmux:           &stubTmux{},
		Agents:         stores.Agents,
		Workflows:      stores.Workflows,
		Tx:             stores.Pool,
		Launcher:       agents.NewLauncher(agents.LauncherParams{Tmux: &stubTmux{}, Addr: "127.0.0.1:8787"}),
		Harnesses:      registry,
		DefaultHarness: claudeHarnessID,
		RestoreRoot:    t.TempDir(),
	})
}

// promoteScheduleAgents marks a schedule's agents as visibly working, which is
// what the reconciler would do once it saw their panes and what the next firing
// reads to decide it has been lapped.
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

// stubTmux stands in for the tmux server a launch would touch. Every window it
// opens is a name rather than a pane, and the command a launch types goes
// nowhere.
type stubTmux struct {
	panes int
}

func (s *stubTmux) NewWindow(_, name, _ string) (string, error) {
	s.panes++

	return fmt.Sprintf("%%%d-%s", s.panes, name), nil
}

func (s *stubTmux) KillPane(string) {}

func (s *stubTmux) SetPaneOwner(string, string) error { return nil }

func (s *stubTmux) PaneOwner(string) string { return "" }

func (s *stubTmux) PaneOwners() (map[string]string, error) { return map[string]string{}, nil }

func (s *stubTmux) ClientPath(string) string { return "" }

func (s *stubTmux) SendKeys(string, ...string) error { return nil }

// blockingLauncher holds a launch open until the test lets it go, standing in for
// an agent partway through being typed into its pane.
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

// TestStopWaitsForALaunchInFlight is the guarantee the shutdown path is built on. A
// process killed partway through a launch leaves the firing marked running, and River's
// rescuer does not come back for it for over an hour, so the stop has to wait rather
// than walk away.
func TestStopWaitsForALaunchInFlight(t *testing.T) {
	stores := storetest.Open(t)
	require.NoError(t, db.MigrateRiver(t.Context(), stores.Pool.Raw()))

	launcher := &blockingLauncher{started: make(chan struct{}), release: make(chan struct{})}

	client, err := NewClient(ClientParams{
		Pool:      stores.Pool.Raw(),
		Schedules: &fakeSchedules{schedule: &schedulestore.Schedule{ID: "s1", Name: "nightly", Enabled: true}},
		Agents:    launcher,
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

	// The launch is still going, so a stop that returned here would be the process
	// walking out on it.
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
