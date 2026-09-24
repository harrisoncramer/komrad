package assemble

import (
	"cmp"
	"strings"

	"komrade/lib/dates"

	agentv1 "komrade/proto/gen/agent/v1"
)

// groupReviewsByAgent indexes the reviews by the agent whose work they read.
func groupReviewsByAgent(reviews map[string]*agentv1.WatchedReview) map[string][]*agentv1.WatchedReview {
	byAgent := map[string][]*agentv1.WatchedReview{}
	for _, review := range reviews {
		byAgent[review.GetAgentId()] = append(byAgent[review.GetAgentId()], review)
	}

	return byAgent
}

// setLocalReview draws how many rounds of the agent's work were reviewed, the last conclusion
// released about it, and where the worktree stands against the round that conclusion was on.
func setLocalReview(row *agentv1.AgentRow, reviews []*agentv1.WatchedReview) {
	rounds := map[string]bool{}
	var concluded *agentv1.WatchedReview
	for _, review := range reviews {
		if review.GetReleasedAt() == "" {
			continue
		}
		rounds[review.GetRoundId()] = true

		if isConclusion(review) && (concluded == nil || compareConclusions(review, concluded) > 0) {
			concluded = review
		}
	}

	row.LocalReviewRounds = int32(len(rounds))
	row.LocalReviewVerdict = concluded.GetVerdict()
	if row.GetAgent().GetTreeStandingSha() == concluded.GetRoundSnapshotSha() {
		row.LocalReviewTree = row.GetAgent().GetTreeStanding()
	}
}

// isConclusion is whether a review said the work is done or is not.
func isConclusion(review *agentv1.WatchedReview) bool {
	switch review.GetVerdict() {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED, agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return true
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return false
}

// compareConclusions orders two conclusions by the round they were given on, then by when each was released.
func compareConclusions(a, b *agentv1.WatchedReview) int {
	return cmp.Or(
		cmp.Compare(a.GetRoundOrdinal(), b.GetRoundOrdinal()),
		dates.Parse(a.GetReleasedAt()).Compare(dates.Parse(b.GetReleasedAt())),
		strings.Compare(a.GetId(), b.GetId()),
	)
}
