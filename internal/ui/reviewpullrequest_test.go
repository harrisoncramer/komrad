package ui_test

import (
	"strings"
	"testing"

	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDescribedReviewServer(t *testing.T, body string) *reviewServer {
	t.Helper()

	server := newOfferedReviewServer(t)
	pullRequest := server.forge[0].(*pullrequestv1.PullRequest)
	pullRequest.Title = "fix the greeting"
	pullRequest.Author = "dana"
	pullRequest.Body = body

	return server
}

// TestReviewPullRequest covers the pull request open from the branch drawn over the sidebar's
// file tree.
func TestReviewPullRequest(t *testing.T) {
	t.Run("an agent with no pull request draws no block", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "#7")
	})

	t.Run("a pull request whose title is not read yet draws no block", func(t *testing.T) {
		harness := openReviewAt(t, newOfferedReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "#7")
	})

	t.Run("a branch moved past its last pull request draws no block", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		pullRequest := server.forge[0].(*pullrequestv1.PullRequest)
		pullRequest.State = pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
		pullRequest.MergedAt = xptr.Ptr("2026-09-20T12:00:00Z")
		pullRequest.HeadSha = "old"
		server.agent.HeadSha = xptr.Ptr("new")
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.NotContains(t, readSidebarProse(harness.readFrame()), "fix the greeting")
	})

	t.Run("the number, title, author and description stand over the files", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.\n\n\n\nMore context.")
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		sidebar := strings.Join(readSidebar(harness.readFrame()), "\n")
		require.Contains(t, sidebar, "#7 fix the greeting")
		assert.Contains(t, sidebar, "@dana")
		assert.Contains(t, sidebar, "Why it changed.\n\nMore context.")
		assert.Less(t, strings.Index(sidebar, "More context."), strings.Index(sidebar, "greet.go"))
	})

	t.Run("a short pane cuts the description and keeps the files", func(t *testing.T) {
		server := newDescribedReviewServer(t, strings.Repeat("A line of the description.\n", 40))
		harness := openReviewAt(t, server, panelHarnessParams{height: 24})

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "#7 fix the greeting")
		assert.Contains(t, sidebar, "…")
		assert.Contains(t, sidebar, "greet.go")
	})

	t.Run("the sidebar stays inside every pane height", func(t *testing.T) {
		server := newDescribedReviewServer(t, strings.Repeat("A line of the description.\n", 40))
		harness := openReview(t, server)

		for height := 8; height <= 40; height++ {
			harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: height})

			assert.Len(t, splitLines(harness.readFrame()), height)
		}
	})
}
