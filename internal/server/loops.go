package server

import (
	"time"

	"weave/internal/jobs"
	"weave/internal/jobs/internaljobs"
)

const ReconcileInterval = 2 * time.Second

const ScheduleReconcileInterval = 10 * time.Second

const MailDeliveryInterval = 2 * time.Second

// TranscriptSyncInterval is how often an agent's log is read for new turns, which decides how
// soon a conversation is searchable.
const TranscriptSyncInterval = 15 * time.Second

const CwdSyncInterval = 15 * time.Second

// PullRequestSyncInterval is how often the forges are asked what changed, which costs a request
// per repository live agents sit on and is what makes a pull request appear without a nudge.
const PullRequestSyncInterval = time.Minute

// WorktreeSizeInterval is how often a few worktree directories are walked for their size.
const WorktreeSizeInterval = 30 * time.Second

const (
	ScheduleReconcileLoop = "schedule reconcile"
	ReconcileLoop         = "reconcile"
	MailDeliveryLoop      = "mail delivery"
	TranscriptSyncLoop    = "transcript sync"
	CwdSyncLoop           = "cwd sync"
	PullRequestSyncLoop   = "pull request sync"
	WorktreeSizeLoop      = "worktree sizes"
)

type buildLoopsParams struct {
	Stores   *stores
	Services *services
	Periodic *jobs.RiverPeriodic
}

func buildLoops(params buildLoopsParams) []Loop {
	return []Loop{
		{
			Name:     ScheduleReconcileLoop,
			Interval: ScheduleReconcileInterval,
			Pass:     internaljobs.NewScheduler(params.Stores.schedules, params.Periodic).Reconcile,
		},
		{
			Name:     ReconcileLoop,
			Interval: ReconcileInterval,
			Pass:     params.Services.reconciler.Reconcile,
		},
		{
			Name:     MailDeliveryLoop,
			Interval: MailDeliveryInterval,
			Pass:     params.Services.delivery.DeliverAll,
		},
		{
			Name:     TranscriptSyncLoop,
			Interval: TranscriptSyncInterval,
			Pass:     params.Services.transcriptSync.Sync,
		},
		{
			Name:     CwdSyncLoop,
			Interval: CwdSyncInterval,
			Pass:     params.Services.cwdSync.Sync,
		},
		{
			Name:     PullRequestSyncLoop,
			Interval: PullRequestSyncInterval,
			Pass:     params.Services.pullRequestSync.Sync,
		},
		{
			Name:     WorktreeSizeLoop,
			Interval: WorktreeSizeInterval,
			Pass:     params.Services.measurer.Measure,
		},
	}
}
