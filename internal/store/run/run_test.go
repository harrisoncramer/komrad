package run

import (
	"testing"

	"komrad/internal/dbtest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	rowstore "komrad/internal/store/row"
	taskstore "komrad/internal/store/task"
	"komrad/internal/store/task/tasktest"
	workflowstore "komrad/internal/store/workflow"
	"komrad/lib/xptr"
	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	*RunStore
	db     store.DBTX
	agents *agentstore.AgentStore
	rows   *rowstore.RowStore
	// workflow is the one every seeded agent is filed under, a run hanging off an agent and an
	// agent off a workflow.
	workflow uuid.UUID
}

func open(t *testing.T) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	workflows := workflowstore.NewWorkflowStore(pool.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default", Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)

	return &fixture{
		RunStore: NewRunStore(pool.GetDB()),
		db:       pool.GetDB(),
		agents:   agentstore.NewAgentStore(pool.GetDB()),
		rows:     rowstore.NewRowStore(pool.GetDB()),
		workflow: created.ID,
	}
}

// failedRun is the mark an agent's row carries for a failed run nobody has looked at, nil when
// it carries none.
func (f *fixture) failedRun(t *testing.T, agentID uuid.UUID) *int32 {
	t.Helper()

	row, err := f.rows.Get(t.Context(), agentID)
	require.NoError(t, err)
	if !row.FailedRun {
		return nil
	}
	require.NotNil(t, row.FailedRunLog, "the mark carries the run to open on")

	return row.FailedRunLog
}

func (f *fixture) seedAgent(t *testing.T) uuid.UUID {
	t.Helper()

	taskID, err := tasktest.Seed(t.Context(), f.db, f.workflow)
	require.NoError(t, err)

	return f.seedAgentOn(t, taskID)
}

func (f *fixture) seedAgentOn(t *testing.T, taskID *uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.New()
	require.NoError(t, f.agents.Create(t.Context(), agentstore.Agent{
		TaskID:     taskID,
		ID:         id,
		Name:       "agent-" + id.String(),
		Cwd:        "/repo",
		State:      agentstore.StateIdle,
		WorkflowID: f.workflow,
	}))

	return id
}

// TestBeginRefusesAnIdNoAgentHolds pins that a run is refused rather than recorded against
// nothing.
func TestBeginRefusesAnIdNoAgentHolds(t *testing.T) {
	f := open(t)

	_, err := f.Begin(t.Context(), BeginParams{
		AgentID: uuid.New(),
		Trigger: eventv1.Moment_MOMENT_AGENT_POST_LAUNCH,
		Script:  "setup",
	})

	require.Error(t, err)
}

// TestFinishRecordsWhatTheRunDid covers the two halves of a run: the row opened before it starts,
// and the failure written onto it after.
func TestFinishRecordsWhatTheRunDid(t *testing.T) {
	tests := []struct {
		name       string
		exitCode   *int32
		failure    string
		wantUnread bool
	}{
		{
			name:     "a run that succeeded is nothing to read",
			exitCode: xptr.Ptr(int32(0)),
		},
		{
			name:       "a run that exited badly is held for the person",
			exitCode:   xptr.Ptr(int32(3)),
			failure:    "a hook failed: setup: exited 3",
			wantUnread: true,
		},
		{
			name:       "a run refused before it started has no status of its own",
			failure:    "a hook failed: setup: unusable working directory",
			wantUnread: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			agentID := f.seedAgent(t)

			id, err := f.Begin(t.Context(), BeginParams{
				AgentID:   agentID,
				Trigger:   eventv1.Moment_MOMENT_AGENT_POST_LAUNCH,
				Script:    "setup",
				LogNumber: xptr.Ptr(int32(7)),
			})
			require.NoError(t, err)

			assert.Nil(t, f.failedRun(t, agentID), "a run still going is not a failure yet")

			require.NoError(t, f.Finish(t.Context(), FinishParams{
				ID:       id,
				ExitCode: tt.exitCode,
				Failure:  tt.failure,
			}))

			if !tt.wantUnread {
				assert.Nil(t, f.failedRun(t, agentID))

				return
			}

			assert.Equal(t, xptr.Ptr(int32(7)), f.failedRun(t, agentID))

			require.NoError(t, f.MarkRead(t.Context(), agentID))

			assert.Nil(t, f.failedRun(t, agentID), "reading it clears the mark")
		})
	}
}

