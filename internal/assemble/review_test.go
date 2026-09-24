package assemble_test

import (
	"strconv"
	"testing"
	"time"

	"weave/internal/assemble"

	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentRowsLocalReview pins what the rows draw from the reviews in the world: released
// rounds are counted against the agent whose work went round, the last conclusion stands with
// the round it was given on, and a comment concludes nothing.
func TestAgentRowsLocalReview(t *testing.T) {
	t.Parallel()

	approved := agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED
	changes := agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED
	commented := agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED
	moved := agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED_MOVED

	tests := []struct {
		name    string
		agent   *agentv1.Agent
		reviews []*agentv1.WatchedReview
		rounds  int32
		verdict agentv1.ReviewVerdict
		tree    agentv1.ReviewTreeStanding
	}{
		{
			name:  "no review is nothing",
			agent: agent(newer),
		},
		{
			name:    "an open review is counted against nobody",
			agent:   agent(newer),
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, "", approved)},
		},
		{
			name:  "the last conclusion stands and a later comment leaves it",
			agent: agent(newer),
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-3*time.Hour), changes),
				review("v2", newer, 2, stamp(-2*time.Hour), approved),
				review("v3", newer, 3, stamp(-time.Hour), commented),
			},
			rounds:  3,
			verdict: approved,
		},
		{
			name:  "two reviews of one round count it once, the later release standing",
			agent: agent(newer),
			reviews: []*agentv1.WatchedReview{
				review("v1", newer, 1, stamp(-time.Hour), approved),
				review("v2", newer, 1, stamp(-2*time.Hour), changes),
			},
			rounds:  1,
			verdict: approved,
		},
		{
			name:    "a review of another agent's work is not this agent's",
			agent:   agent(newer),
			reviews: []*agentv1.WatchedReview{review("v1", older, 1, stamp(-time.Hour), approved)},
		},
		{
			name:    "the tree stands where it was measured against the concluded round",
			agent:   agent(newer, measuredAgainst("snapshot-2")),
			reviews: []*agentv1.WatchedReview{review("v1", newer, 2, stamp(-time.Hour), approved)},
			rounds:  1,
			verdict: approved,
			tree:    moved,
		},
		{
			name:    "a tree measured against another round is unmoved",
			agent:   agent(newer, measuredAgainst("snapshot-1")),
			reviews: []*agentv1.WatchedReview{review("v1", newer, 2, stamp(-time.Hour), approved)},
			rounds:  1,
			verdict: approved,
		},
		{
			name:    "a tree measured against a round nobody concluded on is unmoved",
			agent:   agent(newer, measuredAgainst("snapshot-1")),
			reviews: []*agentv1.WatchedReview{review("v1", newer, 1, stamp(-time.Hour), commented)},
			rounds:  1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			world := assemble.World{
				Agents:  map[string]*agentv1.Agent{tt.agent.GetId(): tt.agent},
				Reviews: map[string]*agentv1.WatchedReview{},
			}
			for _, review := range tt.reviews {
				world.Reviews[review.GetId()] = review
			}

			rows := assemble.AgentRows(world, now)
			require.Len(t, rows, 1)
			assert.Equal(t, tt.rounds, rows[0].GetLocalReviewRounds())
			assert.Equal(t, tt.verdict, rows[0].GetLocalReviewVerdict())
			assert.Equal(t, tt.tree, rows[0].GetLocalReviewTree())
		})
	}
}

// TestApplyReview pins that a review put and dropped by the watch is what the rows count.
func TestApplyReview(t *testing.T) {
	t.Parallel()

	var world assemble.World
	world.Apply(put(agentRow(agent(newer))))
	changed := world.Apply(put(&watchv1.Row{Row: &watchv1.Row_Review{Review: review("v1", newer, 1, stamp(-time.Hour), agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED)}}))
	assert.Equal(t, map[watchv1.Resource][]string{watchv1.Resource_RESOURCE_REVIEW: {"v1"}}, changed.Keys)
	assert.Equal(t, int32(1), assemble.AgentRows(world, now)[0].GetLocalReviewRounds())

	world.Apply(deleted(watchv1.Resource_RESOURCE_REVIEW, "v1"))
	assert.Empty(t, world.Reviews)
	assert.Zero(t, assemble.AgentRows(world, now)[0].GetLocalReviewRounds())
}

func review(id, agentID string, ordinal int32, releasedAt string, verdict agentv1.ReviewVerdict) *agentv1.WatchedReview {
	watched := &agentv1.WatchedReview{
		Id:               id,
		AgentId:          agentID,
		RoundId:          agentID + "-round-" + strconv.Itoa(int(ordinal)),
		RoundOrdinal:     ordinal,
		RoundSnapshotSha: "snapshot-" + strconv.Itoa(int(ordinal)),
		ReleasedAt:       releasedAt,
	}
	if releasedAt != "" {
		watched.Verdict = verdict
	}

	return watched
}

func measuredAgainst(sha string) func(*agentv1.Agent) {
	return func(a *agentv1.Agent) {
		a.TreeStanding = agentv1.ReviewTreeStanding_REVIEW_TREE_STANDING_LANDED_MOVED
		a.TreeStandingSha = sha
	}
}
