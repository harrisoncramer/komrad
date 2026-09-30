package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/gittest"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func newOffer(id, login, body string, line int32) *pullrequestv1.ForgeThread {
	return &pullrequestv1.ForgeThread{
		Id:            id,
		PullRequestId: "pr-1",
		Path:          gittest.File,
		Line:          line,
		Url:           "https://github.com/harrisoncramer/komrad/pull/7#discussion_" + id,
		WrittenAt:     "2026-09-19T12:00:00Z",
		Comments:      []*pullrequestv1.ForgeComment{{Id: id + "-c", AuthorLogin: login, Body: body}},
	}
}

func newOfferedReviewServer(t *testing.T, offers ...*pullrequestv1.ForgeThread) *reviewServer {
	t.Helper()

	server := newReviewServer(t)
	server.agent.RepositoryUrl = xptr.Ptr(repositoryURL)
	server.agent.Branch = xptr.Ptr("agent/auth")
	server.forge = []proto.Message{&pullrequestv1.PullRequest{
		Id:                "pr-1",
		RepositoryUrl:     repositoryURL,
		HeadRepositoryUrl: repositoryURL,
		HeadBranch:        "agent/auth",
		Number:            7,
		State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		OpenedAt:          "2026-09-19T12:00:00Z",
	}}
	for _, offer := range offers {
		server.forge = append(server.forge, offer)
	}

	return server
}

// TestReviewOffers covers the forge's comments in the review's sidebar and the key that walks them.
func TestReviewOffers(t *testing.T) {
	t.Run("the sidebar names each offer by its author and line", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 4))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		sidebar := strings.Join(readSidebar(harness.readFrame()), "\n")
		assert.Contains(t, sidebar, "From the forge  1 open, 0 resolved")
		assert.Contains(t, sidebar, "@dana  "+gittest.File+":4")
		assert.NotContains(t, sidebar, "rename this", "an offer is read out only once chosen")
	})

	t.Run("J moves to an offer's line and reads it out", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 4))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")

		frame := harness.readFrame()
		sidebar := strings.Join(readSidebar(frame), "\n")
		assert.Contains(t, sidebar, "rename this")
		assert.Contains(t, strings.ReplaceAll(sidebar, "\n    ", ""), "https://github.com/harrisoncramer/komrad/pull/7#discussion_f1")
		assert.Contains(t, readReviewCursor(frame), gittest.File+":4")
	})

	t.Run("J walks on to the next offer and round again", func(t *testing.T) {
		server := newOfferedReviewServer(t,
			newOffer("f1", "dana", "rename this", 4),
			newOffer("f2", "lee", "and this", 1),
		)
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")
		require.Contains(t, strings.Join(readSidebar(harness.readFrame()), "\n"), "and this")
		harness.press("J")
		require.Contains(t, strings.Join(readSidebar(harness.readFrame()), "\n"), "rename this")
		harness.press("J")
		assert.Contains(t, strings.Join(readSidebar(harness.readFrame()), "\n"), "and this")
	})

	t.Run("a review with no offers leaves J to the comment under the cursor", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readLegend(harness.readFrame()), "next comment from the forge")
		assert.NotContains(t, strings.Join(readSidebar(harness.readFrame()), "\n"), "From the forge")
	})
}