// TestUnreadFailuresNameTheNewestRun pins the mark pointing at the run that failed last rather
// than at whichever of an agent's failures the rows happened to come back in.
func TestUnreadFailuresNameTheNewestRun(t *testing.T) {
	f := open(t)
	agentID := f.seedAgent(t)

	for number := range 3 {
		id, err := f.Begin(t.Context(), BeginParams{
			AgentID:   agentID,
			Trigger:   eventv1.Moment_MOMENT_AGENT_POST_LAUNCH,
			Script:    "setup",
			LogNumber: xptr.Ptr(int32(number + 1)),
		})
		require.NoError(t, err)
		require.NoError(t, f.Finish(t.Context(), FinishParams{ID: id, Failure: "it failed"}))
	}

	assert.Equal(t, xptr.Ptr(int32(3)), f.failedRun(t, agentID))
}

// TestAbandonDropsTheRow covers the refusal that is not a run at all, where the log file is
// removed too and a row left behind would spend one of the fifty an agent keeps.
func TestAbandonDropsTheRow(t *testing.T) {
	f := open(t)
	agentID := f.seedAgent(t)

	id, err := f.Begin(t.Context(), BeginParams{
		AgentID: agentID,
		Trigger: eventv1.Moment_MOMENT_AGENT_DEPROVISION,
		Script:  "_worktree_cleanup",
	})
	require.NoError(t, err)
	require.NoError(t, f.Finish(t.Context(), FinishParams{ID: id, Failure: "it would not start"}))

	require.NoError(t, f.Abandon(t.Context(), id))

	assert.Nil(t, f.failedRun(t, agentID))
}

// TestPruneKeepsTheNewest holds the rows to the same bound the log files are held to, so a row
// is never left pointing at output the sweep has taken.
func TestPruneKeepsTheNewest(t *testing.T) {
	f := open(t)
	agentID := f.seedAgent(t)
	other := f.seedAgent(t)

	var ids []uuid.UUID
	for range 5 {
		id, err := f.Begin(t.Context(), BeginParams{
			AgentID: agentID,
			Trigger: eventv1.Moment_MOMENT_AGENT_RESUME,
			Script:  "worktree",
		})
		require.NoError(t, err)
		require.NoError(t, f.Finish(t.Context(), FinishParams{ID: id, Failure: "it failed"}))
		ids = append(ids, id)
	}

	kept, err := f.Begin(t.Context(), BeginParams{
		AgentID: other,
		Trigger: eventv1.Moment_MOMENT_AGENT_RESUME,
		Script:  "worktree",
	})
	require.NoError(t, err)
	require.NoError(t, f.Finish(t.Context(), FinishParams{ID: kept, Failure: "it failed"}))

	require.NoError(t, f.Prune(t.Context(), agentID, 2))

	remaining := f.runIDs(t, agentID)
	assert.Equal(t, ids[3:], remaining, "the oldest three went and the newest two stayed")
	assert.Equal(t, []uuid.UUID{kept}, f.runIDs(t, other), "another agent's runs are left alone")
}

// runIDs reads an agent's rows oldest first, which nothing in the store does because nothing
// above it reads a run back yet.
func (f *fixture) runIDs(t *testing.T, agentID uuid.UUID) []uuid.UUID {
	t.Helper()

	rows, err := f.db.Query(t.Context(), "SELECT id FROM agent_runs WHERE agent_id = $1 ORDER BY started_at, id", agentID)
	require.NoError(t, err)
	defer rows.Close()

	found := make([]uuid.UUID, 0, 8)
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		found = append(found, id)
	}
	require.NoError(t, rows.Err())

	return found
}

type recordParams struct {
	agentID uuid.UUID
	taskID  *uuid.UUID
	trigger eventv1.Moment
	failure string
	number  int32
}

// record begins and finishes one run of an agent on a task, failing it when failure is set.
func (f *fixture) record(t *testing.T, params recordParams) uuid.UUID {
	t.Helper()

	id, err := f.Begin(t.Context(), BeginParams{
		AgentID:   params.agentID,
		TaskID:    params.taskID,
		Trigger:   params.trigger,
		Script:    "setup",
		LogNumber: xptr.Ptr(params.number),
	})
	require.NoError(t, err)
	require.NoError(t, f.Finish(t.Context(), FinishParams{ID: id, Failure: params.failure}))

	return id
}

