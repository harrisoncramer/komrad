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
	"komrad/lib/xslice"
)

// commentRereadAfter is how long a read of a pull request's threads is trusted while its mark holds still.
const commentRereadAfter = 10 * time.Minute

type transactor interface {
	InTx(ctx context.Context, fn func(store.DBTX) error) error
}

// NewCommentSyncerParams is what a comment syncer is built over.
type NewCommentSyncerParams struct {
	Forges  pullRequestForgeFinder
	Threads *pullrequeststore.ForgeThreadStore
	Tx      transactor
	// Now is the clock, time.Now when unset.
	Now func() time.Time
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
	forge        forge.Forge
	repositories []*commentRepository
	throttled    bool
}

type commentRepository struct {
	remote  git.RemoteURL
	targets []pullrequeststore.CommentSyncTarget
}

// Sync reads each pull request's comment mark and rereads the threads of the ones whose mark moved or whose last read has gone stale.
func (s *CommentSyncer) Sync(ctx context.Context) error {
	targets, err := s.threads.ListCommentSyncTargets(ctx)
	if err != nil {
		return err
	}

	read := 0
	for _, host := range s.groupByHost(ctx, targets) {
		read += s.syncHost(ctx, host)
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "review comment sync pass",
		slog.Int(logging.RowsListed, len(targets)),
		slog.Int(logging.ThreadsRead, read))

	return nil
}

func (s *CommentSyncer) groupByHost(ctx context.Context, targets []pullrequeststore.CommentSyncTarget) []*commentHost {
	hosts := []*commentHost{}
	byHost := map[string]*commentHost{}
	byRepository := map[string]*commentRepository{}
	skipped := map[string]bool{}
	for _, target := range targets {
		remote, err := git.ParseRemoteURL(target.RepositoryURL)
		if err != nil || remote.Host == "" || skipped[remote.Host] {
			continue
		}

		host, ok := byHost[remote.Host]
		if !ok {
			host = s.openHost(ctx, remote.Host)
			if host == nil {
				skipped[remote.Host] = true

				continue
			}

			byHost[remote.Host] = host
			hosts = append(hosts, host)
		}

		repository, ok := byRepository[remote.Canonical()]
		if !ok {
			repository = &commentRepository{remote: remote}
			byRepository[remote.Canonical()] = repository
			host.repositories = append(host.repositories, repository)
		}

		repository.targets = append(repository.targets, target)
	}

	return hosts
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

	return &commentHost{host: hostname, connectionID: connection.ID, forge: f}
}

func (s *CommentSyncer) syncHost(ctx context.Context, host *commentHost) int {
	read := 0
	for _, repository := range host.repositories {
		for start := 0; start < len(repository.targets) && !host.throttled; start += forge.MaxCommentMarks {
			batch := repository.targets[start:min(start+forge.MaxCommentMarks, len(repository.targets))]
			read += s.syncBatch(ctx, host, repository.remote, batch)
		}
	}
	if !host.throttled {
		delete(s.refusals, host.host)
		s.said.forget(saidRefused, host.host)
	}

	return read
}

func (s *CommentSyncer) syncBatch(ctx context.Context, host *commentHost, remote git.RemoteURL, batch []pullrequeststore.CommentSyncTarget) int {
	numbers := xslice.Map(batch, func(target pullrequeststore.CommentSyncTarget) int { return target.Number })

	marks, err := host.forge.CommentMarks(ctx, remote, numbers)
	if err != nil {
		s.settle(ctx, host, remote, err)

		return 0
	}

	read := 0
	for _, target := range batch {
		mark, ok := marks[target.Number]
		if !ok || !s.isDue(target, mark) {
			continue
		}

		target.Mark = mark
		if err := s.readThreads(ctx, host, remote, target); err != nil {
			s.settle(ctx, host, remote, err)
			if host.throttled {
				return read
			}

			continue
		}

		read++
	}

	return read
}

func (s *CommentSyncer) isDue(target pullrequeststore.CommentSyncTarget, mark string) bool {
	return mark != target.Mark || target.ReadAt == nil || s.now().Sub(*target.ReadAt) >= commentRereadAfter
}

func (s *CommentSyncer) readThreads(ctx context.Context, host *commentHost, remote git.RemoteURL, target pullrequeststore.CommentSyncTarget) error {
	threads, err := host.forge.ReviewThreads(ctx, remote, target.Number)
	if err != nil {
		return err
	}

	readAt := s.now()

	return s.tx.InTx(ctx, func(db store.DBTX) error {
		return pullrequeststore.NewForgeThreadStore(db).SetThreads(ctx, pullrequeststore.SetThreadsParams{
			PullRequestID: target.ID,
			Mark:          target.Mark,
			ReadAt:        readAt,
			Threads:       threads,
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
	case errors.Is(err, forge.ErrNotFound):
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
