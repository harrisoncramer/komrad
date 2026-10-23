package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	taskv1 "komrad/proto/gen/task/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestViews covers the switcher rendered over every list and the keys that move between views.
func TestViews(t *testing.T) {
	t.Run("a page opened from a view renders the trail back to it beside the logo", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), paneltest.Reviewer("reviewer", "reviewer", "writer"))
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")

		assert.True(t, strings.HasPrefix(paneltest.Trail(harness.Frame()), "agents › reviewer › logs"))
	})

	tests := []struct {
		name       string
		press      []string
		wantScreen screen.ID
	}{
		{
			name:       "tab opens the next view",
			press:      []string{"tab"},
			wantScreen: screen.Schedules,
		},
		{
			name:       "shift-tab opens the previous view, wrapping to the last",
			press:      []string{"shift+tab"},
			wantScreen: screen.Worktrees,
		},
		{
			name:       "the switcher keeps tab through each view it opens",
			press:      []string{"tab", "tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "the switcher keeps shift-tab through each view it opens",
			press:      []string{"shift+tab", "shift+tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "escape hands the keys back to the view it stands on",
			press:      []string{"tab", "esc", "x"},
			wantScreen: screen.Schedules,
		},
		{
			name:       "escape on the agents opens the switcher without moving",
			press:      []string{"esc"},
			wantScreen: screen.Agents,
		},
		{
			name:       "escape on the agents hands tab to the switcher",
			press:      []string{"esc", "tab", "tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "escape on the schedules opens the switcher",
			press:      []string{"tab", "esc", "esc", "tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "escape on the documents opens the switcher",
			press:      []string{"tab", "tab", "esc", "esc", "tab"},
			wantScreen: screen.Worktrees,
		},
		{
			name:       "escape on the worktrees opens the switcher",
			press:      []string{"shift+tab", "esc", "esc", "tab"},
			wantScreen: screen.Agents,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})

			harness.Press(tt.press...)

			assert.Equal(t, tt.wantScreen, paneltest.Screen(harness.Frame()))
		})
	}

	t.Run("the list keeps its arrow while the switcher is open", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})
		harness.Press(paneltest.WidenListPresses...)
		hovered := paneltest.HoveredRow(harness.Frame())
		require.NotEmpty(t, hovered)

		harness.Press("esc")
		require.Contains(t, harness.Frame(), "tab/S-tab switch")
		assert.Equal(t, hovered, paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("a key the switcher has no use for closes it and reaches the view", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})

		harness.Press(append(paneltest.WidenListPresses, "tab", "G")...)

		frame := harness.Frame()
		assert.Equal(t, screen.Schedules, paneltest.Screen(harness.Frame()))
		assert.NotEmpty(t, paneltest.HoveredRow(frame), "the switcher let the list have the keys")
		assert.Equal(t, "schedules › agent00", paneltest.Trail(frame), "and the list took the key")
	})
}

// TestTrail covers the trail under the switcher, from the view that is up to the row the cursor is
// on.
func TestTrail(t *testing.T) {
	tests := []struct {
		name      string
		press     []string
		wantTrail string
	}{
		{
			name:      "the agents trail runs to the hovered agent",
			press:     []string{"G"},
			wantTrail: "agents › agent39",
		},
		{
			name:      "the schedules trail runs to the hovered schedule",
			press:     []string{"tab", "G"},
			wantTrail: "schedules › agent00",
		},
		{
			name:      "the archived agents trail runs to the hovered agent",
			press:     []string{"F", "tab", "esc", "G"},
			wantTrail: "agents › agent39",
		},
		{
			name:      "the worktrees trail runs to the hovered worktree",
			press:     []string{"shift+tab", "G"},
			wantTrail: "worktrees › task/agent39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})

			harness.Press(tt.press...)

			assert.Equal(t, tt.wantTrail, paneltest.Trail(harness.Frame()))
		})
	}

	t.Run("an empty view's trail is the view alone", func(t *testing.T) {
		harness := paneltest.OpenAgents(t)

		assert.Equal(t, "agents", paneltest.Trail(harness.Frame()))
	})

	t.Run("the trail follows the cursor", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})

		harness.Press("G")
		require.Equal(t, "agents › agent39", paneltest.Trail(harness.Frame()))

		harness.Press("k")
		assert.Equal(t, "agents › agent38", paneltest.Trail(harness.Frame()))
	})
}

// TestWorktreesLanding covers the worktrees view opening on the directory of the row the cursor
// was on.
func TestWorktreesLanding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		press       []string
		wantHovered string
	}{
		{
			name:        "from the agents list it lands on the hovered agent's directory",
			press:       []string{"shift+tab", "esc"},
			wantHovered: "treeview",
		},
		{
			name:        "a task lands on its writer's directory",
			press:       []string{"j", "shift+tab", "esc"},
			wantHovered: "treeview",
		},
		{
			name:        "an occupant lands on the directory it works in",
			press:       []string{"j", "j", "shift+tab", "esc"},
			wantHovered: "treeview",
		},
		{
			name:        "from the archived agents it lands on the archived agent's directory",
			press:       []string{"F", "tab", "esc", "j", "shift+tab", "esc"},
			wantHovered: "reviewer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := paneltest.OpenAgents(t, buildWorktreeRows()...)
			harness.AnswerWorktreeReads()

			harness.Press(tt.press...)

			frame := harness.Frame()
			assert.Equal(t, screen.Worktrees, paneltest.Screen(frame))
			assert.Equal(t, tt.wantHovered, strings.TrimSpace(paneltest.HoveredRow(frame)))
		})
	}
}

