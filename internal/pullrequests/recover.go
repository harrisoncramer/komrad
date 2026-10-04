package pullrequests

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/logging"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// unreachableHostWait is how long a read of a superseded head's checks waits on a host it cannot reach.
const unreachableHostWait = 5 * time.Minute

// RecoverOldChecks reads the checks on a head the pull request no longer points at, reading them again a step later
// while any is in flight and marking those still in flight unknown once the steps run out.
func (s *PullRequestSyncer) RecoverOldChecks(ctx context.Context, args internaljobs.RecoverOldChecksArgs) (internaljobs.RecoverOutcome, error) {
	pullRequest, err := s.pullRequests.GetByID(ctx, args.PullRequestID)
	if errors.Is(err, store.ErrNotFound) {
		return internaljobs.RecoverOutcome{}, nil
	}
	if err != nil {
		return internaljobs.RecoverOutcome{}, err
	}
	if pullRequest.HeadSHA == args.HeadSHA {
		return internaljobs.RecoverOutcome{}, nil
	}
	if s.now().After(args.SupersededAt.Add(internaljobs.RecoverOldChecksCap)) {
		return internaljobs.RecoverOutcome{}, s.markUnknown(ctx, args)
	}

	remote, err := git.ParseRemoteURL(pullRequest.RepositoryURL)
	if err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the pull request's repository is not a remote komrad can read, so its old checks are left as they are",
			append([]slog.Attr{slog.String(logging.RepositoryURL, pullRequest.RepositoryURL)}, logging.Err(err)...)...)

		return internaljobs.RecoverOutcome{}, nil
	}

	reached := s.hosts.reach(ctx, remote.Host)
	switch {
	case !reached.throttledUntil.IsZero():
		return internaljobs.RecoverOutcome{Snooze: reached.throttledUntil.Sub(s.now())}, nil
	case reached.host == nil:
		return internaljobs.RecoverOutcome{Snooze: unreachableHostWait}, nil
	}

	reading, err := reached.host.forge.ReadCommitChecks(ctx, forge.ReadCommitChecksParams{
		Repo:       remote,
		ProviderID: pullRequest.ProviderID,
		SHA:        args.HeadSHA,
	})
	var throttled *forge.ThrottledError
	switch {
	case err == nil:
	case errors.As(err, &throttled):
		until := s.hosts.holdUntil(ctx, reached.host, throttled.RetryAt)

		return internaljobs.RecoverOutcome{Snooze: until.Sub(s.now())}, nil
	case errors.Is(err, forge.ErrUnauthorized), errors.Is(err, forge.ErrUnsupported):
		return internaljobs.RecoverOutcome{}, nil
	case errors.Is(err, forge.ErrNotFound):
		return internaljobs.RecoverOutcome{}, s.markUnknown(ctx, args)
	default:
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "failed to read the checks on a superseded head, so the next step reads them again",
			append(recoverAttrs(pullRequest, args), logging.Err(err)...)...)

		return internaljobs.RecoverOutcome{}, s.nextStep(ctx, args, nil)
	}

	if reading.RateLimit.IsExhausted() {
		s.hosts.holdUntil(ctx, reached.host, reading.RateLimit.ResetAt)
	}

	listing := commitListing(reading, args.HeadSHA)
	if listing == nil {
		return internaljobs.RecoverOutcome{}, nil
	}

	return internaljobs.RecoverOutcome{}, s.nextStep(ctx, args, listing)
}

// nextStep writes the checks the step read, when it read any, and queues the next step while any is in flight,
// settling those in flight as unknown when no step is left.
func (s *PullRequestSyncer) nextStep(ctx context.Context, args internaljobs.RecoverOldChecksArgs, listing *forge.StatusCheckListing) error {
	readAt := s.now()

	return s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		checks := pullrequeststore.NewStatusCheckStore(tx)
		if listing != nil {
			if err := checks.SetStatusChecks(ctx, pullrequeststore.SetStatusChecksParams{
				PullRequestID: args.PullRequestID,
				HeadSHA:       args.HeadSHA,
				StatusChecks:  listing.StatusChecks,
				Truncated:     listing.Truncated,
				ReadAt:        readAt,
			}); err != nil {
				return nil, err
			}
			if !forge.AnyInFlight(listing.StatusChecks) {
				return nil, nil
			}
		}

		if args.Step+1 >= len(internaljobs.RecoverOldChecksDelays) {
			return nil, checks.MarkUnknown(ctx, pullrequeststore.MarkUnknownParams{
				PullRequestID: args.PullRequestID,
				HeadSHA:       args.HeadSHA,
				ReadAt:        readAt,
			})
		}

		return []river.JobArgs{internaljobs.RecoverOldChecksArgs{
			PullRequestID: args.PullRequestID,
			HeadSHA:       args.HeadSHA,
			Step:          args.Step + 1,
			SupersededAt:  args.SupersededAt,
		}}, nil
	})
}

// markUnknown moves every check still in flight on the head to unknown.
func (s *PullRequestSyncer) markUnknown(ctx context.Context, args internaljobs.RecoverOldChecksArgs) error {
	readAt := s.now()

	return s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		return nil, pullrequeststore.NewStatusCheckStore(tx).MarkUnknown(ctx, pullrequeststore.MarkUnknownParams{
			PullRequestID: args.PullRequestID,
			HeadSHA:       args.HeadSHA,
			ReadAt:        readAt,
		})
	})
}

// commitListing is the checks the reading lists on the head, nil when the forge listed none for it.
func commitListing(reading *forge.Reading, headSHA string) *forge.StatusCheckListing {
	for _, pullRequest := range reading.PullRequests {
		if pullRequest.HeadSHA == headSHA && pullRequest.MergeReadiness != nil {
			return pullRequest.MergeReadiness.Listing
		}
	}

	return nil
}

func recoverAttrs(pullRequest *pullrequeststore.PullRequest, args internaljobs.RecoverOldChecksArgs) []slog.Attr {
	return []slog.Attr{
		slog.String(logging.ForgeHost, pullRequest.Host),
		slog.String(logging.RepositoryURL, pullRequest.RepositoryURL),
		slog.String(logging.HeadSHA, args.HeadSHA),
	}
}
