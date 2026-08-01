package agents

import (
	"testing"

	"weave/internal/store/schedule"
	"weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWorkflow(t *testing.T) {
	tests := []struct {
		name     string
		cfg      workflow.Config
		wantName string
	}{
		{
			name:     "a blank name falls back to a label, so the list never renders an empty row",
			cfg:      workflow.Config{Name: "   "},
			wantName: "workflow",
		},
		{
			name:     "a given name is kept",
			cfg:      workflow.Config{Name: "backend"},
			wantName: "backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created, err := workflowsFor(newStore(t)).NewWorkflow(t.Context(), tt.cfg)
			require.NoError(t, err)
			require.NotNil(t, created)
			assert.Equal(t, tt.wantName, created.Name)
		})
	}
}

func TestUpdateWorkflow(t *testing.T) {
	tests := []struct {
		name      string
		create    bool
		cfg       workflow.Config
		wantErrIs error
		wantName  string
	}{
		{
			name:      "an id nobody holds",
			cfg:       workflow.Config{Name: "ghost"},
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:     "a blank name keeps the one it had",
			create:   true,
			cfg:      workflow.Config{Name: "   "},
			wantName: "backend",
		},
		{
			name:     "a new name is written",
			create:   true,
			cfg:      workflow.Config{Name: "platform"},
			wantName: "platform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			workflows := workflowsFor(st)

			id := "00000000-0000-0000-0000-000000000000"
			if tt.create {
				created, err := workflows.NewWorkflow(t.Context(), workflow.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID
			}

			err := workflows.UpdateWorkflow(t.Context(), id, tt.cfg)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
				return
			}

			require.NoError(t, err)

			stored, err := st.GetWorkflow(t.Context(), id)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, tt.wantName, stored.Name)
		})
	}
}

// TestArchiveWorkflow covers what becomes of the agents inside, and what stops
// the archive. Both halves commit together, so an agent is never left pointing at
// a workflow nothing lists.
func TestArchiveWorkflow(t *testing.T) {
	tests := []struct {
		name   string
		create bool
		// withSchedule leaves a live schedule launching into the workflow, which is
		// what refuses the delete.
		withSchedule bool
		wantErrIs    error
	}{
		{
			name:      "an id nobody holds",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:   "the agents are archived alongside it, still findable in search",
			create: true,
		},
		{
			// Deleting it would leave the schedule with nowhere to run, and switching
			// off recurring work as a side effect of tidying is not something to do
			// quietly.
			name:         "a workflow still scheduled into is refused",
			create:       true,
			withSchedule: true,
			wantErrIs:    ErrWorkflowHasSchedules,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			manager := newManager(newFakeTmux(), st, "")
			workflows := workflowsFor(st)
			ctx := t.Context()

			id := "00000000-0000-0000-0000-000000000000"
			if tt.create {
				created, err := workflows.NewWorkflow(ctx, workflow.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID

				_, err = manager.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			if tt.withSchedule {
				st.SeedSchedule(t, schedule.Config{
					Name: "nightly", Cron: "0 2 * * *", Prompt: "sweep", Enabled: true, WorkflowID: id,
				})
			}

			err := workflows.ArchiveWorkflow(ctx, id)
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				if tt.create {
					listed, listErr := st.Workflows.List(ctx)
					require.NoError(t, listErr)
					assert.Len(t, listed, 1, "a refused delete leaves the workflow alone")
				}

				return
			}

			require.NoError(t, err)

			listed, err := st.Workflows.List(ctx)
			require.NoError(t, err)
			assert.Empty(t, listed, "the workflow is hidden")

			agent, err := st.Get(ctx, "agent")
			require.NoError(t, err)
			assert.Nil(t, agent, "the agent is hidden from the live listing")

			all, err := st.ListAll(ctx)
			require.NoError(t, err)
			require.Len(t, all, 1)
			assert.NotNil(t, all[0].ArchivedAt, "and archived rather than removed")
		})
	}
}

