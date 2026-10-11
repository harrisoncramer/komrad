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

// countLocalReviewRounds is how many rounds of the task's work were reviewed.
func countLocalReviewRounds(reviews []*agentv1.WatchedReview) int32 {
	rounds := map[string]bool{}
	for _, review := range reviews {
		if review.GetReleasedAt() != "" {
			rounds[review.GetRoundId()] = true
		}
	}

	return int32(len(rounds))
}

// groupThreadsByTask indexes the review threads by the task whose work they are on.
func groupThreadsByTask(threads map[string]*agentv1.ReviewThread) map[string][]*agentv1.ReviewThread {
	byTask := map[string][]*agentv1.ReviewThread{}
	for _, thread := range threads {
		byTask[thread.GetTaskId()] = append(byTask[thread.GetTaskId()], thread)
	}

	return byTask
}

// commentCounts is how the comments on a task's work stand, reviewer agents apart from the person.
type commentCounts struct {
	reviewer *agentv1.CommentCounts
	person   *agentv1.CommentCounts
}

// countComments counts the comments on the task's work, reviewer agents then the person, leaving
// out a resolved conversation once its round has landed.
func countComments(reviews []*agentv1.WatchedReview, threads []*agentv1.ReviewThread) (*agentv1.CommentCounts, *agentv1.CommentCounts) {
	released := map[string]*agentv1.WatchedReview{}
	for _, review := range reviews {
		if review.GetReleasedAt() == "" {
			continue
		}
		released[review.GetId()] = review
	}

	var counted commentCounts
	for _, thread := range threads {
		review, ok := released[thread.GetReviewId()]
		if !ok {
			continue
		}

		isOpen := thread.GetResolvedAt() == ""
		if !isOpen && review.GetRoundLandedAt() != "" {
			continue
		}

		counts := counted.getOrAdd(review)
		if isOpen {
			counts.Open++
		} else {
			counts.Resolved++
		}
		if thread.GetUnreadByHuman() {
			counts.Unread++
		}
	}

	return counted.reviewer, counted.person
}

// getOrAdd returns the side a review's comments are counted on, adding it when it is absent.
func (c *commentCounts) getOrAdd(review *agentv1.WatchedReview) *agentv1.CommentCounts {
	if review.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		if c.reviewer == nil {
			c.reviewer = &agentv1.CommentCounts{}
		}

		return c.reviewer
	}

	if c.person == nil {
		c.person = &agentv1.CommentCounts{}
	}

	return c.person
}