// taskFailedRun is whether a task's row carries the mark of a failed run nobody has looked at.
func (f *fixture) taskFailedRun(t *testing.T, taskID uuid.UUID) bool {
	t.Helper()

	tasks, err := taskstore.NewTaskStore(f.db).ListWatched(t.Context(), []uuid.UUID{taskID})
	require.NoError(t, err)
	require.Len(t, tasks, 1)

	return tasks[0].FailedRun
}

// TestTaskRunsKeepTaskEventsOffTheAgent pins the split between an agent's runs and its task's:
// the agent's mark counts only its own events, while the task's counts every run on it.
func TestTaskRunsKeepTaskEventsOffTheAgent(t *testing.T) {
	tests := []struct {
		name          string
		trigger       eventv1.Moment
		wantAgentMark bool
		wantTaskMark  bool
		markAgentRead bool
		markTaskRead  bool
	}{
		{
			name:          "a failed task event marks the task but not the agent that ran it",
			trigger:       eventv1.Moment_MOMENT_TASK_PRE_OPEN,
			wantTaskMark:  true,
			markAgentRead: true,
		},
		{
			name:         "opening a task's logs claims a failed task event",
			trigger:      eventv1.Moment_MOMENT_TASK_POST_ARCHIVE,
			wantTaskMark: true,
			markTaskRead: true,
		},
		{
			name:          "opening a task's logs claims a failed agent event on it too",
			trigger:       eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
			wantAgentMark: true,
			wantTaskMark:  true,
			markTaskRead:  true,
		},
		{
			name:          "a failed agent event marks both the agent and its task",
			trigger:       eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
			wantAgentMark: true,
			wantTaskMark:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)
			taskID, err := tasktest.Seed(t.Context(), f.db, f.workflow)
			require.NoError(t, err)
			agentID := f.seedAgentOn(t, taskID)

			f.record(t, recordParams{
				agentID: agentID,
				taskID:  taskID,
				trigger: tt.trigger,
				failure: "it failed",
				number:  1,
			})

			assert.Equal(t, tt.wantAgentMark, f.failedRun(t, agentID) != nil)
			assert.Equal(t, tt.wantTaskMark, f.taskFailedRun(t, *taskID))

			if tt.markAgentRead {
				require.NoError(t, f.MarkRead(t.Context(), agentID))
				assert.True(t, f.taskFailedRun(t, *taskID), "an agent's logs leave a task event unread")
			}
			if tt.markTaskRead {
				require.NoError(t, f.MarkTaskRead(t.Context(), *taskID))
				assert.False(t, f.taskFailedRun(t, *taskID))
				assert.Nil(t, f.failedRun(t, agentID))
			}
		})
	}
}

// TestListByTask pins a task's runs holding every agent's on it, of both kinds, newest first, and
// no other task's.
func TestListByTask(t *testing.T) {
	f := open(t)
	taskID, err := tasktest.Seed(t.Context(), f.db, f.workflow)
	require.NoError(t, err)
	writer := f.seedAgentOn(t, taskID)
	reviewer := f.seedAgentOn(t, taskID)
	otherTask, err := tasktest.Seed(t.Context(), f.db, f.workflow)
	require.NoError(t, err)
	elsewhere := f.seedAgentOn(t, otherTask)

	opened := f.record(t, recordParams{
		agentID: writer,
		taskID:  taskID,
		trigger: eventv1.Moment_MOMENT_TASK_PRE_OPEN,
		number:  1,
	})
	launched := f.record(t, recordParams{
		agentID: reviewer,
		taskID:  taskID,
		trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
		failure: "it failed",
		number:  1,
	})
	f.record(t, recordParams{
		agentID: elsewhere,
		taskID:  otherTask,
		trigger: eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH,
		number:  1,
	})
	runs, err := f.ListByTask(t.Context(), *taskID)
	require.NoError(t, err)

	assert.Equal(t, []uuid.UUID{launched, opened}, xslice.Map(runs, func(run Run) uuid.UUID { return run.ID }))
	assert.Equal(t, reviewer, runs[0].AgentID)
	assert.Equal(t, "it failed", runs[0].Failure)
}
