package tasks

import (
	"testing"
	"time"

	"komrad/internal/gittest"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const standRepository = "https://github.com/harrisoncramer/komrad"

// TestPlanStand pins which tasks a new task may stand on and what it records of each.
func TestPlanStand(t *testing.T) {
	tests := []struct {
		name    string
		branch  string
		cwd     string
		opened  bool
		wantErr error
		wantPR  bool
	}{
		{
			name:    "a task with no worktree is refused",
			branch:  "task/parent",
			wantErr: errStandNoWorktree,
		},
		{
			name:    "a task on the base branch is refused",
			branch:  "main",
			cwd:     "/tree",
			wantErr: errStandOnBase,
		},
		{
			name:   "a task with no pull request records its tip on origin alone",
			branch: "task/parent",
			cwd:    "/tree",
		},
		{
			name:   "a task with an open pull request records it too",
			branch: "task/parent",
			cwd:    "/tree",
			opened: true,
			wantPR: true,
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
			repository := standRepository
			require.NoError(t, stores.Tasks.SetHead(ctx, parent.ID, taskstore.SetHeadParams{RepositoryURL: &repository, Branch: &tt.branch}))
			var pullRequestID string
			if tt.opened {
				pullRequestID = seedStandPullRequest(t, stores, tt.branch)
			}
			svc := NewTaskService(NewTaskServiceParams{
				Tasks:        stores.Tasks,
				Workflows:    stores.Workflows,
				PullRequests: stores.PullRequests,
				Git:          &gittest.Mover{Tips: map[string]string{"task/parent": "tip"}},
			})

			stand, err := svc.PlanStand(ctx, PlanStandParams{WorkflowID: workflow.ID, Parent: "parent"})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, &taskstore.Stand{ParentID: parent.ID, Tip: "tip", PullRequestID: pullRequestID}, stand)
		})
	}
}

// TestRefuseStoodOn pins which deletes a task standing on another turns down.
func TestRefuseStoodOn(t *testing.T) {
	stores := storetest.Open(t)
	svc := NewTaskService(NewTaskServiceParams{Tasks: stores.Tasks})
	parent := stores.SeedTask(t, taskstore.CreateParams{Slug: "parent"})
	child := stores.SeedTask(t, taskstore.CreateParams{
		WorkflowID: parent.WorkflowID,
		Slug:       "child",
		Stand:      &taskstore.Stand{ParentID: parent.ID, Tip: "tip"},
	})

	tests := []struct {
		name    string
		ids     []string
		wantErr error
	}{
		{
			name:    "a task another stands on is refused",
			ids:     []string{parent.ID},
			wantErr: errStoodOn,
		},
		{
			name: "a task deleted with every task standing on it goes ahead",
			ids:  []string{parent.ID, child.ID},
		},
		{
			name: "a task standing on another goes ahead",
			ids:  []string{child.ID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := svc.RefuseStoodOn(t.Context(), tt.ids)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func seedStandPullRequest(t *testing.T, stores *storetest.Stores, branch string) string {
	t.Helper()

	opened := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	_, err := stores.PullRequests.Upsert(t.Context(), pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_parent",
		URL:               standRepository + "/pull/1",
		RepositoryURL:     standRepository,
		HeadRepositoryURL: standRepository,
		Number:            1,
		Title:             "parent",
		HeadBranch:        branch,
		HeadSHA:           "tip",
		BaseBranch:        "main",
		BaseSHA:           "base",
		State:             "OPEN",
		OpenedAt:          opened,
		ChangedAt:         opened,
	})
	require.NoError(t, err)

	pullRequest, err := stores.PullRequests.GetByIdentity(t.Context(), "github.com", "PR_parent")
	require.NoError(t, err)

	return pullRequest.ID
}
