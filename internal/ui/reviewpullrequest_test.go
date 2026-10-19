package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestReviewPullRequest covers the pull request open from the branch, folded into the sidebar over the
// file tree.
func TestReviewPullRequest(t *testing.T) {
	t.Run("an agent with no pull request renders no block", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "#7")
	})

	t.Run("a pull request whose title is not read yet renders no block", func(t *testing.T) {
		harness := openReviewAt(t, newPullRequestReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "#7")
	})

	t.Run("a branch moved past its last pull request renders no block", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		pullRequest := server.forge[0].(*pullrequestv1.PullRequest)
		pullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
		pullRequest.MergedAt = xptr.Ptr("2026-09-20T12:00:00Z")
		pullRequest.HeadSha = "old"
		server.task.HeadSha = xptr.Ptr("new")
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "fix the greeting")
	})

	t.Run("a merge request is numbered the way GitLab does", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		server.forge[0].(*pullrequestv1.PullRequest).Forge = "gitlab"
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.Contains(t, readSidebarProse(harness.readFrame()), "!7 fix the greeting")
	})

	t.Run("the sidebar stays inside every pane height", func(t *testing.T) {
		server := newDescribedReviewServer(t, strings.Repeat("A line of the description.\n", 40))
		harness := openReview(t, server)

		for height := 8; height <= 40; height++ {
			harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: height})

			assert.Len(t, splitLines(harness.readFrame()), height)
		}
	})

	t.Run("the description folds to its number and title over the files", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), panelHarnessParams{height: 40})

		sidebar := strings.Join(readSidebar(harness.readFrame()), "\n")
		require.Contains(t, sidebar, "#7 fix the greeting")
		assert.NotContains(t, sidebar, "Why it changed.")
		assert.Less(t, strings.Index(sidebar, "#7 fix the greeting"), strings.Index(sidebar, "greet.go"))
	})

	t.Run("the open key unfolds the author and the description in place", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed.\n\nMore context."), panelHarnessParams{height: 40})

		unfoldDescription(harness)

		sidebar := strings.Join(readSidebar(harness.readFrame()), "\n")
		assert.Contains(t, sidebar, "@dana")
		assert.Contains(t, sidebar, "Why it changed.")
		assert.Less(t, strings.Index(sidebar, "More context."), strings.Index(sidebar, "greet.go"))
	})

	t.Run("the open key on the title folds the description again", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, "Why it changed."), panelHarnessParams{height: 40})
		unfoldDescription(harness)

		harness.press("o")

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "Why it changed.")
	})

	t.Run("the cursor walks an open description and the rows follow it", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 24})
		unfoldDescription(harness)
		require.NotContains(t, readSidebarProse(harness.readFrame()), "line 30")

		harness.press(slices.Repeat([]string{"j"}, 30)...)

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "line 30")
		assert.Contains(t, sidebar, "greet.go")
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
		OpenedAt:          "2026-09-19T12:00:00Z",
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
func unfoldDescription(harness *panelHarness) {
	harness.t.Helper()

	harness.press("ctrl+space", "ctrl+space", "k", "o")
}

func newNumberedBody(count int) string {
	var body strings.Builder
	for i := range count {
		fmt.Fprintf(&body, "line %02d\n", i+1)
	}

	return body.String()
}
