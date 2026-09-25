package ui_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	gitMenuTitle   = "Git on api"
	pullRequestURL = "https://github.com/harrisoncramer/komrad/pull/740"
)

type recordingOpener struct {
	mu      sync.Mutex
	targets []string
	err     error
}

// TestGitMenu covers the menu of git actions drawn over the agent the cursor is on.
func TestGitMenu(t *testing.T) {
	t.Parallel()

	t.Run("the git key opens the menu on an agent and not on a workflow header", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildGitAgent()...)

		harness.press("k", "b")
		require.NotContains(t, harness.readFrame(), gitMenuTitle, "a workflow header has no branch of its own")

		harness.press("j", "b")
		assert.Contains(t, harness.readFrame(), gitMenuTitle)
	})

	t.Run("esc closes the menu without a word", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, gitMenuTitle)
		assert.NotContains(t, frame, "Could not")
	})

	t.Run("an entry answers its own letter", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("r")

		assert.NotContains(t, harness.readFrame(), gitMenuTitle)
		harness.assertSentTo(agentv1connect.AgentServicePlanRebaseProcedure, &agentv1.PlanRebaseRequest{AgentId: "a1"})
	})

	t.Run("the menu takes no filter, so the letters stay the entries'", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		require.NotContains(t, harness.readFrame(), "filter")

		harness.press("/")

		assert.Contains(t, harness.readFrame(), "Rebase onto the base (r)")
	})

	t.Run("an entry picked after its agent has left the list says so", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		harness.watch(buildSnapshot(buildGitAgent()[0]))

		harness.press("r")

		assert.Contains(t, readOpenMessages(harness), "the agent the menu was opened on has left the list")
		harness.assertSentTo(agentv1connect.AgentServicePlanRebaseProcedure)
	})
}

// TestGitMenuRefusals covers each entry naming why it would do nothing, and saying so when picked.
func TestGitMenuRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rows        []proto.Message
		press       string
		wantEntry   string
		wantRefusal string
	}{
		{
			name:        "the rebase of an agent with no worktree of its own",
			rows:        buildUncutAgent(),
			press:       "r",
			wantEntry:   "Rebase onto the base (r) · no worktree of its own",
			wantRefusal: "this agent has no worktree of its own, so there is no base to move it onto",
		},
		{
			name:        "the pull request of a repository not read yet",
			rows:        buildUncutAgent(),
			press:       "o",
			wantEntry:   "Open the pull request (o) · not read yet",
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name: "the pull request of a branch with none",
			rows: append(buildUncutAgent(), &pullrequestv1.Repository{
				Url:  repositoryURL,
				Read: true,
			}),
			press:       "o",
			wantEntry:   "Open the pull request (o) · none from this branch",
			wantRefusal: "komrad has read the repository and nothing has been opened from this branch",
		},
		{
			name:        "the pull request of a branch that has moved past its last one",
			rows:        append(buildUncutAgent(), newMergedPullRequest()),
			press:       "o",
			wantEntry:   "Open the pull request (o) · moved on since its last one",
			wantRefusal: "the branch has moved on since its last pull request finished",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, tt.rows...)

			frame := harness.readFrame()
			assert.Contains(t, frame, tt.wantEntry)
			assert.Contains(t, frame, "Cancel")

			harness.press(tt.press)

			assert.NotContains(t, harness.readFrame(), gitMenuTitle)
			assert.Contains(t, readOpenMessages(harness), tt.wantRefusal)
		})
	}
}

// TestGitMenuOpensThePullRequest covers the entry handing the browser the current pull request.
func TestGitMenuOpensThePullRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		refuse    error
		wantFrame string
	}{
		{
			name:      "a browser that refuses is reported",
			refuse:    errors.New("no application knows how to open it"),
			wantFrame: "Could not open",
		},
		{
			name: "the address is handed over",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opener := &recordingOpener{err: tt.refuse}
			harness := newPanelHarness(t, panelHarnessParams{browser: opener})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(append(buildGitAgent(), newOpenPullRequest())...))
			harness.press("b", "o")

			assert.Equal(t, []string{pullRequestURL}, opener.readTargets())
			frame := harness.readFrame()
			assert.NotContains(t, frame, gitMenuTitle)
			if tt.wantFrame == "" {
				assert.NotContains(t, frame, "Could not")

				return
			}
			assert.Contains(t, frame, tt.wantFrame)
		})
	}
}

func (r *recordingOpener) OpenURL(_ context.Context, target string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.targets = append(r.targets, target)

	return r.err
}

func (r *recordingOpener) readTargets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.targets
}

func openGitMenu(t *testing.T, rows ...proto.Message) *panelHarness {
	t.Helper()

	harness := openAgents(t, rows...)
	harness.press("b")
	require.Contains(t, harness.readFrame(), gitMenuTitle)

	return harness
}

func buildGitAgent() []proto.Message {
	agent := newBranchAgent("a1", "api", "agent/a1")
	agent.Cwd = "/trees/a1"
	agent.HeadSha = proto.String("new")

	return []proto.Message{
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		agent,
	}
}

func buildUncutAgent() []proto.Message {
	rows := buildGitAgent()
	rows[0] = newLiveWorkflow("w1", "backend")

	return rows
}

func newOpenPullRequest() *pullrequestv1.PullRequest {
	return &pullrequestv1.PullRequest{
		Id:                "pr-740",
		RepositoryUrl:     repositoryURL,
		HeadRepositoryUrl: repositoryURL,
		HeadBranch:        "agent/a1",
		Number:            740,
		Url:               pullRequestURL,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		OpenedAt:          "2026-09-19T12:00:00Z",
	}
}

func newMergedPullRequest() *pullrequestv1.PullRequest {
	merged := newOpenPullRequest()
	merged.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	merged.MergedAt = proto.String("2026-09-20T12:00:00Z")
	merged.HeadSha = "old"

	return merged
}

func readOpenMessages(harness *panelHarness) string {
	harness.press("ctrl+g")

	return joinProse(splitLines(harness.readFrame()))
}
