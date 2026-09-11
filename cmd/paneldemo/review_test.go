package main

import (
	"net/http/httptest"
	"testing"

	"weave/cmd/paneldemo/scenario"
	"weave/internal/diff"
	"weave/internal/git"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// demoReview builds the scenario's repository and mounts the fake over it, which is what the
// review screen is pointed at.
func demoReview(t *testing.T) (scenario.Scenario, agentv1connect.AgentServiceClient) {
	t.Helper()

	repo, err := buildRepo(t.Context(), t.TempDir())
	require.NoError(t, err)

	held := scenario.Review().WithRepo(repo)

	server := httptest.NewServer(serviceFor(held))
	t.Cleanup(server.Close)

	return held, agentv1connect.NewAgentServiceClient(server.Client(), server.URL)
}

// TestTheReviewScenarioDrawsARealDiff is what the demo is for: the review screen runs git in the
// agent's directory, so a scenario that only held rows would draw nothing at all.
func TestTheReviewScenarioDrawsARealDiff(t *testing.T) {
	t.Parallel()

	held, agents := demoReview(t)

	opened, err := agents.OpenReviewRound(t.Context(), connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: held.Agents[0].GetId(),
	}))
	require.NoError(t, err)

	round := opened.Msg.GetRound()
	require.Equal(t, "round-3", round.GetId(), "the newest round is the one a review opens on")

	patch, err := diff.Load(t.Context(), git.NewClient(), diff.LoadParams{
		Dir:         held.Agents[0].GetCwd(),
		BaseSHA:     round.GetBaseSha(),
		SnapshotSHA: round.GetSnapshotSha(),
	})
	require.NoError(t, err)

	assert.Len(t, patch.Files, 3, "the fake change touches three files")
}

// TestEachRoundOfTheScenarioIsItsOwnDiff pins what the round keys walk, since two rounds of one
// agent standing on the same commit would draw the same patch and the walk would look broken.
func TestEachRoundOfTheScenarioIsItsOwnDiff(t *testing.T) {
	t.Parallel()

	held, _ := demoReview(t)

	seen := map[string]string{}

	for _, round := range held.Rounds {
		require.NotEmpty(t, round.GetSnapshotSha(), "round %s stands on a commit", round.GetId())

		key := round.GetAgentId() + "\x00" + round.GetSnapshotSha()
		assert.Empty(t, seen[key], "round %s stands on its own commit", round.GetId())
		seen[key] = round.GetId()

		patch, err := diff.Load(t.Context(), git.NewClient(), diff.LoadParams{
			Dir:         held.Agents[0].GetCwd(),
			BaseSHA:     round.GetBaseSha(),
			SnapshotSHA: round.GetSnapshotSha(),
		})
		require.NoError(t, err)
		assert.NotEmpty(t, patch.Files, "round %s draws something", round.GetId())
	}
}

// TestACommentTypedIntoTheDemoStays covers the demo being worth pressing keys in: the fake holds
// what the panel wrote, so the sidebar draws it on the next tick.
func TestACommentTypedIntoTheDemoStays(t *testing.T) {
	t.Parallel()

	held, agents := demoReview(t)
	agentID := held.Agents[0].GetId()

	before, err := agents.ListReviewThreads(t.Context(), connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &agentID,
	}))
	require.NoError(t, err)

	_, err = agents.CreateReviewThread(t.Context(), connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:  agentID,
		FilePath: "internal/ui/reviewband.go",
		NewStart: 5,
		NewEnd:   5,
		Body:     "this row is doing two things",
	}))
	require.NoError(t, err)

	after, err := agents.ListReviewThreads(t.Context(), connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &agentID,
	}))
	require.NoError(t, err)

	assert.Len(t, after.Msg.GetThreads(), len(before.Msg.GetThreads())+1)

	written := after.Msg.GetThreads()[len(after.Msg.GetThreads())-1]
	assert.Equal(t, "this row is doing two things", written.GetMessages()[0].GetBody())
	assert.Equal(t,
		agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
		written.GetMessages()[0].GetDelivery(),
		"a comment typed into the demo is held rather than sent")
}

// TestTheStageFlagLetsAScreenGiveUpTheWindow pins what -stage is for: the screens that put
// another process beside the sidebar can only be looked at when a split answers with a pane.
func TestTheStageFlagLetsAScreenGiveUpTheWindow(t *testing.T) {
	t.Parallel()

	refused := noPanes{}
	assert.Empty(t, refused.SplitRight(demoPane, "", ""), "by default every split is refused")
	assert.False(t, refused.IsPaneAlive(stagePane))

	pretending := noPanes{stage: true}
	assert.Equal(t, stagePane, pretending.SplitRight(demoPane, "", ""))
	assert.True(t, pretending.IsPaneAlive(stagePane), "and the pane it hands back stays alive")

	// The frame reads real behaviour off the stage and the info pane being different panes, which
	// tmux could never answer otherwise.
	assert.Equal(t, infoPane, pretending.SplitBelow(demoPane, "", ""))
	assert.NotEqual(t, pretending.SplitRight(demoPane, "", ""), pretending.SplitBelow(demoPane, "", ""))
	assert.True(t, pretending.IsPaneAlive(infoPane))
}
