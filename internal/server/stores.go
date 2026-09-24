package server

import (
	"context"

	"komrade/internal/store"
	agentstore "komrade/internal/store/agent"
	forgestore "komrade/internal/store/forge"
	mailstore "komrade/internal/store/mail"
	markstore "komrade/internal/store/mark"
	pullrequeststore "komrade/internal/store/pullrequest"
	reviewstore "komrade/internal/store/review"
	rowstore "komrade/internal/store/row"
	runstore "komrade/internal/store/run"
	schedulestore "komrade/internal/store/schedule"
	transcriptstore "komrade/internal/store/transcript"
	workflowstore "komrade/internal/store/workflow"
	worktreestore "komrade/internal/store/worktree"
)

type stores struct {
	pool             *store.Pool
	agents           *agentstore.AgentStore
	rows             *rowstore.RowStore
	workflows        *workflowstore.WorkflowStore
	schedules        *schedulestore.ScheduleStore
	transcripts      *transcriptstore.TranscriptStore
	mail             *mailstore.MailStore
	review           *reviewstore.ReviewStore
	reviewThreads    *reviewstore.ThreadStore
	runs             *runstore.RunStore
	marks            *markstore.MarkStore
	forges           *forgestore.ForgeStore
	pullRequests     *pullrequeststore.PullRequestStore
	pullRequestSyncs *pullrequeststore.PullRequestSyncStore
	worktrees        *worktreestore.WorktreeStore
}

func openStores(ctx context.Context, databaseURL string) (*stores, error) {
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	db := pool.GetDB()

	return &stores{
		pool:             pool,
		agents:           agentstore.NewAgentStore(db),
		rows:             rowstore.NewRowStore(db),
		workflows:        workflowstore.NewWorkflowStore(db),
		schedules:        schedulestore.NewScheduleStore(db),
		transcripts:      transcriptstore.NewTranscriptStore(db),
		mail:             mailstore.NewMailStore(db),
		review:           reviewstore.NewReviewStore(db),
		reviewThreads:    reviewstore.NewThreadStore(db),
		runs:             runstore.NewRunStore(db),
		marks:            markstore.NewMarkStore(db),
		forges:           forgestore.NewForgeStore(db),
		pullRequests:     pullrequeststore.NewPullRequestStore(db),
		pullRequestSyncs: pullrequeststore.NewPullRequestSyncStore(db),
		worktrees:        worktreestore.NewWorktreeStore(db),
	}, nil
}
