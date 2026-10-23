package pullrequests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/google/uuid"
)

// commentReadsPerPass is the most pull requests one pass reads, so the comments never spend the
// rate limit the pull request sync shares with them.
const commentReadsPerPass = 5

// NewCommentSyncerParams is what a comment syncer is built over.
type NewCommentSyncerParams struct {
	Forges  *forge.Service
	Threads *pullrequeststore.ForgeThreadStore
	Tx      *store.Pool
	// Mirror copies a pull request's threads into the reviews of the agents on it once komrad stores them.
	Mirror forgeMirror
	Now    func() time.Time
}

// CommentSyncer keeps the forge's review threads in step on the open pull requests live agents are linked to.
type CommentSyncer struct {
	threads *pullrequeststore.ForgeThreadStore
	tx      *store.Pool
	mirror  forgeMirror
	hosts   *hostGate
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
		threads: params.Threads,
		tx:      params.Tx,
		mirror:  params.Mirror,
		hosts:   newHostGate(params.Forges, now),
		said:    onceLog{},
	}
}

type forgeMirror interface {
	Mirror(ctx context.Context, pullRequestID uuid.UUID) error
}

type commentHost struct {
	*forgeHost
	login string
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
			s.hosts.resetBackOff(host.forgeHost)
			s.hosts.forgetRefusal(host.forgeHost)
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
	opened, _ := s.hosts.open(ctx, hostname)
	if opened == nil {
		return nil
	}

	return &commentHost{
		forgeHost: opened,
		login:     viewerOf(*opened.connection),
	}
}

// viewerOf is the person a connection reads as, or empty for one made from an App, whose login is
// the App rather than anybody who comments.
func viewerOf(connection forgestore.Connection) string {
	if connection.Method == forgev1.CredentialMethod_CREDENTIAL_METHOD_GITHUB_APP {
		return ""
	}

	return connection.Login
}

func (s *CommentSyncer) readThreads(ctx context.Context, host *commentHost, remote git.RemoteURL, target pullrequeststore.CommentSyncTarget) error {
	answer, err := host.forge.ReviewThreads(ctx, remote, target.Number)
	if err != nil {
		return fmt.Errorf("failed to read the review threads of pull request %d: %w", target.Number, err)
	}
	if answer.Truncated && s.said.first(saidTruncated, target.ID.String()) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the pull request holds more review comments than one read returns, so none are removed until it holds fewer",
			slog.String(logging.RepositoryURL, remote.Canonical()), slog.Int(logging.PullRequestNumber, target.Number))
	}

	err = s.tx.InTx(ctx, func(db store.DBTX) error {
		return pullrequeststore.NewForgeThreadStore(db).SetThreads(ctx, pullrequeststore.SetThreadsParams{
			PullRequestID: target.ID,
			Viewer:        host.login,
			Threads:       answer.Threads,
			Prune:         !answer.Truncated,
		})
	})
	if err != nil {
		return err
	}
	if s.mirror == nil {
		return nil
	}

	return s.mirror.Mirror(ctx, target.ID)
}

func (s *CommentSyncer) settle(ctx context.Context, host *commentHost, remote git.RemoteURL, err error) {
	switch {
	case s.hosts.settle(ctx, host.forgeHost, err):
	case errors.Is(err, forge.ErrNotFound), errors.Is(err, errors.ErrUnsupported):
	default:
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to sync the review comments",
			append([]slog.Attr{slog.String(logging.RepositoryURL, remote.Canonical())}, logging.Err(err)...)...)
	}
}
