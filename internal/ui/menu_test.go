package ui_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/gittest"
	"komrad/internal/panes"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	taskv1 "komrad/proto/gen/task/v1"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestMenu covers the menu space opens, the hovered row's entries over the screen's own.
func TestMenu(t *testing.T) {
	t.Parallel()

	t.Run("a screen with no hovered row opens its own band alone", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"))
		harness.Press("tab", "space")

		frame := harness.Frame()
		assert.Contains(t, frame, "› new schedule")
		assert.NotContains(t, frame, "logs")
	})

	t.Run("an agent's entries sit over the agents list's own, with no new task", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.MoveToRow("reviewer")
		harness.Press("space")

		frame := harness.Frame()
		assert.Contains(t, frame, "› logs")
		assert.Contains(t, frame, "edit files")
		assert.NotContains(t, frame, "new task")
		assert.Less(t, strings.Index(frame, "edit files"), strings.Index(frame, "new workflow"))
	})

	t.Run("a workflow's entries, new task among them, sit over the agents list's own", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, buildSubpagesAgents()...)
		harness.Press("g", "g", "space")

		frame := harness.Frame()
		require.Contains(t, frame, "new task")
		require.Contains(t, frame, "───")
		assert.Less(t, strings.Index(frame, "new task"), strings.Index(frame, "───"))
		assert.Less(t, strings.Index(frame, "edit workflow"), strings.Index(frame, "───"))
		assert.Less(t, strings.Index(frame, "───"), strings.Index(frame, "new workflow"))
	})

	t.Run("a screen with no band of its own opens nothing", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), paneltest.Reviewer("reviewer", "reviewer", "writer"))
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")
		before := harness.Frame()

		harness.Press("space")

		assert.Equal(t, before, harness.Frame())
	})

	t.Run("the legend offers it only where it opens", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name  string
			open  func(*testing.T) *paneltest.Panel
			wants bool
		}{
			{
				name: "off an agent",
				open: func(t *testing.T) *paneltest.Panel {
					harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "api"), newNamedSchedule("s1", "morning-ci"))
					harness.Press("tab")
					harness.Press(paneltest.WidenListPresses...)

					return harness
				},
				wants: true,
			},
			{
				name: "on the logs",
				open: func(t *testing.T) *paneltest.Panel {
					harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), paneltest.Reviewer("reviewer", "reviewer", "writer"))
					harness.MoveToRow("reviewer")
					harness.Press("space", "l")

					return harness
				},
				wants: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				harness := tt.open(t)
				harness.Press("?")

				if tt.wants {
					assert.Regexp(t, `space +menu`, paneltest.Legend(harness.Frame()))
				} else {
					assert.NotRegexp(t, `space +menu`, paneltest.Legend(harness.Frame()))
				}
			})
		}
	})

	t.Run("an entry that closes it hands the window back with the stage in one move", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{TriageRows: 3})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(buildTriage()...))
		harness.Press("t")
		harness.Advance(time.Second)
		harness.Press("space")
		sent := len(harness.Tmux().MoveBatches())

		harness.Press("z")

		batches := harness.Tmux().MoveBatches()[sent:]
		require.Len(t, batches, 1)
		assert.Contains(t, batches[0], panes.KindZoom)
		assert.Contains(t, batches[0], panes.KindRespawn)
	})
}

// TestGitMenuOnTheReview covers the git menu the review's own menu opens in its popup.
func TestGitMenuOnTheReview(t *testing.T) {
	t.Parallel()

	t.Run("the menu holds the pull request's entries and none that rewrite the worktree", func(t *testing.T) {
		t.Parallel()

		harness := openPullRequestReview(t)

		harness.Press("space", "g")

		popup := paneltest.PopupProse(harness.Frame())
		assert.Contains(t, popup, "push")
		assert.Contains(t, popup, "toggle draft on")
		assert.Contains(t, popup, "re-request review")
		assert.Contains(t, popup, "merge pr")
		assert.NotContains(t, popup, "rebase")
		assert.NotContains(t, popup, "pick fork point")
		assert.NotContains(t, popup, "abort")
	})

	t.Run("a push asks in the popup, then pushes", func(t *testing.T) {
		t.Parallel()

		harness := openPullRequestReview(t)
		harness.Answer(agentv1connect.AgentServicePlanPushProcedure, paneltest.AnswerWith(&agentv1.PlanPushResponse{Branch: "agent/auth", Commits: 2}))
		harness.Answer(agentv1connect.AgentServicePushWorktreeProcedure, paneltest.AnswerWith(&agentv1.PushWorktreeResponse{
			Branch:  "agent/auth",
			Commits: 2,
			Sha:     "abcdef1234567",
		}))

		harness.Press("space", "g", "p")

		require.Contains(t, paneltest.PopupProse(harness.Frame()), "Komrad pushes 2 commits")

		harness.Press("y")

		harness.AssertSentTo(agentv1connect.AgentServicePushWorktreeProcedure, &agentv1.PushWorktreeRequest{Task: &agentv1.PushWorktreeRequest_TaskId{TaskId: "t1"}})
		assert.Contains(t, harness.ReadMessages(), "auth: pushed agent/auth")
	})
}

