package assemble

import (
	"cmp"
	"strings"

	"komrad/lib/dates"

	agentv1 "komrad/proto/gen/agent/v1"
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

// groupThreadsByAgent indexes the review threads by the agent whose work they are on.
func groupThreadsByAgent(threads map[string]*agentv1.ReviewThread) map[string][]*agentv1.ReviewThread {
	byAgent := map[string][]*agentv1.ReviewThread{}
	for _, thread := range threads {
		byAgent[thread.GetAgentId()] = append(byAgent[thread.GetAgentId()], thread)
	}

	return byAgent
}

// setCommentCounts counts the comments on the agent's work, reviewer agents apart from the
// person, leaving out a resolved conversation once its round has landed.
func setCommentCounts(row *agentv1.AgentRow, reviews []*agentv1.WatchedReview, threads []*agentv1.ReviewThread) {
	released := map[string]*agentv1.WatchedReview{}
	for _, review := range reviews {
		if review.GetReleasedAt() == "" {
			continue
		}
		released[review.GetId()] = review

		if review.GetRoundLandedAt() == "" {
			countsFor(row, review)
		}
	}

	for _, thread := range threads {
		review, ok := released[thread.GetReviewId()]
		if !ok {
			continue
		}

		isOpen := thread.GetResolvedAt() == ""
		if !isOpen && review.GetRoundLandedAt() != "" {
			continue
		}

		counts := countsFor(row, review)
		if isOpen {
			counts.Open++
		} else {
			counts.Resolved++
		}
		counts.Unread = counts.GetUnread() || thread.GetUnreadByHuman()
	}
}

// countsFor is the side of the row a review's comments are counted on, made when it is absent.
func countsFor(row *agentv1.AgentRow, review *agentv1.WatchedReview) *agentv1.CommentCounts {
	if review.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		if row.ReviewerComments == nil {
			row.ReviewerComments = &agentv1.CommentCounts{}
		}

		return row.ReviewerComments
	}

	if row.PersonComments == nil {
		row.PersonComments = &agentv1.CommentCounts{}
	}

	return row.PersonComments
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
