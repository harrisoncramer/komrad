package server

import (
	"context"
	"log/slog"
	"time"

	"komrad/internal/forge"
	"komrad/internal/jobs"
	"komrad/internal/logging"
	"komrad/internal/postgres"
	"komrad/internal/worktrees"
)

const (
	reconcileInterval    = 2 * time.Second
	mailDeliveryInterval = 2 * time.Second
	// transcriptSyncInterval is how often an agent's log is read for new turns.
	transcriptSyncInterval = 15 * time.Second
	cwdSyncInterval        = 15 * time.Second
	// cascadeInterval is how often the cascade looks for children whose parent moved.
	cascadeInterval = 15 * time.Second
	// pullRequestSyncInterval is how often the pull request sync asks which repositories are due.
	pullRequestSyncInterval = 5 * time.Second
	// commentSyncInterval is how often komrad asks the forges whether review comments moved.
	commentSyncInterval = time.Minute
	// worktreeSizeInterval is how often komrad walks a few worktree directories for their size.
	worktreeSizeInterval = 30 * time.Second
	// forgeRotationInterval is how often komrad checks rotating forge tokens for nearing expiry.
	forgeRotationInterval = time.Hour
	// forgeProxyInterval is how often an engine behind the forge proxy asks GitHub who its login belongs to.
	forgeProxyInterval = time.Minute
	// embeddedPostgresInterval is how often the server checks that the embedded Postgres accepts connections.
	embeddedPostgresInterval = 2 * time.Second
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
	ReconcileLoop        = "reconcile"
	mailDeliveryLoop     = "mail delivery"
	TranscriptSyncLoop   = "transcript sync"
	CwdSyncLoop          = "cwd sync"
	cascadeLoop          = "cascade"
	PullRequestSyncLoop  = "pull request sync"
	CommentSyncLoop      = "review comment sync"
	WorktreeSizeLoop     = "worktree sizes"
	forgeRotationLoop    = "forge token rotation"
	ForgeProxyLoop       = "forge proxy connection"
	embeddedPostgresLoop = "embedded postgres"
)

type loopWakers struct {
	cascade         *loopWaker
	cwdSync         *loopWaker
	pullRequestSync *loopWaker
	transcriptSync  *loopWaker
}

func newLoopWakers() *loopWakers {
	return &loopWakers{
		cascade:         newLoopWaker(),
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
	// EmbeddedPostgresDir is where the embedded Postgres lives, and empty when the config turns it off.
	EmbeddedPostgresDir string
	// CommitIdentityPath is where the engine writes who it commits as, and empty when it sends GitHub's hosts through no forge proxy.
	CommitIdentityPath string
}

func buildLoops(params buildLoopsParams) []*loop {
	loops := []*loop{
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
			Name:     cascadeLoop,
			Interval: cascadeInterval,
			Floor:    localLoopFloor,
			Run:      params.Services.checkouts.service.Cascade,
			Waker:    params.Wakers.cascade,
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

	if params.CommitIdentityPath != "" {
		loops = append(loops, newLoop(newLoopParams{
			Name:     ForgeProxyLoop,
			Interval: forgeProxyInterval,
			Floor:    pullRequestSyncFloor,
			Run:      connectThroughForgeProxy(params.Services.pullRequests.forge, params.CommitIdentityPath),
		}))
	}

	if params.EmbeddedPostgresDir != "" {
		loops = append(loops, newLoop(newLoopParams{
			Name:     embeddedPostgresLoop,
			Interval: embeddedPostgresInterval,
			Floor:    localLoopFloor,
			Run:      keepEmbeddedPostgresRunning(params.EmbeddedPostgresDir),
		}))
	}

	return loops
}

// connectThroughForgeProxy keeps github.com's connection on the account the forge proxy logs the engine in as, and the engine's commits by that account.
func connectThroughForgeProxy(service *forge.Service, identityPath string) func(context.Context) error {
	return func(ctx context.Context) error {
		viewer, err := service.ConnectThroughForgeProxy(ctx)
		if err != nil {
			return err
		}

		return forge.WriteCommitIdentity(identityPath, *viewer)
	}
}

// keepEmbeddedPostgresRunning starts the embedded Postgres in dir again whenever it has exited.
func keepEmbeddedPostgresRunning(dir string) func(context.Context) error {
	return func(ctx context.Context) error {
		started, err := postgres.EnsureRunning(ctx, dir)
		if err != nil {
			return err
		}

		if started {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the embedded Postgres had exited, so the server started it again",
				slog.String(logging.FilePath, dir))
		}

		return nil
	}
}