// openPullRequestReview opens the review of a task on a branch with an open pull request.
func openPullRequestReview(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness, reviewed := startReviewed(t, paneltest.Params{})
	reviewed.task.RepositoryUrl = xptr.Ptr(repositoryURL)
	reviewed.task.Branch = xptr.Ptr("agent/auth")
	harness.Watch(reviewed.snapshot(&pullrequestv1.PullRequest{
		Id:                "pr-1",
		RepositoryUrl:     repositoryURL,
		HeadRepositoryUrl: repositoryURL,
		HeadBranch:        "agent/auth",
		Number:            7,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		OpenedAt:          timestamppb.New(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)),
	}))
	harness.Press("space", "r")

	return harness
}

// reviewed is a workflow that cuts worktrees, its writer alive in pane %9 and the writer's task,
// kept in a repository the panel can read.
type reviewed struct {
	workflow *workflowv1.Workflow
	agent    *agentv1.Agent
	task     *taskv1.Task
}

// startReviewed starts the panel over a repository of its own and answers the reads that open its
// review.
func startReviewed(t *testing.T, params paneltest.Params) (*paneltest.Panel, *reviewed) {
	t.Helper()

	repo := gittest.NewRepo(t.TempDir())
	params.Git = repo
	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	harness.Answer(agentv1connect.AgentServiceOpenReviewHistoryProcedure, paneltest.AnswerWith(&agentv1.OpenReviewHistoryResponse{
		Snapshot: &agentv1.ReviewSnapshot{
			Id:      "snapshot-1",
			TaskId:  "t1",
			Ordinal: 1,
			BaseSha: gittest.BaseSHA,
			Sha:     gittest.SnapshotSHA,
		},
		Resolved: &agentv1.ReviewResolvedSpan{BaseSha: gittest.BaseSHA, HeadSha: gittest.SnapshotSHA},
	}))
	harness.Answer(agentv1connect.AgentServiceListReviewViewedFilesProcedure, paneltest.AnswerWith(&agentv1.ListReviewViewedFilesResponse{}))

	return harness, &reviewed{
		workflow: &workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
		},
		agent: &agentv1.Agent{
			Id:         "a1",
			Name:       "auth",
			WorkflowId: "w1",
			Role:       agentv1.AgentRole_AGENT_ROLE_WRITER,
			TaskId:     xptr.Ptr("t1"),
			Cwd:        repo.Root,
			PaneId:     "%9",
			Alive:      xptr.Ptr(true),
			State:      agentv1.AgentState_AGENT_STATE_IDLE,
		},
		task: &taskv1.Task{
			Id:         "t1",
			WorkflowId: "w1",
			Slug:       "auth",
			Cwd:        xptr.Ptr(repo.Root),
		},
	}
}

// snapshot is the reviewed rows beside any others given.
func (r *reviewed) snapshot(rows ...proto.Message) *watchv1.WatchEvent {
	return paneltest.Snapshot(append([]proto.Message{r.workflow, r.agent, r.task}, rows...)...)
}

// buildTriage is a workflow with three agents waiting on a person and one that is not, broken the
// longest but in a tier below the others.
func buildTriage() []proto.Message {
	return []proto.Message{
		paneltest.Workflow("w1", "backend"),
		paneltest.AgentAt("quiet", "w1", 1),
		newTriageAgent("asker", agentv1.AgentState_AGENT_STATE_WAITING, 3),
		newTriageAgent("broken", agentv1.AgentState_AGENT_STATE_ERRORED, 2),
		newTriageAgent("third", agentv1.AgentState_AGENT_STATE_WAITING, 4),
	}
}

func newTriageAgent(name string, state agentv1.AgentState, minute int) *agentv1.Agent {
	agent := paneltest.AgentAt(name, "w1", minute)
	agent.State = state
	agent.Alive = xptr.Ptr(true)
	agent.StateChangedAt = timestamppb.New(time.Date(2026, 9, 20, 13, minute, 0, 0, time.UTC))

	return agent
}

const repositoryURL = "https://github.com/harrisoncramer/komrad"
