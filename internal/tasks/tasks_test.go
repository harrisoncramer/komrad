package tasks

import (
	"testing"
	"time"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetTask pins which task an id or a slug names.
func TestGetTask(t *testing.T) {
	stores := storetest.Open(t)
	svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks})
	work := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})
	archived := stores.SeedTask(t, taskstore.CreateParams{Slug: "old-work"})
	require.NoError(t, stores.Tasks.Archive(t.Context(), archived.ID))

	tests := []struct {
		name     string
		ref      string
		wantErr  error
		wantSlug string
	}{
		{
			name:    "an unknown slug is not found",
			ref:     "missing",
			wantErr: store.ErrNotFound,
		},
		{
			name:    "an unknown id is not found",
			ref:     "01a11e6f-af0b-7497-91fb-d3750f8cbef2",
			wantErr: store.ErrNotFound,
		},
		{
			name:     "an id names its task",
			ref:      work.ID,
			wantSlug: "work",
		},
		{
			name:     "a slug names its task",
			ref:      "work",
			wantSlug: "work",
		},
		{
			name:     "an archived task is still found",
			ref:      "old-work",
			wantSlug: "old-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.GetTask(t.Context(), tt.ref)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSlug, got.Slug)
		})
	}
}

// TestArchiveTask pins that a slug archives its task.
func TestArchiveTask(t *testing.T) {
	stores := storetest.Open(t)
	svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks})
	work := stores.SeedTask(t, taskstore.CreateParams{Slug: "work"})

	require.ErrorIs(t, svc.ArchiveTask(t.Context(), "missing"), store.ErrNotFound)
	require.NoError(t, svc.ArchiveTask(t.Context(), "work"))

	got, err := stores.Tasks.Get(t.Context(), work.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.ArchivedAt)
}

// TestGetTaskForWorkflow pins which tasks an agent being created may be put on.
func TestGetTaskForWorkflow(t *testing.T) {
	stores := storetest.Open(t)
	svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks})
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	other := stores.SeedWorkflow(t, workflowstore.Config{Name: "other"})
	stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})
	stores.SeedTask(t, taskstore.CreateParams{WorkflowID: other.ID, Slug: "elsewhere"})
	archived := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "old-work"})
	require.NoError(t, stores.Tasks.Archive(t.Context(), archived.ID))

	tests := []struct {
		name    string
		ref     string
		wantErr error
	}{
		{
			name:    "an unknown task is not found",
			ref:     "missing",
			wantErr: store.ErrNotFound,
		},
		{
			name:    "a task in another workflow is refused",
			ref:     "elsewhere",
			wantErr: ErrOtherWorkflow,
		},
		{
			name:    "an archived task is refused",
			ref:     "old-work",
			wantErr: errArchived,
		},
		{
			name: "a live task in the workflow is joined",
			ref:  "work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := svc.GetTaskForWorkflow(t.Context(), tt.ref, workflow.ID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.ref, got.Slug)
		})
	}
}

// TestCreateForAgent pins the task an agent created without one is given.
func TestCreateForAgent(t *testing.T) {
	tests := []struct {
		name      string
		taken     []string
		params    CreateForAgentParams
		wantSlug  string
		wantTitle string
		wantGoal  string
	}{
		{
			name:      "a name with nothing sluggable is called task",
			params:    CreateForAgentParams{Name: "!!!", Prompt: "go"},
			wantSlug:  "task",
			wantTitle: "!!!",
			wantGoal:  "go",
		},
		{
			name:      "a taken slug is suffixed until it is free",
			taken:     []string{"fix-login", "fix-login-2"},
			params:    CreateForAgentParams{Name: "fix-login", Prompt: "go"},
			wantSlug:  "fix-login-3",
			wantTitle: "fix-login",
			wantGoal:  "go",
		},
		{
			name:      "the slug comes from the name and the goal from the prompt's first line",
			params:    CreateForAgentParams{Name: "Fix Login", Prompt: "  Fix the login page.  \nMore detail."},
			wantSlug:  "fix-login",
			wantTitle: "Fix Login",
			wantGoal:  "Fix the login page.",
		},
		{
			name:      "a prompt opening on a blank line takes its first line with text as the goal",
			params:    CreateForAgentParams{Name: "fix-login", Prompt: "\n\n  Fix the login page.\nMore."},
			wantSlug:  "fix-login",
			wantTitle: "fix-login",
			wantGoal:  "Fix the login page.",
		},
		{
			name:      "a title is kept over the name",
			params:    CreateForAgentParams{Name: "fix-login", Title: " Login is broken ", Prompt: "go"},
			wantSlug:  "fix-login",
			wantTitle: "Login is broken",
			wantGoal:  "go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
			for _, slug := range tt.taken {
				stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: slug})
			}

			params := tt.params
			params.WorkflowID = workflow.ID
			got, err := CreateForAgent(t.Context(), stores.Tasks, params)
			require.NoError(t, err)

			assert.Equal(t, tt.wantSlug, got.Slug)
			assert.Equal(t, tt.wantTitle, got.Title)
			assert.Equal(t, tt.wantGoal, got.Goal)
			assert.Equal(t, workflow.ID, got.WorkflowID)
		})
	}
}

