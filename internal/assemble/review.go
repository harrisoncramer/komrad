package assemble

import (
	agentv1 "komrad/proto/gen/agent/v1"
)

// groupReviewsByTask indexes the reviews by the task whose work they read.
func groupReviewsByTask(reviews map[string]*agentv1.WatchedReview) map[string][]*agentv1.WatchedReview {
	byTask := map[string][]*agentv1.WatchedReview{}
	for _, review := range reviews {
		byTask[review.GetTaskId()] = append(byTask[review.GetTaskId()], review)
	}

	return byTask
}

// setLocalReview draws how many rounds of the task's work were reviewed.
func setLocalReview(row *agentv1.AgentRow, reviews []*agentv1.WatchedReview) {
	rounds := map[string]bool{}
	for _, review := range reviews {
		if review.GetReleasedAt() != "" {
			rounds[review.GetRoundId()] = true
		}
	}

	row.LocalReviewRounds = int32(len(rounds))
}

// groupThreadsByTask indexes the review threads by the task whose work they are on.
func groupThreadsByTask(threads map[string]*agentv1.ReviewThread) map[string][]*agentv1.ReviewThread {
	byTask := map[string][]*agentv1.ReviewThread{}
	for _, thread := range threads {
		byTask[thread.GetTaskId()] = append(byTask[thread.GetTaskId()], thread)
	}

	return byTask
}

// setCommentCounts counts the comments on the task's work, reviewer agents apart from the
// person, leaving out a resolved conversation once its round has landed.
func setCommentCounts(row *agentv1.AgentRow, reviews []*agentv1.WatchedReview, threads []*agentv1.ReviewThread) {
	released := map[string]*agentv1.WatchedReview{}
	for _, review := range reviews {
		if review.GetReleasedAt() == "" {
			continue
		}
		released[review.GetId()] = review
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

		counts := getOrAddCounts(row, review)
		if isOpen {
			counts.Open++
		} else {
			counts.Resolved++
		}
		if thread.GetUnreadByHuman() {
			counts.Unread++
		}
	}
}

// getOrAddCounts returns the side of the row a review's comments are counted on, adding it when it is absent.
func getOrAddCounts(row *agentv1.AgentRow, review *agentv1.WatchedReview) *agentv1.CommentCounts {
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
