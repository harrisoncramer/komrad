package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/gittest"
	"komrad/internal/position"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func newOffer(id, login, body string, line int32) *pullrequestv1.ForgeThread {
	return &pullrequestv1.ForgeThread{
		Id:            id,
		PullRequestId: "pr-1",
		Path:          gittest.File,
		Line:          line,
		CommitSha:     gittest.SnapshotSHA,
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

// TestReviewOffers covers the forge's comments read in the review's popup and the key that walks
// them.
func TestReviewOffers(t *testing.T) {
	t.Run("an offer is read out only once the cursor is on its line", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 4))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.NotContains(t, readPopupProse(harness.readFrame()), "rename this")
	})

	t.Run("J moves to an offer's line and reads it out", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 4))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")

		frame := harness.readFrame()
		popup := readPopupProse(frame)
		assert.Contains(t, popup, "@dana on the forge")
		assert.Contains(t, popup, "rename this")
		assert.Contains(t, strings.Join(readPopup(frame), ""), "https://github.com/harrisoncramer/komrad/pull/7#discussion_f1")
		assert.Contains(t, readReviewCursor(frame), gittest.File+":4")
	})

	t.Run("J walks on to the next offer and round again", func(t *testing.T) {
		server := newOfferedReviewServer(t,
			newOffer("f1", "dana", "rename this", 4),
			newOffer("f2", "lee", "and this", 1),
		)
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")
		require.Contains(t, readPopupProse(harness.readFrame()), "and this")
		harness.press("J")
		require.Contains(t, readPopupProse(harness.readFrame()), "rename this")
		harness.press("J")
		assert.Contains(t, readPopupProse(harness.readFrame()), "and this")
	})

	t.Run("an offer's body reaches the terminal without its escape sequences", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "\x1b]8;;https://evil.example\x07click\x1b]8;;\x07 \x1b[5mhere\x1b[0m\x07", 4))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")

		raw := harness.readRawFrame()
		assert.NotContains(t, raw, "evil.example")
		assert.NotContains(t, raw, "\x1b[5m")
		assert.NotContains(t, raw, "\x07")
		assert.Contains(t, readPopupProse(harness.readFrame()), "click here")
	})

	t.Run("J takes an offer on a file with no line to the file", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "kim", "whole file", 0))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")

		frame := harness.readFrame()
		assert.Contains(t, readPopupProse(frame), "whole file")
		assert.NotContains(t, frame, "not in this diff")
		assert.Empty(t, readReviewCursor(frame), "the cursor rests on the file's header rather than a line")
	})

	t.Run("an offer is marked in the gutter of the line it is on", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 1))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "○")
	})

	t.Run("standing on an offer's line reads it out", func(t *testing.T) {
		server := newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 1))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		assert.Contains(t, readPopupProse(harness.readFrame()), "rename this")
	})

	t.Run("a resolved offer is marked settled", func(t *testing.T) {
		offer := newOffer("f1", "dana", "rename this", 1)
		offer.Resolved = true
		harness := openReviewAt(t, newOfferedReviewServer(t, offer), panelHarnessParams{height: 40})

		assert.Contains(t, findLine(readStage(harness.readFrame()), "package greet"), "✓")
	})

	t.Run("x dismisses the offer under the cursor", func(t *testing.T) {
		harness := openReviewAt(t, newOfferedReviewServer(t, newOffer("f1", "dana", "rename this", 1)), panelHarnessParams{height: 40})
		harness.answer(pullrequestv1connect.PullRequestServiceDismissForgeThreadProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&emptypb.Empty{}), nil
		})
		harness.press("?")
		require.Contains(t, readLegend(harness.readFrame()), "dismiss the forge comment")

		harness.press("x")

		sent := harness.readSent(pullrequestv1connect.PullRequestServiceDismissForgeThreadProcedure)
		require.Len(t, sent, 1)
		assert.True(t, proto.Equal(&pullrequestv1.DismissForgeThreadRequest{Id: "f1"}, sent[0]))
	})

	t.Run("x restores a dismissed offer under the cursor", func(t *testing.T) {
		dismissed := newOffer("f1", "dana", "rename this", 1)
		dismissed.Dismissed = true
		harness := openReviewAt(t, newOfferedReviewServer(t, dismissed), panelHarnessParams{height: 40})
		harness.answer(pullrequestv1connect.PullRequestServiceDismissForgeThreadProcedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
			return connect.NewResponse(&emptypb.Empty{}), nil
		})
		harness.press("?")
		require.Contains(t, readLegend(harness.readFrame()), "restore the forge comment")

		harness.press("x")

		sent := harness.readSent(pullrequestv1connect.PullRequestServiceDismissForgeThreadProcedure)
		require.Len(t, sent, 1)
		assert.True(t, proto.Equal(&pullrequestv1.DismissForgeThreadRequest{Id: "f1", Restore: true}, sent[0]))
	})

	t.Run("a dismissed offer is still drawn, marked skipped, and J passes it by", func(t *testing.T) {
		dismissed := newOffer("f1", "dana", "rename this", 1)
		dismissed.Dismissed = true
		server := newOfferedReviewServer(t, dismissed, newOffer("f2", "lee", "and this", 4))
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		frame := harness.readFrame()
		assert.Contains(t, findLine(readStage(frame), "package greet"), "")
		assert.Contains(t, readPopupProse(frame), "rename this")
		assert.Contains(t, strings.Join(readPopup(frame), ""), "dismissed")

		harness.press("J")
		require.Contains(t, readReviewCursor(harness.readFrame()), gittest.File+":4")
		harness.press("J")
		assert.Contains(t, readReviewCursor(harness.readFrame()), gittest.File+":4")
	})

	t.Run("an offer written on an earlier commit follows its line", func(t *testing.T) {
		offer := newOffer("f1", "dana", "rename this", 9)
		offer.CommitSha = "earlier"
		server := newOfferedReviewServer(t, offer)
		server.repo.Mappings = map[string][]position.Mapping{gittest.NamePair("earlier", gittest.SnapshotSHA): {{
			File:   position.FileMapping{OldPath: gittest.File, NewPath: gittest.File},
			Ranges: []position.RangeMapping{{Old: position.Range{Start: 1, End: 6}, New: position.Range{Start: 1, End: 1}}},
		}}}
		harness := openReviewAt(t, server, panelHarnessParams{height: 40})

		harness.press("J")

		assert.Contains(t, readReviewCursor(harness.readFrame()), gittest.File+":4")
	})

	t.Run("an offer on a commit the checkout lacks is placed by the line it quoted", func(t *testing.T) {
		offer := newOffer("f1", "dana", "rename this", 40)
		offer.CommitSha = "unknown"
		offer.Quote = "func Hello() string {"
		harness := openReviewAt(t, newOfferedReviewServer(t, offer), panelHarnessParams{height: 40})

		harness.press("J")

		frame := harness.readFrame()
		assert.Contains(t, findLine(readStage(frame), "func Hello() string {"), "○")
		assert.Contains(t, readPopupProse(frame), "rename this")
	})

	t.Run("an offer with nothing left to place it by is not in the diff", func(t *testing.T) {
		offer := newOffer("f1", "dana", "rename this", 40)
		offer.CommitSha = "unknown"
		offer.Path = "gone.go"
		harness := openReviewAt(t, newOfferedReviewServer(t, offer), panelHarnessParams{height: 40})

		harness.press("J")

		assert.Contains(t, harness.readFrame(), "gone.go:40 is not in this diff")
	})

	t.Run("a review with no offers leaves J to the comment under the cursor", func(t *testing.T) {
		harness := openReviewAt(t, newReviewServer(t), panelHarnessParams{height: 40})

		assert.NotContains(t, readLegend(harness.readFrame()), "next comment from the forge")
	})
}
