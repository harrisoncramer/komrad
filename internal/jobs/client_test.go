package jobs

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"komrad/db"
	"komrad/internal/agents"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/schedules"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRiverClient(t *testing.T) *river.Client[pgx.Tx] {
	t.Helper()

	client, _ := newRiverClientWithStores(t)

	return client
}

func newRiverClientWithStores(t *testing.T) (*river.Client[pgx.Tx], *storetest.Stores) {
	t.Helper()

	stores := storetest.Open(t)

	require.NoError(t, db.MigrateRiver(t.Context(), stores.Pool.GetRawPool(), stores.Pool.GetRiverSchema()))

	client, err := NewClient(NewClientParams{
		Schema:    stores.Pool.GetRiverSchema(),
		Pool:      stores.Pool.GetRawPool(),
		Schedules: stores.Schedules,
		Runs:      stores.Schedules,
		Running:   stores.Agents,
	})
	require.NoError(t, err)

	return client, stores
}

// TestFiringIsQueuedOnce covers two inserts naming the same occurrence of the same
// schedule collapsing into one job while everything else stays its own.
func TestFiringIsQueuedOnce(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name          string
		first         internaljobs.LaunchAgentArgs
		second        internaljobs.LaunchAgentArgs
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

// TestFiringIsQueuedOnceAfterAnAttemptEnded covers the same guarantee once the first attempt is
// off the queue.
func TestFiringIsQueuedOnceAfterAnAttemptEnded(t *testing.T) {
	nine := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC)

	tests := []struct {
		name   string
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

// TestDeleteWorktreesIsQueuedOncePerSet covers a second press naming the same directories
// collapsing into the job already queued, while a different set is a job of its own.
func TestDeleteWorktreesIsQueuedOncePerSet(t *testing.T) {
	client := newRiverClient(t)

	first, err := client.Insert(t.Context(), internaljobs.DeleteWorktreesArgs{Dirs: []string{"/trees/a", "/trees/b"}}, nil)
	require.NoError(t, err)
	require.False(t, first.UniqueSkippedAsDuplicate)

	again, err := client.Insert(t.Context(), internaljobs.DeleteWorktreesArgs{Dirs: []string{"/trees/a", "/trees/b"}}, nil)
	require.NoError(t, err)
	assert.True(t, again.UniqueSkippedAsDuplicate, "the same directories are the job already queued")

	other, err := client.Insert(t.Context(), internaljobs.DeleteWorktreesArgs{Dirs: []string{"/trees/c"}}, nil)
	require.NoError(t, err)
	assert.False(t, other.UniqueSkippedAsDuplicate, "a different set is its own job")
}

// TestWorktreeDeleteClaimsWhatItQueues covers the worktree delete claiming only the archived
// agents among the directories' owners, queuing their deletes, and holding the directories
// until its own job is finished with.
func TestWorktreeDeleteClaimsWhatItQueues(t *testing.T) {
	client, stores := newRiverClientWithStores(t)
	ctx := t.Context()
	worktreeDelete := NewWorktreeDelete(NewEnqueuer(client, stores.Pool.GetRawPool()))

	workflow, err := stores.Workflows.Create(ctx, workflowstore.Config{Name: "backend", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	live := agentstore.Agent{ID: newID(t), Name: "live", WorkflowID: workflow.ID, State: agentstore.StateIdle}
	archived := agentstore.Agent{ID: newID(t), Name: "archived", WorkflowID: workflow.ID, State: agentstore.StateIdle}
	require.NoError(t, stores.Agents.Create(ctx, live))
	require.NoError(t, stores.Agents.Create(ctx, archived))
	_, err = stores.Agents.Archive(ctx, []string{archived.ID})
	require.NoError(t, err)
	dirs := []string{"/trees/" + archived.ID, "/trees/" + live.ID, "/trees/orphan"}

	require.NoError(t, worktreeDelete.DeleteWorktrees(ctx, dirs))

	claimed, err := stores.Agents.GetByID(ctx, archived.ID)
	require.NoError(t, err)
	assert.NotNil(t, claimed.DeletingAt, "the archived agent is claimed")
	left, err := stores.Agents.GetByID(ctx, live.ID)
	require.NoError(t, err)
	assert.Nil(t, left.DeletingAt, "the live agent is not")
	kept, err := stores.Workflows.GetIncludingArchived(ctx, workflow.ID)
	require.NoError(t, err)
	assert.Nil(t, kept.DeletingAt, "a live workflow is never claimed")
	assert.Equal(t, []string{archived.ID}, queuedDeletes(t, client), "the claimed agent's delete is queued")

	listed, err := worktreeDelete.ListClaimedDirs(ctx)
	require.NoError(t, err)
	assert.Equal(t, dirs, listed, "the directories are claimed while the job is owed")

	queued, err := client.JobList(ctx, river.NewJobListParams().Kinds(internaljobs.DeleteWorktreesArgs{}.Kind()))
	require.NoError(t, err)
	require.Len(t, queued.Jobs, 1)
	assert.Equal(t, internaljobs.QueueDeleteWorktrees, queued.Jobs[0].Queue)
	_, err = client.JobCancel(ctx, queued.Jobs[0].ID)
	require.NoError(t, err)

	listed, err = worktreeDelete.ListClaimedDirs(ctx)
	require.NoError(t, err)
	assert.Empty(t, listed, "and not once it is finished with")
}

// TestAgentDeleteQueuesWhatItClaims covers a delete queuing one job for each agent it took
// whose parent it did not, nothing for a second press, and nothing at all when an id names
// no agent.
func TestAgentDeleteQueuesWhatItClaims(t *testing.T) {
	client, stores := newRiverClientWithStores(t)
	ctx := t.Context()
	agentDelete := NewAgentDelete(NewEnqueuer(client, stores.Pool.GetRawPool()))

	parent := stores.SeedAgent(t, agentstore.Agent{Name: "author"})
	stores.SeedAgent(t, agentstore.Agent{Name: "reviewer", ParentAgentID: &parent.ID})
	other := stores.SeedAgent(t, agentstore.Agent{Name: "other"})

	err := agentDelete.DeleteAgents(ctx, []string{other.ID, newID(t)})
	require.ErrorIs(t, err, agents.ErrAgentNotFound)
	untouched, err := stores.Agents.GetByID(ctx, other.ID)
	require.NoError(t, err)
	assert.Nil(t, untouched.DeletingAt, "a refused batch claims nothing")
	assert.Empty(t, queuedDeletes(t, client))

	require.NoError(t, agentDelete.DeleteAgents(ctx, []string{parent.ID}))
	assert.Equal(t, []string{parent.ID}, queuedDeletes(t, client), "the child goes with its parent's job")

	require.NoError(t, agentDelete.DeleteAgents(ctx, []string{parent.ID, other.ID}))
	assert.ElementsMatch(t, []string{parent.ID, other.ID}, queuedDeletes(t, client), "a second press queues only what it newly took")
}

// TestAgentDeleteResume covers a starting server queuing the deletes the last one left owed
// and no more, and releasing a workflow no delete still holds.
func TestAgentDeleteResume(t *testing.T) {
	client, stores := newRiverClientWithStores(t)
	ctx := t.Context()
	agentDelete := NewAgentDelete(NewEnqueuer(client, stores.Pool.GetRawPool()))

	queued := stores.SeedAgent(t, agentstore.Agent{Name: "queued"})
	require.NoError(t, agentDelete.DeleteAgents(ctx, []string{queued.ID}))
	owed := stores.SeedAgent(t, agentstore.Agent{Name: "owed"})
	_, err := stores.Agents.MarkDeleting(ctx, []string{owed.ID})
	require.NoError(t, err)

	abandoned, err := stores.Workflows.Create(ctx, workflowstore.Config{Name: "abandoned", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	gone := stores.SeedAgent(t, agentstore.Agent{Name: "gone", WorkflowID: abandoned.ID})
	_, err = stores.Agents.Archive(ctx, []string{gone.ID})
	require.NoError(t, err)
	require.NoError(t, stores.Workflows.Archive(ctx, abandoned.ID))
	_, err = stores.Agents.MarkArchivedDeletingByID(ctx, []string{gone.ID})
	require.NoError(t, err)
	_, err = stores.Agents.Delete(ctx, []string{gone.ID})
	require.NoError(t, err)

	require.NoError(t, agentDelete.Resume(ctx))

	assert.ElementsMatch(t, []string{queued.ID, owed.ID}, queuedDeletes(t, client), "only the delete no job covered is queued")
	released, err := stores.Workflows.GetIncludingArchived(ctx, abandoned.ID)
	require.NoError(t, err)
	assert.Nil(t, released.DeletingAt, "a workflow no claimed agent holds is released")
}

func queuedDeletes(t *testing.T, client *river.Client[pgx.Tx]) []string {
	t.Helper()

	listed, err := client.JobList(t.Context(), river.NewJobListParams().Kinds(internaljobs.DeleteAgentArgs{}.Kind()))
	require.NoError(t, err)

	return xslice.Map(listed.Jobs, func(job *rivertype.JobRow) string {
		assert.Equal(t, internaljobs.QueueDeleteAgent, job.Queue)
		var args internaljobs.DeleteAgentArgs
		require.NoError(t, json.Unmarshal(job.EncodedArgs, &args))

		return args.AgentID
	})
}

func newID(t *testing.T) string {
	t.Helper()

	id, err := store.NewID()
	require.NoError(t, err)

	return id
}

// TestPeriodicRegistrationIsKeyedByItsId covers the id River files a registration under, which
// the scheduler takes one off by.
func TestPeriodicRegistrationIsKeyedByItsId(t *testing.T) {
	cron, err := schedules.ParseCron("0 9 * * *", "")
	require.NoError(t, err)

	tests := []struct {
		name       string
		added      []string
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

// TestStopWaitsForALaunchInFlight covers the stop waiting rather than walking away, River's
// rescuer not coming back for an abandoned firing for over an hour.
func TestStopWaitsForALaunchInFlight(t *testing.T) {
	stores := storetest.Open(t)
	require.NoError(t, db.MigrateRiver(t.Context(), stores.Pool.GetRawPool(), stores.Pool.GetRiverSchema()))

	launcher := &blockingLauncher{started: make(chan struct{}), release: make(chan struct{})}
	schedule := stores.SeedSchedule(t, schedulestore.Config{
		Name:    "nightly",
		Cron:    "0 9 * * *",
		Prompt:  "sweep",
		Enabled: true,
	})

	client, err := NewClient(NewClientParams{
		Schema:    stores.Pool.GetRiverSchema(),
		Pool:      stores.Pool.GetRawPool(),
		Schedules: stores.Schedules,
		Runs:      stores.Schedules,
		Launcher:  launcher,
		Running:   stores.Agents,
	})
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))

	inserted, err := client.Insert(t.Context(), internaljobs.LaunchAgentArgs{
		ScheduleID:   schedule.ID,
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