// TestStatusChecksOpen covers the status checks screen opening from the subpages menu.
func TestStatusChecksOpen(t *testing.T) {
	t.Parallel()

	t.Run("a task with no pull request has no status checks to list", func(t *testing.T) {
		t.Parallel()

		harness := openTaskChecks(t)

		frame := harness.Frame()
		assert.Equal(t, screen.StatusChecks, paneltest.Screen(frame))
		assert.Contains(t, frame, "agents › author › status checks")
		assert.Contains(t, frame, "no pull request")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openTaskChecks(t)
		harness.Press("esc")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Contains(t, paneltest.HoveredRow(harness.Frame()), "author")
	})
}

// TestReviewOpen covers the review opening from the subpages menu.
func TestReviewOpen(t *testing.T) {
	t.Parallel()

	t.Run("review answers its letter", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		harness.Watch(reviewed.snapshot())
		harness.Press("space")
		menuUp := len(harness.Tmux().Zooms())
		require.True(t, harness.Tmux().IsZoomed())

		harness.Press("r")

		assert.Equal(t, screen.Review, paneltest.Screen(harness.Frame()))
		assert.NotContains(t, harness.Tmux().Zooms()[menuUp:], false, "the list is not rendered again on the way")
	})

	t.Run("a task's row opens the review of its task", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		reviewed.task.Slug = "login-fix"
		harness.Watch(reviewed.snapshot())
		require.Contains(t, paneltest.HoveredRow(harness.Frame()), "login-fix")

		harness.Press("space", "r")

		assert.Equal(t, screen.Review, paneltest.Screen(harness.Frame()))
		harness.AssertSentTo(agentv1connect.AgentServiceOpenReviewHistoryProcedure, &agentv1.OpenReviewHistoryRequest{Task: &agentv1.OpenReviewHistoryRequest_TaskId{TaskId: "t1"}})
	})

	t.Run("an agent whose worktree was removed in a workflow with no directory is not opened", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		reviewed.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		reviewed.task.Snapshots = 1
		harness.Watch(reviewed.snapshot())

		harness.Press("space", "r")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})

	t.Run("an agent whose worktree was removed with no snapshot is not opened", func(t *testing.T) {
		t.Parallel()

		harness, reviewed := startReviewed(t, paneltest.Params{})
		reviewed.task.WorktreeRemovalReason = taskv1.WorktreeRemovalReason_WORKTREE_REMOVAL_REASON_GONE
		reviewed.workflow.Cwd = reviewed.agent.GetCwd()
		harness.Watch(reviewed.snapshot())

		harness.Press("space", "r")

		assert.Equal(t, screen.Agents, paneltest.Screen(harness.Frame()))
		assert.Empty(t, harness.Sent(agentv1connect.AgentServiceOpenReviewHistoryProcedure))
	})
}

// openTaskChecks opens the status checks of a task with no pull request from the agents list.
func openTaskChecks(t *testing.T) *paneltest.Panel {
	t.Helper()

	author := paneltest.Agent("author", "author", "w1")
	harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), author, paneltest.TaskOf(author))
	harness.MoveToRow("task-author")
	harness.Press("space", "x")

	return harness
}

// buildWorktreeRows is a live task's checkout holding its writer and an occupant, two archived
// tasks' checkouts, and one no task answers for.
func buildWorktreeRows() []proto.Message {
	live := paneltest.Agent("live", "treeview", "w1")
	live.Cwd = "/trees/live"
	live.Alive = xptr.Ptr(true)
	live.State = agentv1.AgentState_AGENT_STATE_THINKING
	live.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	live.TaskId = xptr.Ptr("live")

	occupant := paneltest.Agent("occ", "review-607", "w1")
	occupant.Cwd = "/trees/live"
	occupant.Alive = xptr.Ptr(true)

	archived := paneltest.Agent("archived", "reviewer", "w1")
	archived.Cwd = "/trees/archived"
	archived.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	archived.ArchivedAt = paneltest.ArchivedAt
	archived.TaskId = xptr.Ptr("archived")

	admin := paneltest.Agent("admin", "cargo-fix", "w1")
	admin.Cwd = "/trees/admin"
	admin.Role = agentv1.AgentRole_AGENT_ROLE_WRITER
	admin.ArchivedAt = paneltest.ArchivedAt
	admin.TaskId = xptr.Ptr("admin")

	return []proto.Message{
		paneltest.Workflow("w1", "komrad"),
		live,
		occupant,
		archived,
		admin,
		&taskv1.Task{
			Id:         "live",
			WorkflowId: "w1",
			Slug:       "treeview",
			Cwd:        xptr.Ptr("/trees/live"),
		},
		&taskv1.Task{
			Id:         "archived",
			WorkflowId: "w1",
			Slug:       "reviewer",
			Cwd:        xptr.Ptr("/trees/archived"),
			ArchivedAt: paneltest.ArchivedAt,
		},
		&taskv1.Task{
			Id:         "admin",
			WorkflowId: "w1",
			Slug:       "cargo-fix",
			Cwd:        xptr.Ptr("/trees/admin"),
			ArchivedAt: paneltest.ArchivedAt,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/live",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/live",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/archived",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/archived",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/admin",
			RepoDir:  "/home/h/gadgets/admin",
			Branch:   "task/admin",
			Readable: true,
		},
		&worktreev1.WatchedWorktree{
			Dir:      "/trees/orphan",
			RepoDir:  "/home/h/komrad",
			Branch:   "task/orphan",
			Readable: true,
		},
	}
}
