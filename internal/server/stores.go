package server

import (
	"context"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	documentstore "komrad/internal/store/document"
	forgestore "komrad/internal/store/forge"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	reviewstore "komrad/internal/store/review"
	rowstore "komrad/internal/store/row"
	runstore "komrad/internal/store/run"
	schedulestore "komrad/internal/store/schedule"
	transcriptstore "komrad/internal/store/transcript"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
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
	documents        *documentstore.DocumentStore
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
		documents:        documentstore.NewDocumentStore(db),
		forges:           forgestore.NewForgeStore(db),
		pullRequests:     pullrequeststore.NewPullRequestStore(db),
		pullRequestSyncs: pullrequeststore.NewPullRequestSyncStore(db),
		worktrees:        worktreestore.NewWorktreeStore(db),
	}, nil
}
