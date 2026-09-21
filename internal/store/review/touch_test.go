package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReviewWritesTouchTheReviewedAgent pins that a round, a review and its release each name
// the agent under review, whose row carries the tally and the verdict.
func TestReviewWritesTouchTheReviewedAgent(t *testing.T) {
	ctx := t.Context()
	st := open(t)
	agentID := seedAgent(t, st)
	st.touched.Reset()

	round := openRound(t, st, agentID, "one")
	assert.Equal(t, []string{agentID}, st.touched.Agents(), "a round names the agent")

	st.touched.Reset()
	review := openReview(t, st, agentID, round.ID)
	assert.Equal(t, []string{agentID}, st.touched.Agents(), "an opened review names the agent")

	st.touched.Reset()
	require.NoError(t, st.DeleteReview(ctx, review.ID))
	assert.Equal(t, []string{agentID}, st.touched.Agents(), "a deleted review names the agent it was on")
}
