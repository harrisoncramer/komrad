package server

import (
	"context"
	"time"

	"komrad/config"
	"komrad/lib/xslice"

	"komrad/internal/rpc/render"
	"komrad/internal/watch"
	watchv1 "komrad/proto/gen/watch/v1"

	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	schedulestore "komrad/internal/store/schedule"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
)

// buildWatchLoaders pairs each watched resource's lister with its renderer.
func buildWatchLoaders(st *stores, loadConfig func() (*config.Config, error)) []watch.Loader {
	return []watch.Loader{
		watch.NewLoader(watchv1.Resource_RESOURCE_AGENT, st.rows, renderWatchedAgent),
		watch.NewLoader(watchv1.Resource_RESOURCE_WORKFLOW, st.workflows, renderWatchedWorkflow),
		watch.NewLoader(watchv1.Resource_RESOURCE_SCHEDULE, st.schedules, renderWatchedSchedule),
		watch.NewLoader(watchv1.Resource_RESOURCE_MAIL, st.mail, renderWatchedMail),
		watch.NewLoader(watchv1.Resource_RESOURCE_PULL_REQUEST, st.pullRequests, renderWatchedPullRequest),
		watch.NewLoader(watchv1.Resource_RESOURCE_REPOSITORY, st.pullRequestSyncs, renderWatchedRepository),
		watch.NewLoader(watchv1.Resource_RESOURCE_REVIEW, st.review, renderWatchedReview),
		watch.NewLoader(watchv1.Resource_RESOURCE_REVIEW_THREAD, st.reviewThreads, renderWatchedReviewThread),
		watch.NewLoader(watchv1.Resource_RESOURCE_WORKTREE, st.worktrees, renderWatchedWorktree),
		watch.NewLoader(watchv1.Resource_RESOURCE_FORGE_THREAD, forgeThreadLister{threads: st.forgeThreads, loadConfig: loadConfig}, renderWatchedForgeThread),
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

func renderWatchedReviewThread(row reviewstore.ThreadWithMessages) (string, *watchv1.Row) {
	thread := render.WatchedReviewThread(row)

	return thread.GetId(), &watchv1.Row{Row: &watchv1.Row_ReviewThread{ReviewThread: thread}}
}

func renderWatchedWorktree(row worktreestore.Worktree) (string, *watchv1.Row) {
	worktree := render.WatchedWorktree(row)

	return worktree.GetDir(), &watchv1.Row{Row: &watchv1.Row_Worktree{Worktree: worktree}}
}

type identifiedForgeThread struct {
	thread   pullrequeststore.ForgeThread
	identity config.Forge
}

type forgeThreadLister struct {
	threads    *pullrequeststore.ForgeThreadStore
	loadConfig func() (*config.Config, error)
}

func (l forgeThreadLister) ListWatched(ctx context.Context, keys []string) ([]identifiedForgeThread, error) {
	cfg, err := l.loadConfig()
	if err != nil {
		return nil, err
	}

	threads, err := l.threads.ListWatched(ctx, keys)
	if err != nil {
		return nil, err
	}

	return xslice.Map(threads, func(thread pullrequeststore.ForgeThread) identifiedForgeThread {
		return identifiedForgeThread{thread: thread, identity: cfg.Forge}
	}), nil
}

func renderWatchedForgeThread(row identifiedForgeThread) (string, *watchv1.Row) {
	thread := render.ForgeThread(row.thread, row.identity)

	return thread.GetId(), &watchv1.Row{Row: &watchv1.Row_ForgeThread{ForgeThread: thread}}
}
