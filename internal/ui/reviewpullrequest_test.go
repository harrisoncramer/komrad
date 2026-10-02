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

	t.Run("a merge request is numbered the way GitLab does", func(t *testing.T) {
		server := newDescribedReviewServer(t, "Why it changed.")
		server.forge[0].(*pullrequestv1.PullRequest).Forge = "gitlab"
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.Contains(t, readSidebarProse(harness.readFrame()), "!7 fix the greeting")
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

	t.Run("a long description draws ten rows and says where they stand", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 40})

		sidebar := readSidebar(harness.readFrame())
		assert.Contains(t, sidebar, "line 09")
		assert.NotContains(t, sidebar, "line 10")
		assert.Contains(t, sidebar, "rows 1-9 of 40")
	})

	t.Run("a description that fits draws whole with no count of its rows", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(5)), panelHarnessParams{height: 40})

		sidebar := readSidebarProse(harness.readFrame())
		assert.Contains(t, sidebar, "line 05")
		assert.NotContains(t, sidebar, "rows ")
	})

	t.Run("the picker hands the keys to the pull request, which scrolls its description inside its ends", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 40})

		harness.press("pick:pullrequest", "k")
		assert.Contains(t, readSidebar(harness.readFrame()), "rows 1-9 of 40")

		harness.press("j", "j")
		assert.Contains(t, readSidebar(harness.readFrame()), "rows 3-11 of 40")

		harness.press("ctrl+d")
		assert.Contains(t, readSidebar(harness.readFrame()), "rows 8-16 of 40")

		harness.press(slices.Repeat([]string{"ctrl+d"}, 10)...)
		assert.Contains(t, readSidebar(harness.readFrame()), "rows 32-40 of 40")
		assert.Contains(t, readFilesBlock(harness.readFrame()), "▸ greet.go +1 −1")
	})

	t.Run("a taller pane pulls the description's end up and the next key scrolls from there", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 20})

		harness.press("pick:pullrequest")
		harness.press(slices.Repeat([]string{"ctrl+d"}, 20)...)
		require.NotContains(t, readSidebar(harness.readFrame()), "rows 32-40 of 40")

		harness.send(tea.WindowSizeMsg{Width: reviewWidth, Height: 40})
		harness.press("k")

		assert.Contains(t, readSidebar(harness.readFrame()), "rows 31-39 of 40")
	})

	t.Run("the back key hands the keys from the pull request to the diff", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 40})

		harness.press("pick:pullrequest", "esc", "j")

		assert.Contains(t, readSidebar(harness.readFrame()), "rows 1-9 of 40")
	})

	t.Run("the toggle steps from the diff to the files and never onto the pull request", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 40})

		harness.press("ctrl+space", "ctrl+space", "j")
		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame), "rows 1-9 of 40")
		assert.Contains(t, readFilesBlock(frame), "› greet.go +1 −1")

		harness.press("pick:pullrequest", "ctrl+space", "ctrl+space", "j")
		assert.Contains(t, readSidebar(harness.readFrame()), "rows 1-9 of 40")

		harness.press("ctrl+space", "ctrl+space", "j")
		assert.Contains(t, readSidebar(harness.readFrame()), "rows 1-9 of 40")
	})

	t.Run("a pane too narrow for the diff gives the picker no pull request", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{width: 120, height: 40})
		harness.send(tea.WindowSizeMsg{Width: 45, Height: 40})

		harness.press("ctrl+space", "j")

		assert.Empty(t, harness.panel.ReadPickLetter("pullrequest"))
	})

	t.Run("the menu leaves the pull request holding the keys", func(t *testing.T) {
		harness := openReviewAt(t, newDescribedReviewServer(t, newNumberedBody(40)), panelHarnessParams{height: 40})

		harness.press("pick:pullrequest", "space", "esc", "j")

		assert.Contains(t, readSidebar(harness.readFrame()), "rows 2-10 of 40")
	})
}

func newNumberedBody(count int) string {
	var body strings.Builder
	for i := range count {
		fmt.Fprintf(&body, "line %02d\n", i+1)
	}

	return body.String()
}
