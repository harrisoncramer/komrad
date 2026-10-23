package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	"komrad/proto/gen/agent/v1/agentv1connect"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestReviewPullRequest covers the pull request open from the branch, folded into the sidebar over the
// file tree.
func TestReviewPullRequest(t *testing.T) {
	t.Run("an agent with no pull request renders no block", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), paneltest.Params{Height: 40})

		assert.NotContains(t, paneltest.SidebarProse(harness.Frame()), "#7")
	})

	t.Run("a pull request whose title is not read yet renders no block", func(t *testing.T) {
		harness := openReviewAt(t, newPullRequestReviewServer(t), paneltest.Params{Height: 40})

		assert.NotContains(t, paneltest.SidebarProse(harness.Frame()), "#7")
	})

	t.Run("a branch moved past its last pull request renders no block", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		pullRequest := server.forge[0].(*pullrequestv1.PullRequest)
		pullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
		pullRequest.MergedAt = timestamppb.New(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
		pullRequest.HeadSha = "old"
		server.task.HeadSha = xptr.Ptr("new")
		harness := openReviewAt(t, server, paneltest.Params{Height: 40})

		assert.NotContains(t, paneltest.SidebarProse(harness.Frame()), "fix the greeting")
	})

	t.Run("a merge request is numbered the way GitLab does", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		server.forge[0].(*pullrequestv1.PullRequest).Forge = forgev1.Kind_KIND_GITLAB
		harness := openReviewAt(t, server, paneltest.Params{Height: 40})

		assert.Contains(t, paneltest.SidebarProse(harness.Frame()), "!7 fix the greeting")
	})

	t.Run("the sidebar stays inside every pane height", func(t *testing.T) {
		server := newDescribedReviewServer(t, strings.Repeat("A line of the description.\n", 40))
		harness := openReview(t, server)

		for height := 8; height <= 40; height++ {
			harness.Send(tea.WindowSizeMsg{Width: reviewWidth, Height: height})

			assert.Len(t, paneltest.SplitLines(harness.Frame()), height)
		}
	})

	t.Run("the description folds to its number and title over the files", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), paneltest.Params{Height: 40})

		sidebar := strings.Join(paneltest.Sidebar(harness.Frame()), "\n")
		require.Contains(t, sidebar, "#7 fix the greeting")
		assert.NotContains(t, sidebar, "Why it changed.")
		assert.Less(t, strings.Index(sidebar, "#7 fix the greeting"), strings.Index(sidebar, "greet.go"))
	})

	t.Run("the open key unfolds the author and the description in place", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed.\n\nMore context."), paneltest.Params{Height: 40})

		unfoldDescription(harness)

		sidebar := strings.Join(paneltest.Sidebar(harness.Frame()), "\n")
		assert.Contains(t, sidebar, "@dana")
		assert.Contains(t, sidebar, "Why it changed.")
		assert.Less(t, strings.Index(sidebar, "More context."), strings.Index(sidebar, "greet.go"))
	})

	t.Run("the open key on the title folds the description again", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), paneltest.Params{Height: 40})
		unfoldDescription(harness)

		harness.Press("o")

		assert.NotContains(t, paneltest.SidebarProse(harness.Frame()), "Why it changed.")
	})

	t.Run("the cursor walks an open description and the rows follow it", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), paneltest.Params{Height: 24})
		unfoldDescription(harness)
		require.NotContains(t, paneltest.SidebarProse(harness.Frame()), "line 30")

		harness.Press(slices.Repeat([]string{"j"}, 30)...)

		sidebar := paneltest.SidebarProse(harness.Frame())
		assert.Contains(t, sidebar, "line 30")
		assert.Contains(t, sidebar, "greet.go")
	})
}

// TestGitMenuOnTheReview covers the git menu the review's own menu opens in its popup.
func TestGitMenuOnTheReview(t *testing.T) {
	t.Run("the menu holds the pull request's entries and none that rewrite the worktree", func(t *testing.T) {
		harness := openReview(t, newPullRequestReviewServer(t))

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
		harness := openReview(t, newPullRequestReviewServer(t))
		planned := &agentv1.PlanPushResponse{Branch: "agent/auth", Commits: 2}
		harness.Answer(agentv1connect.AgentServicePlanPushProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(planned), nil
		})
		harness.Answer(agentv1connect.AgentServicePushWorktreeProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&agentv1.PushWorktreeResponse{Branch: "agent/auth", Commits: 2, Sha: "abcdef1234567"}), nil
		})

		harness.Press("space", "g", "p")

		require.Contains(t, paneltest.PopupProse(harness.Frame()), "Komrad pushes 2 commits")

		harness.Press("y")

		harness.AssertSentTo(agentv1connect.AgentServicePushWorktreeProcedure, &agentv1.PushWorktreeRequest{Task: &agentv1.PushWorktreeRequest_TaskId{TaskId: "t1"}})
		assert.Contains(t, harness.ReadMessages(), "auth: pushed agent/auth")
	})
}

func newPullRequestReviewServer(t *testing.T) *reviewServer {
	t.Helper()

	server := newReviewServer(t)
	server.task.RepositoryUrl = xptr.Ptr(repositoryURL)
	server.task.Branch = xptr.Ptr("agent/auth")
	server.forge = []proto.Message{&pullrequestv1.PullRequest{
		Id:                "pr-1",
		RepositoryUrl:     repositoryURL,
		HeadRepositoryUrl: repositoryURL,
		HeadBranch:        "agent/auth",
		Number:            7,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		OpenedAt:          timestamppb.New(time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)),
	}}

	return server
}

func newDescribedReviewServer(t *testing.T, body string) *reviewServer {
	t.Helper()

	server := newPullRequestReviewServer(t)
	pullRequest := server.forge[0].(*pullrequestv1.PullRequest)
	pullRequest.Title = "fix the greeting"
	pullRequest.Author = "dana"
	pullRequest.Body = body

	return server
}

// unfoldDescription walks the sidebar cursor up from the file tree onto the description and opens it.
func unfoldDescription(harness *paneltest.Panel) {
	harness.Press("ctrl+space", "ctrl+space", "k", "o")
}

func newNumberedBody(count int) string {
	var body strings.Builder
	for i := range count {
		fmt.Fprintf(&body, "line %02d\n", i+1)
	}

	return body.String()
}