// TestSetTaskNotes pins who may write a task's notes and that a write over a stale version is refused.
func TestSetTaskNotes(t *testing.T) {
	stores := storetest.Open(t)
	svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks, Agents: stores.Agents})
	workflow := stores.SeedWorkflow(t, workflowstore.Config{Name: "komrad"})
	elsewhere := stores.SeedWorkflow(t, workflowstore.Config{Name: "elsewhere"})
	work := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "work"})
	other := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "other"})
	writer := stores.SeedAgent(t, agentstore.Agent{Name: "writer", WorkflowID: workflow.ID, TaskID: &work.ID})
	reviewer := stores.SeedAgent(t, agentstore.Agent{Name: "reviewer", WorkflowID: workflow.ID, ParentAgentID: &writer.ID, TaskID: &work.ID})
	stranger := stores.SeedAgent(t, agentstore.Agent{Name: "stranger", WorkflowID: workflow.ID, TaskID: &other.ID})
	orchestrator := stores.SeedAgent(t, agentstore.Agent{Name: "orchestrator", WorkflowID: workflow.ID, Role: agentstore.RoleOrchestrator})
	farOrchestrator := stores.SeedAgent(t, agentstore.Agent{Name: "far", WorkflowID: elsewhere.ID, Role: agentstore.RoleOrchestrator})

	tests := []struct {
		name      string
		callerID  string
		stale     bool
		viaUpdate bool
		wantErr   error
	}{
		{
			name:     "an agent on another task is refused",
			callerID: stranger.ID,
			wantErr:  ErrNotesNotYours,
		},
		{
			name:     "the orchestrator of another workflow is refused",
			callerID: farOrchestrator.ID,
			wantErr:  ErrNotesNotYours,
		},
		{
			name:      "the orchestrator of another workflow is refused through an update",
			callerID:  farOrchestrator.ID,
			viaUpdate: true,
			wantErr:   ErrNotesNotYours,
		},
		{
			name:     "a write over a version since rewritten is refused",
			callerID: writer.ID,
			stale:    true,
			wantErr:  taskstore.ErrNotesChanged,
		},
		{
			name:     "the writer on the task writes them",
			callerID: writer.ID,
		},
		{
			name:     "a child on the task writes them",
			callerID: reviewer.ID,
		},
		{
			name:     "the workflow's orchestrator writes them",
			callerID: orchestrator.ID,
		},
		{
			name: "the person writes them",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current, err := stores.Tasks.Get(t.Context(), work.ID)
			require.NoError(t, err)

			read := current.NotesUpdatedAt
			if tt.stale {
				read = xptr.Ptr(time.Now().Add(-time.Hour))
			}

			var got *taskstore.Task
			if tt.viaUpdate {
				got, err = svc.UpdateTask(t.Context(), UpdateTaskParams{
					Ref:           "work",
					Fields:        taskstore.UpdateParams{Notes: &tt.name, ReadNotesUpdatedAt: read},
					CallerAgentID: tt.callerID,
				})
			} else {
				got, err = svc.SetTaskNotes(t.Context(), SetTaskNotesParams{
					Ref:                "work",
					Notes:              tt.name,
					ReadNotesUpdatedAt: read,
					CallerAgentID:      tt.callerID,
				})
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.name, got.Notes)
		})
	}
}
