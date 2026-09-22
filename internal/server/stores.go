package server

import (
	"context"

	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	forgestore "weave/internal/store/forge"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	runstore "weave/internal/store/run"
	schedulestore "weave/internal/store/schedule"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"
	worktreestore "weave/internal/store/worktree"
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
	runs             *runstore.RunStore
	marks            *markstore.MarkStore
	forges           *forgestore.ForgeStore
	pullRequests     *pullrequeststore.PullRequestStore
	pullRequestSyncs *pullrequeststore.PullRequestSyncStore
	sizes            *worktreestore.WorktreeStore
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
		runs:             runstore.NewRunStore(db),
		marks:            markstore.NewMarkStore(db),
		forges:           forgestore.NewForgeStore(db),
		pullRequests:     pullrequeststore.NewPullRequestStore(db),
		pullRequestSyncs: pullrequeststore.NewPullRequestSyncStore(db),
		sizes:            worktreestore.NewWorktreeStore(db),
	}, nil
}
