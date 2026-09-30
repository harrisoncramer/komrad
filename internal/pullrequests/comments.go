package pullrequests

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
)

const saidTruncated = "truncated"

// commentReadsPerPass is the most pull requests one pass reads, so the comments never spend the
// rate limit the pull request sync shares with them.
const commentReadsPerPass = 5

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
	forges  pullRequestForgeFinder
	threads *pullrequeststore.ForgeThreadStore
	tx      transactor
	now     func() time.Time
	backoff *hostBackoff
	said    onceLog
	next    int
}

// NewCommentSyncer builds a comment syncer over the forge service and the store.
func NewCommentSyncer(params NewCommentSyncerParams) *CommentSyncer {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &CommentSyncer{
		forges:  params.Forges,
		threads: params.Threads,
		tx:      params.Tx,
		now:     now,
		backoff: newHostBackoff(params.Forges, now),
		said:    onceLog{},
	}
}

type commentHost struct {
	host         string
	connectionID string
	login        string
	forge        forge.Forge
	throttled    bool
}

// Sync rereads the review threads of the next few open pull requests live agents are linked to.
func (s *CommentSyncer) Sync(ctx context.Context) error {
	targets, err := s.threads.ListCommentSyncTargets(ctx)
	if err != nil {
		return err
	}
	targets = s.takeTurn(targets)

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
			s.backoff.reset(host.host)
			s.said.forget(saidRefused, host.host)
		}
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "review comment sync pass",
		slog.Int(logging.RowsListed, len(targets)),
		slog.Int(logging.ThreadsRead, read))

	return nil
}

// takeTurn is the next few targets after the ones the last pass read, wrapping round, so every
// pull request is read in turn however many there are.
func (s *CommentSyncer) takeTurn(targets []pullrequeststore.CommentSyncTarget) []pullrequeststore.CommentSyncTarget {
	if len(targets) <= commentReadsPerPass {
		s.next = 0

		return targets
	}

	start := s.next % len(targets)
	turn := slices.Concat(targets[start:], targets[:start])[:commentReadsPerPass]
	s.next = start + commentReadsPerPass

	return turn
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

	return &commentHost{host: hostname, connectionID: connection.ID, login: viewerOf(*connection), forge: f}
}

// viewerOf is the person a connection reads as, or empty for one made from an App, whose login is
// the App rather than anybody who comments.
func viewerOf(connection forgestore.Connection) string {
	if connection.Method == forge.MethodGitHubApp {
		return ""
	}

	return connection.Login
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
		s.backoff.record(ctx, host.host, host.connectionID, throttled.RetryAt)
	case errors.Is(err, forge.ErrUnauthorized):
		host.throttled = true
		s.backoff.record(ctx, host.host, host.connectionID, time.Time{})
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
