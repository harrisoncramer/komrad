package ui_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	forgev1 "komrad/proto/gen/forge/v1"
	"komrad/proto/gen/forge/v1/forgev1connect"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	gitMenuEntry   = "rebase with base"
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

	t.Run("the menu opens it to its own right and names both in the trail", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		frame := harness.readFrame()
		assert.Equal(t, "agents › api › git", readTrail(frame))
		assert.Contains(t, frame, "┃ › git ›", "the menu it came from stays drawn with its cursor on git")
	})

	t.Run("esc goes back to the menu it came from, its cursor still on git", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("esc")

		frame := harness.readFrame()
		assert.NotContains(t, frame, gitMenuEntry)
		assert.Contains(t, frame, "› git ›")
		assert.Equal(t, "agents › api", readTrail(frame))

		harness.press("esc")

		frame = harness.readFrame()
		assert.NotContains(t, frame, "git ›")
		assert.NotContains(t, frame, "Could not")
	})

	t.Run("back does what esc does", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("k", "ctrl+s")

		frame := harness.readFrame()
		assert.NotContains(t, frame, gitMenuEntry)
		assert.Contains(t, frame, "› git ›")
	})

	t.Run("a narrow panel draws only the menu on top", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 36, height: 20})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(buildGitAgent()...))
		harness.press("space", "g")

		frame := harness.readFrame()
		assert.Contains(t, frame, gitMenuEntry)
		assert.NotContains(t, frame, "documents")
	})

	t.Run("an entry answers its own letter", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)

		harness.press("r")

		assert.NotContains(t, harness.readFrame(), gitMenuEntry)
		harness.assertSentTo(agentv1connect.AgentServicePlanRebaseProcedure, &agentv1.PlanRebaseRequest{AgentId: "a1"})
	})

	t.Run("the menu takes no filter, so the letters stay the entries'", func(t *testing.T) {
		t.Parallel()

		harness := openGitMenu(t, buildGitAgent()...)
		require.NotContains(t, harness.readFrame(), "filter")

		harness.press("/")

		assert.Contains(t, harness.readFrame(), "rebase with base")
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

// TestGitMenuRefusals covers an entry that would do nothing saying why when picked.
func TestGitMenuRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		rows        []proto.Message
		press       []string
		wantRefusal string
	}{
		{
			name:        "the rebase of an agent with no worktree of its own",
			rows:        buildUncutAgent(),
			press:       []string{"r"},
			wantRefusal: "this agent has no worktree of its own, so there is no base to move it onto",
		},
		{
			name:        "the pull request of a repository not read yet",
			rows:        buildUncutAgent(),
			press:       []string{"o"},
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name: "the pull request of a branch with none",
			rows: append(buildUncutAgent(), &pullrequestv1.Repository{
				Url:  repositoryURL,
				Read: true,
			}),
			press:       []string{"o"},
			wantRefusal: "komrad has read the repository and nothing has been opened from this branch",
		},
		{
			name:        "the pull request of a branch that has moved past its last one",
			rows:        append(buildUncutAgent(), newMergedPullRequest()),
			press:       []string{"o"},
			wantRefusal: "the branch has moved on since its last pull request finished",
		},
		{
			name:        "the merge of a branch with no pull request",
			rows:        buildUncutAgent(),
			press:       []string{"m", "m"},
			wantRefusal: "komrad has not read this agent's repository yet",
		},
		{
			name:        "the merge of a pull request that has already merged",
			rows:        append(buildGitAgent(), newCurrentMergedPullRequest()),
			press:       []string{"m", "m"},
			wantRefusal: "its pull request has already merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, tt.rows...)

			harness.press(tt.press...)

			assert.NotContains(t, harness.readFrame(), gitMenuEntry)
			assert.Contains(t, readOpenMessages(harness), tt.wantRefusal)
		})
	}
}

// TestGitMenuMerges covers the merge menu merging the current pull request and archiving the
// agent after.
func TestGitMenuMerges(t *testing.T) {
	t.Parallel()

	merge := &forgev1.MergeForgePullRequestRequest{Url: pullRequestURL}

	tests := []struct {
		name        string
		press       []string
		refuse      error
		wantMerges  []proto.Message
		wantArchive []proto.Message
		wantMessage string
	}{
		{
			name:        "merge leaves the agent where it is",
			press:       []string{"m"},
			wantMerges:  []proto.Message{merge},
			wantMessage: "api: merged #740",
		},
		{
			name:        "merge and archive archives the agent once the forge has merged",
			press:       []string{"a"},
			wantMerges:  []proto.Message{merge},
			wantArchive: []proto.Message{&agentv1.ArchiveAgentsRequest{Ids: []string{"a1"}}},
			wantMessage: "api: merged #740",
		},
		{
			name:  "back sends nothing",
			press: []string{"esc"},
		},
		{
			name:        "a merge the forge refuses archives nothing",
			press:       []string{"a"},
			refuse:      connect.NewError(connect.CodeFailedPrecondition, errors.New("github refused to merge it: Pull Request is not mergeable")),
			wantMerges:  []proto.Message{merge},
			wantMessage: "Pull Request is not mergeable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			harness := openGitMenu(t, append(buildGitAgent(), newOpenPullRequest())...)
			if tt.refuse != nil {
				harness.refuse(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.refuse)
			}

			harness.press("m")
			require.Equal(t, "agents › api › git › merge", readTrail(harness.readFrame()))
			harness.press(tt.press...)

			harness.assertSentTo(forgev1connect.ForgeServiceMergeForgePullRequestProcedure, tt.wantMerges...)
			harness.assertSentTo(agentv1connect.AgentServiceArchiveAgentsProcedure, tt.wantArchive...)
			assert.NotContains(t, harness.readFrame(), "merge and archive")
			if tt.wantMessage != "" {
				assert.Contains(t, readOpenMessages(harness), tt.wantMessage)
			}
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
			harness.press("space", "g", "o")

			assert.Equal(t, []string{pullRequestURL}, opener.readTargets())
			frame := harness.readFrame()
			assert.NotContains(t, frame, gitMenuEntry)
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
	harness.press("space", "g")
	require.Contains(t, harness.readFrame(), gitMenuEntry)

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

func newCurrentMergedPullRequest() *pullrequestv1.PullRequest {
	merged := newMergedPullRequest()
	merged.HeadSha = "new"

	return merged
}
