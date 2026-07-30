package agents

import (
	"testing"

	"claude-agents/internal/store/schedule"
	"claude-agents/internal/store/workflow"

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

// TestDeleteWorkflow covers what becomes of the agents inside, and what stops
// the delete. Both halves commit together, so an agent is never left pointing at
// a workflow nothing lists.
func TestDeleteWorkflow(t *testing.T) {
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

			err := workflows.DeleteWorkflow(ctx, id)
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
			assert.True(t, all[0].Deleted, "and archived rather than removed")
		})
	}
}
