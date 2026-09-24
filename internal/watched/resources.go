// Package watched lists every resource the watch carries.
package watched

import (
	"weave/internal/watch"

	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
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
	}
}
