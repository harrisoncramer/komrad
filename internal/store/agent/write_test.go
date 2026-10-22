package agent

import (
	"testing"

	"komrad/db/testmodels"
	"komrad/internal/store"
	taskstore "komrad/internal/store/task"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateNeedsATask pins that every agent but an orchestrator is written onto a task.
func TestCreateNeedsATask(t *testing.T) {
	tests := []struct {
		name    string
		role    Role
		wantErr bool
	}{
		{
			name:    "a writer with no task is refused",
			role:    RoleWriter,
			wantErr: true,
		},
		{
			name:    "a reviewer with no task is refused",
			role:    RoleReviewer,
			wantErr: true,
		},
		{
			name: "an orchestrator needs none",
			role: RoleOrchestrator,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := open(t)

			err := f.AgentStore.Create(t.Context(), Agent{
				Name:       "agent",
				State:      StateIdle,
				Role:       tt.role,
				WorkflowID: f.defaultWorkflow,
			})

			if tt.wantErr {
				require.Error(t, err)

				return
			}
			require.NoError(t, err)
		})
	}
}

// TestDeleteNullsSpawnedByAgentID pins that deleting the orchestrator that spawned a writer nulls
// the reference rather than taking the writer with it.
func TestDeleteNullsSpawnedByAgentID(t *testing.T) {
	f := open(t)
	ctx := t.Context()

	require.NoError(t, f.Create(ctx, Agent{
		Name:  "orchestrator",
		Role:  RoleOrchestrator,
		State: StateIdle,
	}))
	orchestrator, err := f.Get(ctx, "orchestrator")
	require.NoError(t, err)
	require.NotNil(t, orchestrator)

	require.NoError(t, f.Create(ctx, Agent{
		Name:             "writer",
		SpawnedByAgentID: &orchestrator.ID,
		State:            StateIdle,
	}))
	writer, err := f.Get(ctx, "writer")
	require.NoError(t, err)
	require.NotNil(t, writer)
	require.NotNil(t, writer.SpawnedByAgentID)

	_, deleteErr := f.Delete(ctx, []uuid.UUID{orchestrator.ID})

	require.NoError(t, deleteErr)

	stillThere, err := f.GetByID(ctx, writer.ID)
	require.NoError(t, err)
	require.NotNil(t, stillThere, "the writer survives its orchestrator's deletion")
	assert.Nil(t, stillThere.SpawnedByAgentID, "the reference is nulled rather than left dangling")
}

// TestRename pins that a rename onto a live agent's name comes back as the caller's mistake
// while a rename onto a name only an archived row holds is allowed.
func TestRename(t *testing.T) {
	tests := []struct {
		name         string
		target       uuid.UUID
		archiveOther bool
		to           string
		wantErr      error
		wantRenamed  bool
	}{
		{
			name:   "an id no agent holds touches nothing",
			target: uuid.MustParse("00000000-0000-0000-0000-0000000000ff"),
			to:     "free",
		},
		{
			name:    "a name a live agent already holds",
			to:      "taken",
			wantErr: store.ErrNameTaken,
		},
		{
			name:         "a name only an archived row holds is free again",
			archiveOther: true,
			to:           "taken",
			wantRenamed:  true,
		},
		{
			name:        "the name it already answers to",
			to:          "agent",
			wantRenamed: true,
		},
		{
			name:        "a name nobody holds",
			to:          "renamed",
			wantRenamed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{
				Name:  "agent",
				Pane:  "%1",
				State: StateIdle,
			}))
			require.NoError(t, st.Create(ctx, Agent{
				Name:  "taken",
				Pane:  "%2",
				State: StateIdle,
			}))
			if tt.archiveOther {
				require.NoError(t, st.archiveByName(ctx, "taken"))
			}

			created, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			require.NotNil(t, created)

			target := tt.target
			if target == uuid.Nil {
				target = created.ID
			}

			renamed, err := st.Rename(ctx, target, tt.to)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.False(t, renamed)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantRenamed, renamed)

			if !tt.wantRenamed {
				return
			}

			under, err := st.Get(ctx, tt.to)
			require.NoError(t, err)
			require.NotNil(t, under)
			assert.Equal(t, created.ID, under.ID, "the rename kept the agent's id")
		})
	}
}

// TestDeleteInBatches covers the hard delete over a list.
func TestDeleteInBatches(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	ids := make([]uuid.UUID, 0, 3)
	for _, name := range []string{"auth", "api", "db"} {
		require.NoError(t, st.Create(ctx, Agent{Name: name, State: StateIdle}))

		created, err := st.Get(ctx, name)
		require.NoError(t, err)
		require.NotNil(t, created)
		ids = append(ids, created.ID)
	}

	_, deleteErr := st.Delete(ctx, []uuid.UUID{ids[0], uuid.New(), ids[2]})

	require.NoError(t, deleteErr)

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the id that could hold no row took nothing with it")
	assert.Equal(t, "api", all[0].Name)
}

// TestOneOrchestratorPerWorkflow pins the index that makes the rule true.
func TestOneOrchestratorPerWorkflow(t *testing.T) {
	tests := []struct {
		name    string
		archive bool
	}{
		{name: "while the first is live"},
		{name: "and after it has been archived", archive: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			require.NoError(t, st.Create(ctx, Agent{
				Name:  "first",
				State: StateIdle,
				Role:  RoleOrchestrator,
			}))

			if tt.archive {
				first, err := st.GetInWorkflow(ctx, st.defaultWorkflow, "first")
				require.NoError(t, err)
				require.NotNil(t, first)
				_, err = st.Archive(ctx, []uuid.UUID{first.ID})
				require.NoError(t, err)
			}

			err := st.Create(ctx, Agent{
				Name:  "second",
				State: StateIdle,
				Role:  RoleOrchestrator,
			})
			require.Error(t, err, "a workflow holds one orchestrator for life")
			require.ErrorContains(t, err, "agents_workflow_orchestrator",
				"the index is what refused it")

			require.NoError(t, st.Create(ctx, Agent{Name: "worker", State: StateIdle}))
		})
	}
}

// TestDeletingATaskAnAgentIsOnIsRefused pins that a task keeps every agent working on it.
func TestDeletingATaskAnAgentIsOnIsRefused(t *testing.T) {
	f := open(t)
	ctx := t.Context()
	task, err := taskstore.NewTaskStore(f.db).Create(ctx, taskstore.CreateParams{
		WorkflowID: f.defaultWorkflow,
		Slug:       "work",
		Title:      "Work",
	})
	require.NoError(t, err)
	require.NoError(t, f.Create(ctx, Agent{
		Name:   "worker",
		State:  StateIdle,
		TaskID: &task.ID,
	}))
	worker, err := f.Get(ctx, "worker")
	require.NoError(t, err)

	require.Error(t, testmodels.New(f.db).DeleteTask(ctx, task.ID.String()))

	got, err := f.GetByID(ctx, worker.ID)
	require.NoError(t, err)
	assert.Equal(t, &task.ID, got.TaskID, "the agent is still on its task")
}
