package ui_test

import (
	"testing"

	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

func openOfferedAgents(t *testing.T, offers ...*pullrequestv1.ForgeThread) *panelHarness {
	t.Helper()

	rows := []proto.Message{
		newLiveWorkflow("w1", "api"),
		newBranchAgent("a1", "fix-ci", "agent/auth"),
		&pullrequestv1.PullRequest{
			Id:                "pr-1",
			RepositoryUrl:     repositoryURL,
			HeadRepositoryUrl: repositoryURL,
			HeadBranch:        "agent/auth",
			Number:            7,
			State:             pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
			OpenedAt:          "2026-09-19T12:00:00Z",
		},
	}
	for _, offer := range offers {
		rows = append(rows, offer)
	}

	return openAgents(t, rows...)
}

func fromBotAuthor(offer *pullrequestv1.ForgeThread) *pullrequestv1.ForgeThread {
	offer.Comments[0].Bot = true

	return offer
}

func fromTheReader(offer *pullrequestv1.ForgeThread) *pullrequestv1.ForgeThread {
	offer.Comments[0].Yours = true

	return offer
}

// TestOfferNotice covers the header saying when someone comments on the forge on an agent's pull request.
func TestOfferNotice(t *testing.T) {
	t.Run("comments already there when the panel opens say nothing", func(t *testing.T) {
		harness := openOfferedAgents(t, newOffer("f1", "dana", "rename this", 4))

		assert.NotContains(t, harness.readFrame(), "from @dana")
	})

	t.Run("new comments are counted by agent and author", func(t *testing.T) {
		harness := openOfferedAgents(t)

		harness.watch(buildPuts(newOffer("f1", "dana", "rename this", 4), newOffer("f2", "dana", "and this", 1)))

		assert.Contains(t, harness.readFrame(), "2 comments on fix-ci from @dana")
	})

	t.Run("a comment seen once is not said again", func(t *testing.T) {
		harness := openOfferedAgents(t)
		harness.watch(buildPuts(newOffer("f1", "dana", "rename this", 4)))
		harness.fireTimers()

		resolved := newOffer("f1", "dana", "rename this", 4)
		resolved.Resolved = true
		harness.watch(buildPuts(resolved))

		assert.NotContains(t, harness.readFrame(), "from @dana")
	})

	for name, offer := range map[string]*pullrequestv1.ForgeThread{
		"a bot":      fromBotAuthor(newOffer("f1", "ci", "lint", 4)),
		"the reader": fromTheReader(newOffer("f1", "harrisoncramer", "note to self", 4)),
	} {
		t.Run("a comment from "+name+" says nothing", func(t *testing.T) {
			harness := openOfferedAgents(t)

			harness.watch(buildPuts(offer))

			assert.NotContains(t, harness.readFrame(), "comment on fix-ci")
		})
	}
}
