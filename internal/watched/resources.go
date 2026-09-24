// Package watched lists every resource the watch carries.
package watched

import (
	"komrade/internal/watch"

	mailstore "komrade/internal/store/mail"
	markstore "komrade/internal/store/mark"
	pullrequeststore "komrade/internal/store/pullrequest"
	reviewstore "komrade/internal/store/review"
	rowstore "komrade/internal/store/row"
	schedulestore "komrade/internal/store/schedule"
	workflowstore "komrade/internal/store/workflow"
	worktreestore "komrade/internal/store/worktree"
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
