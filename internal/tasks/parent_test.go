package tasks

import (
	"testing"

	"komrad/internal/gittest"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const parentRepository = "https://github.com/harrisoncramer/komrad"

// TestPlanParent pins which tasks a new task may depend on and what it records of each.
func TestPlanParent(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		cwd     string
		opened  bool
		wantErr error
	}{
		{
			name:    "a task with no worktree is refused",
			branch:  "task/parent",
			wantErr: errParentNoWorktree,
		},
		{
			name:    "a task on the base branch is refused",
			branch:  "main",
			cwd:     "/tree",
			wantErr: errParentOnBase,
		},
		{
			name:   "a task with no pull request records its newest commit on origin alone",
			branch: "task/parent",
			cwd:    "/tree",
		},
		{
			name:   "a task with an open pull request records it too",
			branch: "task/parent",
			cwd:    "/tree",
			opened: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stores := storetest.Open(t)
			ctx := t.Context()
			workflow := stores.SeedWorkflow(t, workflowstore.Config{
				Name:     "flow",
				Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
			})
			parent := stores.SeedTask(t, taskstore.CreateParams{WorkflowID: workflow.ID, Slug: "parent"})
			if tt.cwd != "" {
				require.NoError(t, stores.Tasks.SetCheckoutDir(ctx, parent.ID, tt.cwd))
			}
			repository := parentRepository
			require.NoError(t, stores.Tasks.SetHead(ctx, parent.ID, taskstore.SetHeadParams{RepositoryURL: &repository, Branch: &tt.branch}))
			var pullRequestID uuid.UUID
			if tt.opened {
				pullRequestID = stores.SeedPullRequest(t, storetest.SeedPullRequestParams{
					RepositoryURL: parentRepository,
					Number:        1,
					HeadBranch:    tt.branch,
					HeadSHA:       "newest",
					BaseBranch:    "main",
				}).ID
			}
			svc := NewTaskService(NewTaskServiceParams{
				Tasks:        stores.Tasks,
				Workflows:    stores.Workflows,
				PullRequests: stores.PullRequests,
				Git:          &gittest.Mover{Tips: map[string]string{"task/parent": "newest"}},
			})

			planned, err := svc.PlanParent(ctx, PlanParentParams{WorkflowID: workflow.ID, Parent: taskByID(parent.ID)})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, &taskstore.Parent{TaskID: parent.ID, ForkPoint: "newest", PullRequestID: &pullRequestID}, planned)
		})
	}
}

// TestRefuseParentDelete pins which deletes of a parent and its children go ahead.
func TestRefuseParentDelete(t *testing.T) {
	stores := storetest.Open(t)
	svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks})
	parent := stores.SeedTask(t, taskstore.CreateParams{Slug: "parent"})
	child := stores.SeedTask(t, taskstore.CreateParams{
		WorkflowID: parent.WorkflowID,
		Slug:       "child",
		Parent:     &taskstore.Parent{TaskID: parent.ID, ForkPoint: "abc"},
	})

	tests := []struct {
		name    string
		ids     []uuid.UUID
		wantErr error
	}{
		{
			name:    "a parent is refused",
			ids:     []uuid.UUID{parent.ID},
			wantErr: errHasChildren,
		},
		{
			name: "a parent deleted with all its children goes ahead",
			ids:  []uuid.UUID{parent.ID, child.ID},
		},
		{
			name: "a child goes ahead",
			ids:  []uuid.UUID{child.ID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.RefuseParentDelete(t.Context(), tt.ids)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
