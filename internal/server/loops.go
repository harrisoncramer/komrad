package server

import (
	"time"

	"komrad/internal/jobs"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/worktrees"
)

const (
	reconcileInterval         = 2 * time.Second
	scheduleReconcileInterval = 10 * time.Second
	mailDeliveryInterval      = 2 * time.Second
	// transcriptSyncInterval is how often an agent's log is read for new turns.
	transcriptSyncInterval = 15 * time.Second
	cwdSyncInterval        = 15 * time.Second
	// pullRequestSyncInterval is how often the forges are asked what changed.
	pullRequestSyncInterval = time.Minute
	// commentSyncInterval is how often the forges are asked whether review comments moved.
	commentSyncInterval = time.Minute
	// worktreeSizeInterval is how often a few worktree directories are walked for their size.
	worktreeSizeInterval = 30 * time.Second
)

// localLoopFloor is the least time between the starts of two runs of a loop that reads only this
// machine when the second was woken.
const localLoopFloor = time.Second

// cwdSyncFloor is the least time between the starts of two cwd sync runs when the second was
// woken.
const cwdSyncFloor = 5 * time.Second

// pullRequestSyncFloor is the least time between the starts of two pull request sync runs when
// the second was woken.
const pullRequestSyncFloor = 10 * time.Second

const (
	scheduleReconcileLoop = "schedule reconcile"
	ReconcileLoop         = "reconcile"
	mailDeliveryLoop      = "mail delivery"
	TranscriptSyncLoop    = "transcript sync"
	CwdSyncLoop           = "cwd sync"
	PullRequestSyncLoop   = "pull request sync"
	CommentSyncLoop       = "review comment sync"
	WorktreeSizeLoop      = "worktree sizes"
)

type loopWakers struct {
	cwdSync         *loopWaker
	pullRequestSync *loopWaker
	transcriptSync  *loopWaker
}

func newLoopWakers() *loopWakers {
	return &loopWakers{
		cwdSync:         newLoopWaker(),
		pullRequestSync: newLoopWaker(),
		transcriptSync:  newLoopWaker(),
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
		newLoop(newLoopParams{
			Name:     scheduleReconcileLoop,
			Interval: scheduleReconcileInterval,
			Floor:    localLoopFloor,
			Run:      internaljobs.NewScheduler(params.Stores.schedules, params.Periodic).Reconcile,
		}),
		newLoop(newLoopParams{
			Name:     ReconcileLoop,
			Interval: reconcileInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.reconciler.Reconcile,
		}),
		newLoop(newLoopParams{
			Name:     mailDeliveryLoop,
			Interval: mailDeliveryInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.delivery.DeliverAll,
		}),
		newLoop(newLoopParams{
			Name:     TranscriptSyncLoop,
			Interval: transcriptSyncInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.transcriptSync.Sync,
			Waker:    params.Wakers.transcriptSync,
		}),
		newLoop(newLoopParams{
			Name:     CwdSyncLoop,
			Interval: cwdSyncInterval,
			Floor:    cwdSyncFloor,
			Run:      params.Services.cwdSync.Sync,
			Waker:    params.Wakers.cwdSync,
		}),
		newLoop(newLoopParams{
			Name:     PullRequestSyncLoop,
			Interval: pullRequestSyncInterval,
			Floor:    pullRequestSyncFloor,
			Run:      params.Services.pullRequestSync.Sync,
			Waker:    params.Wakers.pullRequestSync,
		}),
		newLoop(newLoopParams{
			Name:     CommentSyncLoop,
			Interval: commentSyncInterval,
			Floor:    pullRequestSyncFloor,
			Run:      params.Services.commentSync.Sync,
		}),
		newLoop(newLoopParams{
			Name:     WorktreeSizeLoop,
			Interval: worktreeSizeInterval,
			Floor:    localLoopFloor,
			Run: worktrees.NewMeasurer(worktrees.NewMeasurerParams{
				Lister:    params.Services.worktreeLister,
				Claimed:   params.Claimed,
				Worktrees: params.Stores.worktrees,
			}).Measure,
		}),
	}
}
