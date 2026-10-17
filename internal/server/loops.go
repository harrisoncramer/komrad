package server

import (
	"time"

	"komrad/internal/jobs"
	"komrad/internal/worktrees"
)

const (
	reconcileInterval    = 2 * time.Second
	mailDeliveryInterval = 2 * time.Second
	// transcriptSyncInterval is how often an agent's log is read for new turns.
	transcriptSyncInterval = 15 * time.Second
	cwdSyncInterval        = 15 * time.Second
	// restackInterval is how often komrad looks for standing tasks whose parent moved.
	restackInterval = 15 * time.Second
	// pullRequestSyncInterval is how often the pull request sync asks which repositories are due.
	pullRequestSyncInterval = 5 * time.Second
	// commentSyncInterval is how often komrad asks the forges whether review comments moved.
	commentSyncInterval = time.Minute
	// worktreeSizeInterval is how often komrad walks a few worktree directories for their size.
	worktreeSizeInterval = 30 * time.Second
	// forgeRotationInterval is how often komrad checks rotating forge tokens for nearing expiry.
	forgeRotationInterval = time.Hour
)

// localLoopFloor is the least time between the starts of two runs of a loop that reads only this
// machine when a wake triggered the second.
const localLoopFloor = time.Second

// cwdSyncFloor is the least time between the starts of two cwd sync runs when the second was
// woken.
const cwdSyncFloor = 5 * time.Second

// pullRequestSyncFloor is the least time between the starts of two pull request sync runs when
// a wake triggered the second.
const pullRequestSyncFloor = 5 * time.Second

const (
	ReconcileLoop       = "reconcile"
	mailDeliveryLoop    = "mail delivery"
	TranscriptSyncLoop  = "transcript sync"
	CwdSyncLoop         = "cwd sync"
	restackLoop         = "restack"
	PullRequestSyncLoop = "pull request sync"
	CommentSyncLoop     = "review comment sync"
	WorktreeSizeLoop    = "worktree sizes"
	forgeRotationLoop   = "forge token rotation"
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
	Claimed  *jobs.WorktreeDelete
	Wakers   *loopWakers
}

func buildLoops(params buildLoopsParams) []*loop {
	return []*loop{
		newLoop(newLoopParams{
			Name:     ReconcileLoop,
			Interval: reconcileInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.agents.reconciler.Reconcile,
		}),
		newLoop(newLoopParams{
			Name:     mailDeliveryLoop,
			Interval: mailDeliveryInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.mail.delivery.DeliverAll,
		}),
		newLoop(newLoopParams{
			Name:     TranscriptSyncLoop,
			Interval: transcriptSyncInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.transcripts.sync.Sync,
			Waker:    params.Wakers.transcriptSync,
		}),
		newLoop(newLoopParams{
			Name:     CwdSyncLoop,
			Interval: cwdSyncInterval,
			Floor:    cwdSyncFloor,
			Run:      params.Services.checkouts.cwdSync.Sync,
			Waker:    params.Wakers.cwdSync,
		}),
		newLoop(newLoopParams{
			Name:     restackLoop,
			Interval: restackInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.checkouts.service.Restack,
		}),
		newLoop(newLoopParams{
			Name:     PullRequestSyncLoop,
			Interval: pullRequestSyncInterval,
			Floor:    pullRequestSyncFloor,
			Run:      params.Services.pullRequests.sync.Sync,
			Waker:    params.Wakers.pullRequestSync,
		}),
		newLoop(newLoopParams{
			Name:     CommentSyncLoop,
			Interval: commentSyncInterval,
			Floor:    pullRequestSyncFloor,
			Run:      params.Services.pullRequests.commentSync.Sync,
		}),
		newLoop(newLoopParams{
			Name:     WorktreeSizeLoop,
			Interval: worktreeSizeInterval,
			Floor:    localLoopFloor,
			Run: worktrees.NewMeasurer(worktrees.NewMeasurerParams{
				Lister:    params.Services.worktrees.lister,
				Claimed:   params.Claimed,
				Worktrees: params.Stores.worktrees,
				Standings: params.Services.worktrees.inspector,
			}).Measure,
		}),
		newLoop(newLoopParams{
			Name:     forgeRotationLoop,
			Interval: forgeRotationInterval,
			Floor:    pullRequestSyncFloor,
			Run:      params.Services.pullRequests.forge.RotateTokens,
		}),
	}
}
