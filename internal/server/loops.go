package server

import (
	"time"

	"komrad/internal/jobs"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/worktrees"
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

// localLoopFloor is the least time between the starts of two runs of a loop that reads only this
// machine when the second was woken.
const localLoopFloor = time.Second

// cwdSyncFloor is the least time between the starts of two cwd sync runs when the second was
// woken.
const cwdSyncFloor = 5 * time.Second

// pullRequestSyncFloor is the least time between the starts of two pull request sync runs when
// the second was woken.
const pullRequestSyncFloor = 15 * time.Second

const (
	ScheduleReconcileLoop = "schedule reconcile"
	ReconcileLoop         = "reconcile"
	MailDeliveryLoop      = "mail delivery"
	TranscriptSyncLoop    = "transcript sync"
	CwdSyncLoop           = "cwd sync"
	PullRequestSyncLoop   = "pull request sync"
	WorktreeSizeLoop      = "worktree sizes"
)

type loopWakers struct {
	cwdSync         *LoopWaker
	pullRequestSync *LoopWaker
	transcriptSync  *LoopWaker
}

func newLoopWakers() *loopWakers {
	return &loopWakers{
		cwdSync:         NewLoopWaker(),
		pullRequestSync: NewLoopWaker(),
		transcriptSync:  NewLoopWaker(),
	}
}

type buildLoopsParams struct {
	Stores   *stores
	Services *services
	Periodic *jobs.RiverPeriodic
	Claimed  *jobs.WorktreeDelete
	Wakers   *loopWakers
}

func buildLoops(params buildLoopsParams) []*Loop {
	return []*Loop{
		NewLoop(NewLoopParams{
			Name:     ScheduleReconcileLoop,
			Interval: ScheduleReconcileInterval,
			Floor:    localLoopFloor,
			Run:      internaljobs.NewScheduler(params.Stores.schedules, params.Periodic).Reconcile,
		}),
		NewLoop(NewLoopParams{
			Name:     ReconcileLoop,
			Interval: ReconcileInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.reconciler.Reconcile,
		}),
		NewLoop(NewLoopParams{
			Name:     MailDeliveryLoop,
			Interval: MailDeliveryInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.delivery.DeliverAll,
		}),
		NewLoop(NewLoopParams{
			Name:     TranscriptSyncLoop,
			Interval: TranscriptSyncInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.transcriptSync.Sync,
			Waker:    params.Wakers.transcriptSync,
		}),
		NewLoop(NewLoopParams{
			Name:     CwdSyncLoop,
			Interval: CwdSyncInterval,
			Floor:    cwdSyncFloor,
			Run:      params.Services.cwdSync.Sync,
			Waker:    params.Wakers.cwdSync,
		}),
		NewLoop(NewLoopParams{
			Name:     PullRequestSyncLoop,
			Interval: PullRequestSyncInterval,
			Floor:    pullRequestSyncFloor,
			Run:      params.Services.pullRequestSync.Sync,
			Waker:    params.Wakers.pullRequestSync,
		}),
		NewLoop(NewLoopParams{
			Name:     WorktreeSizeLoop,
			Interval: WorktreeSizeInterval,
			Floor:    localLoopFloor,
			Run: worktrees.NewMeasurer(worktrees.NewMeasurerParams{
				Lister:    params.Services.worktreeLister,
				Claimed:   params.Claimed,
				Worktrees: params.Stores.worktrees,
			}).Measure,
		}),
	}
}
