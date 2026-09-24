package server

import (
	"time"

	"weave/internal/jobs"
	"weave/internal/rpc/render"
	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"

	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
)

// buildWatchLoaders pairs each watched resource's lister with its renderer.
func buildWatchLoaders(st *stores, runs *jobs.RunLister) []watch.Loader {
	return []watch.Loader{
		watch.NewLoader(watchv1.Resource_RESOURCE_AGENT, st.rows, renderWatchedAgent),
		watch.NewLoader(watchv1.Resource_RESOURCE_WORKFLOW, st.workflows, renderWatchedWorkflow),
		watch.NewLoader(watchv1.Resource_RESOURCE_SCHEDULE, jobs.NewLatestRunReader(st.schedules, runs), renderWatchedSchedule),
		watch.NewLoader(watchv1.Resource_RESOURCE_MARK, st.marks, renderWatchedMark),
		watch.NewLoader(watchv1.Resource_RESOURCE_MAIL, st.mail, renderWatchedMail),
		watch.NewLoader(watchv1.Resource_RESOURCE_PULL_REQUEST, st.pullRequests, renderWatchedPullRequest),
		watch.NewLoader(watchv1.Resource_RESOURCE_REPOSITORY, st.pullRequestSyncs, renderWatchedRepository),
		watch.NewLoader(watchv1.Resource_RESOURCE_REVIEW, st.review, renderWatchedReview),
	}
}

func renderWatchedAgent(row rowstore.Row) (string, *watchv1.Row) {
	agent := render.WatchedAgent(row)

	return agent.GetId(), &watchv1.Row{Row: &watchv1.Row_Agent{Agent: agent}}
}

func renderWatchedWorkflow(row workflowstore.Workflow) (string, *watchv1.Row) {
	workflow := render.Workflow(row, render.WorkflowOptions{})

	return workflow.GetId(), &watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: workflow}}
}

func renderWatchedSchedule(row schedulestore.Schedule) (string, *watchv1.Row) {
	schedule := render.Schedule(row, time.Now(), render.ScheduleOptions{})

	return schedule.GetId(), &watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: schedule}}
}

func renderWatchedMark(row markstore.Mark) (string, *watchv1.Row) {
	mark := render.Mark(row)

	return mark.GetId(), &watchv1.Row{Row: &watchv1.Row_Mark{Mark: mark}}
}

func renderWatchedMail(row mailstore.Mail) (string, *watchv1.Row) {
	mail := render.Mail(row)

	return mail.GetId(), &watchv1.Row{Row: &watchv1.Row_Mail{Mail: mail}}
}

func renderWatchedPullRequest(row pullrequeststore.PullRequest) (string, *watchv1.Row) {
	pullRequest := render.PullRequest(row)

	return pullRequest.GetId(), &watchv1.Row{Row: &watchv1.Row_PullRequest{PullRequest: pullRequest}}
}

func renderWatchedRepository(row pullrequeststore.Sync) (string, *watchv1.Row) {
	repository := render.Repository(row)

	return repository.GetUrl(), &watchv1.Row{Row: &watchv1.Row_Repository{Repository: repository}}
}

func renderWatchedReview(row reviewstore.Review) (string, *watchv1.Row) {
	review := render.WatchedReview(row)

	return review.GetId(), &watchv1.Row{Row: &watchv1.Row_Review{Review: review}}
}
