package pullrequests

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"
)

const saidTruncated = "truncated"

type transactor interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

// NewCommentSyncerParams is what a comment syncer is built over.
type NewCommentSyncerParams struct {
	Forges  pullRequestForgeFinder
	Threads *pullrequeststore.ForgeThreadStore
	Tx      transactor
	Now     func() time.Time
}

// CommentSyncer keeps the forge's review threads in step on the open pull requests live agents are linked to.
type CommentSyncer struct {
	forges   pullRequestForgeFinder
	threads  *pullrequeststore.ForgeThreadStore
	tx       transactor
	now      func() time.Time
	refusals map[string]int
	said     onceLog
}

// NewCommentSyncer builds a comment syncer over the forge service and the store.
func NewCommentSyncer(params NewCommentSyncerParams) *CommentSyncer {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &CommentSyncer{
		forges:   params.Forges,
		threads:  params.Threads,
		tx:       params.Tx,
		now:      now,
		refusals: map[string]int{},
		said:     onceLog{},
	}
}

type commentHost struct {
	host         string
	connectionID string
	login        string
	forge        forge.Forge
	throttled    bool
}

// Sync rereads the review threads of every open pull request a live agent is linked to.
func (s *CommentSyncer) Sync(ctx context.Context) error {
	targets, err := s.threads.ListCommentSyncTargets(ctx)
	if err != nil {
		return err
	}

	hosts := map[string]*commentHost{}
	read := 0
	for _, target := range targets {
		remote, err := git.ParseRemoteURL(target.RepositoryURL)
		if err != nil || remote.Host == "" {
			continue
		}

		host, ok := hosts[remote.Host]
		if !ok {
			host = s.openHost(ctx, remote.Host)
			hosts[remote.Host] = host
		}
		if host == nil || host.throttled {
			continue
		}

		if err := s.readThreads(ctx, host, remote, target); err != nil {
			s.settle(ctx, host, remote, err)

			continue
		}

		read++
	}

	for _, host := range hosts {
		if host != nil && !host.throttled {
			delete(s.refusals, host.host)
			s.said.forget(saidRefused, host.host)
		}
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "review comment sync pass",
		slog.Int(logging.RowsListed, len(targets)),
		slog.Int(logging.ThreadsRead, read))

	return nil
}

func (s *CommentSyncer) openHost(ctx context.Context, hostname string) *commentHost {
	connection, err := s.forges.ConnectionFor(ctx, hostname)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to find the forge connection",
			append([]slog.Attr{slog.String(logging.ForgeHost, hostname)}, logging.Err(err)...)...)

		return nil
	}
	if connection.ThrottledUntil != nil && connection.ThrottledUntil.After(s.now()) {
		return nil
	}

	f, err := s.forges.ForConnection(*connection)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to build the forge",
			append([]slog.Attr{slog.String(logging.ForgeHost, hostname)}, logging.Err(err)...)...)

		return nil
	}

	return &commentHost{host: hostname, connectionID: connection.ID, login: connection.Login, forge: f}
}

func (s *CommentSyncer) readThreads(ctx context.Context, host *commentHost, remote git.RemoteURL, target pullrequeststore.CommentSyncTarget) error {
	answer, err := host.forge.ReviewThreads(ctx, remote, target.Number)
	if err != nil {
		return err
	}
	if answer.Truncated && s.said.first(saidTruncated, target.ID) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the pull request holds more review comments than one read returns, so none are removed until it holds fewer",
			slog.String(logging.RepositoryURL, remote.Canonical()), slog.Int(logging.PullRequestNumber, target.Number))
	}

	return s.tx.InTx(ctx, func(db store.DBTX) error {
		return pullrequeststore.NewForgeThreadStore(db).SetThreads(ctx, pullrequeststore.SetThreadsParams{
			PullRequestID: target.ID,
			Viewer:        host.login,
			Threads:       answer.Threads,
			Prune:         !answer.Truncated,
		})
	})
}

func (s *CommentSyncer) settle(ctx context.Context, host *commentHost, remote git.RemoteURL, err error) {
	var throttled *forge.ThrottledError
	switch {
	case errors.As(err, &throttled):
		host.throttled = true
		s.recordThrottle(ctx, host, throttled.RetryAt)
	case errors.Is(err, forge.ErrUnauthorized):
		host.throttled = true
		s.recordThrottle(ctx, host, time.Time{})
		if s.said.first(saidRefused, host.host) {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge refused the connection's credential, so review comments wait for it to be accepted again",
				append([]slog.Attr{slog.String(logging.ForgeHost, host.host)}, logging.Err(err)...)...)
		}
	case errors.Is(err, forge.ErrNotFound), errors.Is(err, errors.ErrUnsupported):
	default:
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to sync the review comments",
			append([]slog.Attr{slog.String(logging.RepositoryURL, remote.Canonical())}, logging.Err(err)...)...)
	}
}

func (s *CommentSyncer) recordThrottle(ctx context.Context, host *commentHost, retryAt time.Time) {
	until := retryAt
	if until.IsZero() {
		wait := pullRequestThrottleFloor
		for range s.refusals[host.host] {
			wait = min(wait*2, pullRequestThrottleCeiling)
		}
		s.refusals[host.host]++
		until = s.now().Add(wait)
	}

	if err := s.forges.SetThrottledUntil(ctx, host.connectionID, &until); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record the forge's rate limit", logging.Err(err)...)
	}
}
