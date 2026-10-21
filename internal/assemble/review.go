package assemble

import (
	agentv1 "komrad/proto/gen/agent/v1"
)

// commentCounts is how the comments on a task's work stand, reviewer agents apart from the person.
type commentCounts struct {
	reviewer *agentv1.CommentCounts
	person   *agentv1.CommentCounts
}

// groupReviewsByTask indexes the reviews by the task whose work they read.
func groupReviewsByTask(reviews map[string]*agentv1.WatchedReview) map[string][]*agentv1.WatchedReview {
	byTask := map[string][]*agentv1.WatchedReview{}
	for _, review := range reviews {
		byTask[review.GetTaskId()] = append(byTask[review.GetTaskId()], review)
	}

	return byTask
}

// countLocalReviewSnapshots is how many snapshots of the task's work were reviewed: those a
// released review reads and those the person commented on.
func countLocalReviewSnapshots(reviews []*agentv1.WatchedReview, threads []*agentv1.ReviewThread) int32 {
	snapshots := map[string]bool{}
	for _, review := range reviews {
		if review.GetReleasedAt() != "" {
			snapshots[review.GetSnapshotId()] = true
		}
	}
	for _, thread := range threads {
		if thread.ReviewId == nil {
			snapshots[thread.GetSnapshotId()] = true
		}
	}

	return int32(len(snapshots))
}

// groupThreadsByTask indexes the review threads by the task whose work they are on.
func groupThreadsByTask(threads map[string]*agentv1.ReviewThread) map[string][]*agentv1.ReviewThread {
	byTask := map[string][]*agentv1.ReviewThread{}
	for _, thread := range threads {
		byTask[thread.GetTaskId()] = append(byTask[thread.GetTaskId()], thread)
	}

	return byTask
}

// countComments counts the open, resolved and unread comments on the task's work, reviewer agents apart from the person.
func countComments(reviews []*agentv1.WatchedReview, threads []*agentv1.ReviewThread) commentCounts {
	released := map[string]*agentv1.WatchedReview{}
	for _, review := range reviews {
		if review.GetReleasedAt() != "" {
			released[review.GetId()] = review
		}
	}

	var counted commentCounts
	for _, thread := range threads {
		isOpen := thread.GetResolvedAt() == ""
		if thread.ReviewId != nil {
			review, ok := released[thread.GetReviewId()]
			if !ok || (!isOpen && review.GetSnapshotLandedAt() != "") {
				continue
			}
		}

		counts := counted.getOrAdd(thread.GetAuthorKind())
		if isOpen {
			counts.Open++
		} else {
			counts.Resolved++
		}
		if thread.GetUnreadByHuman() {
			counts.Unread++
		}
	}

	return counted
}

// getOrAdd returns the side an author's comments are counted on, adding it when it is absent.
func (c *commentCounts) getOrAdd(author agentv1.ReviewAuthorKind) *agentv1.CommentCounts {
	if author == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
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
