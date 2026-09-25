// Package watched lists every resource the watch carries.
package watched

import (
	"komrad/internal/watch"

	mailstore "komrad/internal/store/mail"
	markstore "komrad/internal/store/mark"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
)

// All lists every resource the watch carries the rows of.
func All() []watch.Resource {
	return []watch.Resource{
		rowstore.WatchedAgents,
		workflowstore.WatchedWorkflows,
		schedulestore.WatchedSchedules,
		markstore.WatchedMarks,
		mailstore.WatchedMail,
		pullrequeststore.WatchedPullRequests,
		pullrequeststore.WatchedRepositories,
		reviewstore.WatchedReviews,
		reviewstore.WatchedReviewThreads,
		worktreestore.WatchedWorktrees,
	}
}