// TestRestoreWorkflow covers taking a workflow out of the archive. Its agents come back
// with it, as rows rather than as running CLIs.
func TestRestoreWorkflow(t *testing.T) {
	tests := []struct {
		name string
		// archive says the workflow was archived first, the only state a restore acts on.
		archive bool
		// takeName leaves a live agent holding the archived agent's name, which the
		// unique index would refuse as it comes back.
		takeName  bool
		wantErrIs error
	}{
		{
			name:      "an id nobody holds",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:      "a workflow that is not archived",
			wantErrIs: ErrWorkflowNotFound,
		},
		{
			name:     "the agents inside come back with it",
			archive:  true,
			takeName: false,
		},
		{
			name:     "an agent whose name was taken comes back renamed",
			archive:  true,
			takeName: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			tmux := newFakeTmux()
			manager := newManager(tmux, st, "")
			workflows := workflowsFor(st)
			ctx := t.Context()

			id := "00000000-0000-0000-0000-000000000000"
			if tt.name != "an id nobody holds" {
				created, err := workflows.NewWorkflow(ctx, workflow.Config{Name: "backend"})
				require.NoError(t, err)
				id = created.ID

				_, err = manager.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: id,
				})
				require.NoError(t, err)
			}
			if tt.archive {
				require.NoError(t, workflows.ArchiveWorkflow(ctx, id))
			}
			if tt.takeName {
				other, err := workflows.NewWorkflow(ctx, workflow.Config{Name: "frontend"})
				require.NoError(t, err)
				_, err = manager.NewAgent(ctx, NewAgentParams{
					Title:      "agent",
					Dir:        t.TempDir(),
					WorkflowID: other.ID,
				})
				require.NoError(t, err)
			}

			opened := len(tmux.newWindows)
			restored, err := workflows.RestoreWorkflow(ctx, id)

			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, restored)

			listed, err := st.Workflows.List(ctx)
			require.NoError(t, err)
			assert.Contains(t, workflowNames(listed), "backend", "the workflow is back in the listing")

			live, err := st.List(ctx)
			require.NoError(t, err)
			names := make([]string, 0, len(live))
			for _, agent := range live {
				if agent.WorkflowID == id {
					names = append(names, agent.Name)
				}
			}
			require.Len(t, names, 1, "the workflow's agent is listed again")
			assert.Len(t, tmux.newWindows, opened, "and nothing was started for it")

			if tt.takeName {
				assert.NotEqual(t, "agent", names[0], "the namesake keeps the name it took")

				return
			}

			assert.Equal(t, "agent", names[0])
		})
	}
}

// TestRestoreWorkflowCeiling covers a folder that no longer fits. A restore without room
// for every agent inside writes nothing.
func TestRestoreWorkflowCeiling(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflows := st.workflowServiceCapped(3)
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflow.Config{Name: "backend"})
	require.NoError(t, err)

	for _, title := range []string{"one", "two"} {
		_, err := manager.NewAgent(ctx, NewAgentParams{Title: title, Dir: t.TempDir(), WorkflowID: created.ID})
		require.NoError(t, err)
	}
	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	elsewhere, err := workflows.NewWorkflow(ctx, workflow.Config{Name: "frontend"})
	require.NoError(t, err)
	for _, title := range []string{"live-one", "live-two"} {
		_, err := manager.NewAgent(ctx, NewAgentParams{Title: title, Dir: t.TempDir(), WorkflowID: elsewhere.ID})
		require.NoError(t, err)
	}

	_, err = workflows.RestoreWorkflow(ctx, created.ID)

	require.ErrorIs(t, err, ErrTooManyAgents)
	listed, err := st.Workflows.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1, "the refused restore leaves the workflow archived")
	assert.Equal(t, "frontend", listed[0].Name)

	archived, err := st.Agents.ListArchivedInWorkflow(ctx, created.ID)
	require.NoError(t, err)
	assert.Len(t, archived, 2, "and both of its agents with it")
}

// workflowNames is the names of a listing, for asserting which workflows a read holds.
func workflowNames(listed []workflow.Workflow) []string {
	names := make([]string, 0, len(listed))
	for _, w := range listed {
		names = append(names, w.Name)
	}

	return names
}

// TestRestoreAgentRestoresItsWorkflow covers the other direction. One agent out of an
// archived workflow brings the workflow with it, or it is live under a header the tree
// does not list and gets no row anywhere.
func TestRestoreAgentRestoresItsWorkflow(t *testing.T) {
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")
	workflows := workflowsFor(st)
	ctx := t.Context()

	created, err := workflows.NewWorkflow(ctx, workflow.Config{Name: "backend"})
	require.NoError(t, err)

	agent, err := manager.NewAgent(ctx, NewAgentParams{Title: "one", Dir: t.TempDir(), WorkflowID: created.ID})
	require.NoError(t, err)
	other, err := manager.NewAgent(ctx, NewAgentParams{Title: "two", Dir: t.TempDir(), WorkflowID: created.ID})
	require.NoError(t, err)

	require.NoError(t, workflows.ArchiveWorkflow(ctx, created.ID))

	restored, err := manager.RestoreAgent(ctx, agent.ID)
	require.NoError(t, err)
	require.NotNil(t, restored)

	listed, err := st.Workflows.List(ctx)
	require.NoError(t, err)
	assert.Contains(t, workflowNames(listed), "backend", "the workflow came back with its agent")

	siblings, err := st.Agents.ListArchivedInWorkflow(ctx, created.ID)
	require.NoError(t, err)
	require.Len(t, siblings, 1, "the agents nobody asked for stay archived")
	assert.Equal(t, other.ID, siblings[0].ID)
}
