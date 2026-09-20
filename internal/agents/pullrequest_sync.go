package agents

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"weave/internal/fail"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/logging"
	"weave/internal/store"
	forgestore "weave/internal/store/forge"
	pullrequeststore "weave/internal/store/pullrequest"
)

// pullRequestSyncBudget is how many requests one pass may make for one repository, the sweep
// and its branch scans together.
const pullRequestSyncBudget = 10

// pullRequestSyncOverlap is how far below the high-water mark a sweep reads, which is what
// catches a second pull request sharing the mark's second and a page reordered under the read.
const pullRequestSyncOverlap = 15 * time.Minute

// pullRequestSyncBackstop is how long an ETag is trusted before page one is read without it.
const pullRequestSyncBackstop = time.Hour

// pullRequestSyncHorizon is how far back a repository's first sweep reads.
const pullRequestSyncHorizon = 30 * 24 * time.Hour

// pullRequestThrottleFloor is the wait after a rate limit refusal that named no time, doubled
// on each refusal in a row from the same host up to pullRequestThrottleCeiling.
const (
	pullRequestThrottleFloor   = time.Minute
	pullRequestThrottleCeiling = time.Hour
)

// errRateLimitSpent says a host's budget ran out part way through a pass, after the reset was
// already recorded on its connection.
var errRateLimitSpent = fail.Exhausted("the forge's rate limit budget is spent")

// pullRequestForgeFinder is what the sync asks of the forge service.
type pullRequestForgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
	SetThrottledUntil(ctx context.Context, id string, until *time.Time) error
}

// PullRequestSyncer keeps the pull requests table in step with the forges, one pass at a time.
type PullRequestSyncer struct {
	forges       pullRequestForgeFinder
	pullRequests *pullrequeststore.Store
	tx           txRunner
	now          func() time.Time
	// refusals counts rate limit refusals in a row per host that named no time to wait.
	refusals map[string]int
	// unconnected holds the repositories already logged as having no connection.
	unconnected map[string]bool
}

type NewPullRequestSyncerParams struct {
	Forges       pullRequestForgeFinder
	PullRequests *pullrequeststore.Store
	Tx           txRunner
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// NewPullRequestSyncer builds a syncer over the forge service and the store.
func NewPullRequestSyncer(params NewPullRequestSyncerParams) *PullRequestSyncer {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &PullRequestSyncer{
		forges:       params.Forges,
		pullRequests: params.PullRequests,
		tx:           params.Tx,
		now:          now,
		refusals:     map[string]int{},
		unconnected:  map[string]bool{},
	}
}

// Sync lists every repository live agents sit on, reads what changed on its forge, and records
// which agents were seen beside which pull requests.
func (s *PullRequestSyncer) Sync(ctx context.Context) error {
	repositories, err := s.pullRequests.ListLiveRepositories(ctx)
	if err != nil {
		return err
	}

	throttledHosts := map[string]bool{}
	for _, repositoryURL := range repositories {
		s.syncRepository(ctx, repositoryURL, throttledHosts)
	}

	observed, err := s.pullRequests.Observe(ctx)
	if err != nil {
		return err
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "pull request sync pass",
		slog.Int(logging.Observations, int(observed)))

	return nil
}

// repositoryPass is what one pass did for one repository, for the log.
type repositoryPass struct {
	budget      int
	inserted    int
	updated     int
	decisions   int
	notModified bool
}

func (s *PullRequestSyncer) syncRepository(ctx context.Context, repositoryURL string, throttledHosts map[string]bool) {
	logger := logging.From(ctx).With(slog.String(logging.RepositoryURL, repositoryURL))
	ctx = logging.WithLogger(ctx, logger)

	remote, err := git.ParseRemoteURL(repositoryURL)
	if err != nil || remote.Host == "" {
		return
	}
	if throttledHosts[remote.Host] {
		return
	}

	connection, err := s.forges.ConnectionFor(ctx, remote.Host)
	if errors.Is(err, store.ErrNotFound) {
		if !s.unconnected[repositoryURL] {
			s.unconnected[repositoryURL] = true
			logger.LogAttrs(ctx, slog.LevelInfo, "no forge connection for the repository, so its pull requests are not read",
				slog.String(logging.ForgeHost, remote.Host))
		}

		return
	}
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to find the forge connection", logging.Err(err)...)

		return
	}
	if connection.ThrottledUntil != nil && connection.ThrottledUntil.After(s.now()) {
		throttledHosts[remote.Host] = true

		return
	}

	f, err := s.forges.ForConnection(*connection)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to build the forge", logging.Err(err)...)

		return
	}

	pass := &repositoryPass{budget: pullRequestSyncBudget}
	err = s.readRepository(ctx, remote, connection, f, pass)
	var throttled *forge.ThrottledError
	switch {
	case err == nil:
	case errors.Is(err, errRateLimitSpent):
		throttledHosts[remote.Host] = true
	case errors.As(err, &throttled):
		throttledHosts[remote.Host] = true
		s.recordThrottle(ctx, remote.Host, connection.ID, throttled.RetryAt)
	default:
		logger.LogAttrs(ctx, slog.LevelError, "failed to sync the repository's pull requests", logging.Err(err)...)
	}

	sync, syncErr := s.pullRequests.GetSync(ctx, repositoryURL)
	if syncErr != nil {
		return
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "pull request sync",
		slog.Int(logging.ForgeCalls, sync.Calls),
		slog.Int(logging.RowsInserted, pass.inserted),
		slog.Int(logging.RowsUpdated, pass.updated),
		slog.Int(logging.DecisionsRead, pass.decisions),
		slog.Bool(logging.NotModified, pass.notModified),
		slog.Bool(logging.SweepInProgress, sync.IsSweeping()),
	)
}

// readRepository runs the sweep, then the branch scans, then the review decisions for one
// repository within the pass's budget, and answers the error that stopped it, if one did.
func (s *PullRequestSyncer) readRepository(ctx context.Context, remote git.RemoteURL, connection *forgestore.Connection, f forge.Forge, pass *repositoryPass) error {
	repositoryURL := remote.Canonical()
	if err := s.pullRequests.EnsureSync(ctx, repositoryURL); err != nil {
		return err
	}
	if err := s.pullRequests.ResetSyncCalls(ctx, repositoryURL); err != nil {
		return err
	}

	if err := s.sweep(ctx, remote, connection, f, pass); err != nil {
		return err
	}
	if err := s.scanBranches(ctx, remote, connection, f, pass); err != nil {
		return err
	}

	return s.readDecisions(ctx, remote, f, pass)
}

// sweep reads the repository's listing by last change from where the cursor row says, page by
// page, until it reaches what it already holds or the budget runs out.
func (s *PullRequestSyncer) sweep(ctx context.Context, remote git.RemoteURL, connection *forgestore.Connection, f forge.Forge, pass *repositoryPass) error {
	repositoryURL := remote.Canonical()
	sync, err := s.pullRequests.GetSync(ctx, repositoryURL)
	if err != nil {
		return err
	}

	page := 1
	if sync.IsSweeping() {
		page = *sync.SweepNextPage
	}

	for pass.budget > 0 {
		pass.budget--

		params := forge.ListUpdatedParams{Repo: remote, Page: page}
		validatedAt := s.now()
		if page == 1 && !sync.IsSweeping() && sync.ETag != "" && sync.ValidatedAt != nil && s.now().Sub(*sync.ValidatedAt) < pullRequestSyncBackstop {
			params.ETag = sync.ETag
		}

		listed, err := f.ListUpdated(ctx, params)
		if recordErr := s.pullRequests.RecordSyncRequest(ctx, repositoryURL); recordErr != nil {
			return recordErr
		}
		if err != nil {
			return err
		}
		spent := s.noteRateLimit(ctx, remote.Host, connection.ID, listed.RateLimit)

		if listed.NotModified {
			pass.notModified = true

			return nil
		}

		if page == 1 && !sync.IsSweeping() {
			newest := s.now()
			if len(listed.PullRequests) > 0 {
				newest = listed.PullRequests[0].UpdatedAt
			}
			start := pullrequeststore.StartSweepParams{
				RepositoryURL: repositoryURL,
				Newest:        newest,
				ETag:          listed.ETag,
			}
			if params.ETag == "" {
				start.ValidatedAt = &validatedAt
			}
			if err := s.pullRequests.StartSweep(ctx, start); err != nil {
				return err
			}
		}

		stopped := !listed.HasMore || s.reachesStop(listed.PullRequests, sync.UpdatedThrough)
		if err := s.writePage(ctx, listed.PullRequests, pass, func(st *pullrequeststore.Store) error {
			if stopped {
				return st.CompleteSweep(ctx, repositoryURL)
			}

			return st.AdvanceSweep(ctx, repositoryURL, page+1)
		}); err != nil {
			return err
		}
		if stopped {
			return nil
		}
		if spent {
			return errRateLimitSpent
		}

		page++
	}

	return nil
}

// reachesStop says a page holds a pull request older than the sweep reads to: the high-water
// mark less the overlap, or the horizon on a first sweep.
func (s *PullRequestSyncer) reachesStop(pullRequests []forge.PullRequest, updatedThrough *time.Time) bool {
	cutoff := s.now().Add(-pullRequestSyncHorizon)
	if updatedThrough != nil {
		cutoff = updatedThrough.Add(-pullRequestSyncOverlap)
	}

	for _, pullRequest := range pullRequests {
		if pullRequest.UpdatedAt.Before(cutoff) {
			return true
		}
	}

	return false
}

// scanBranches reads, once, the pull requests of every branch a live agent sits on that the
// sweep's horizon may have missed.
func (s *PullRequestSyncer) scanBranches(ctx context.Context, remote git.RemoteURL, connection *forgestore.Connection, f forge.Forge, pass *repositoryPass) error {
	repositoryURL := remote.Canonical()
	branches, err := s.pullRequests.ListUnscannedBranches(ctx, repositoryURL)
	if err != nil {
		return err
	}

	for _, branch := range branches {
		page := 1
		scan, err := s.pullRequests.GetBranchScan(ctx, repositoryURL, branch)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil {
			page = scan.NextPage
		}

		for pass.budget > 0 {
			pass.budget--

			listed, err := f.List(ctx, forge.ListParams{
				Repo:    remote,
				Branch:  branch,
				Page:    page,
				PerPage: pullRequestScanPageSize,
			})
			if recordErr := s.pullRequests.RecordSyncRequest(ctx, repositoryURL); recordErr != nil {
				return recordErr
			}
			if err != nil {
				return err
			}
			spent := s.noteRateLimit(ctx, remote.Host, connection.ID, listed.RateLimit)

			if err := s.writePage(ctx, listed.PullRequests, pass, func(st *pullrequeststore.Store) error {
				if listed.HasMore {
					return st.SetBranchScanNextPage(ctx, repositoryURL, branch, page+1)
				}

				return st.CompleteBranchScan(ctx, repositoryURL, branch, page+1)
			}); err != nil {
				return err
			}
			if !listed.HasMore {
				break
			}
			if spent {
				return errRateLimitSpent
			}

			page++
		}
	}

	return nil
}

// readDecisions reads the review decision of every open pull request on a live branch that
// changed since its decision was last read, newest change first, until the budget runs out.
func (s *PullRequestSyncer) readDecisions(ctx context.Context, remote git.RemoteURL, f forge.Forge, pass *repositoryPass) error {
	repositoryURL := remote.Canonical()
	stale, err := s.pullRequests.ListStaleDecisions(ctx, repositoryURL)
	if err != nil {
		return err
	}

	for _, pullRequest := range stale {
		if pass.budget <= 0 {
			return nil
		}
		pass.budget--

		decision, err := f.GetReviewDecision(ctx, remote, pullRequest.Number)
		if recordErr := s.pullRequests.RecordSyncRequest(ctx, repositoryURL); recordErr != nil {
			return recordErr
		}
		if err != nil && !errors.Is(err, forge.ErrNotFound) {
			return err
		}

		if err := s.pullRequests.SetReviewDecision(ctx, pullRequest.ID, decision); err != nil {
			return err
		}
		pass.decisions++
	}

	return nil
}

// pullRequestScanPageSize is the page a branch scan asks for, the most a forge hands over.
const pullRequestScanPageSize = 100

// writePage upserts a page's pull requests and the position after it in one transaction, so a
// page that fails part way is written again from the start.
func (s *PullRequestSyncer) writePage(ctx context.Context, pullRequests []forge.PullRequest, pass *repositoryPass, advance func(*pullrequeststore.Store) error) error {
	inserted, updated := 0, 0
	err := s.tx.InTx(ctx, func(db store.DBTX) error {
		st := pullrequeststore.New(db)
		for _, pullRequest := range pullRequests {
			outcome, err := st.Upsert(ctx, toUpsert(pullRequest))
			if err != nil {
				return err
			}
			switch {
			case outcome.Inserted:
				inserted++
			case outcome.Changed:
				updated++
			}
		}

		return advance(st)
	})
	if err != nil {
		return err
	}

	pass.inserted += inserted
	pass.updated += updated

	return nil
}

// noteRateLimit records a spent budget on the connection so the next pass waits for the reset
// rather than spending a request to be refused, and says whether it was spent.
func (s *PullRequestSyncer) noteRateLimit(ctx context.Context, host, connectionID string, limit forge.RateLimit) bool {
	if limit.Known {
		delete(s.refusals, host)
	}
	if !limit.IsExhausted() {
		return false
	}

	s.recordThrottle(ctx, host, connectionID, limit.ResetAt)

	return true
}

// recordThrottle writes when to try the host again, which is what the forge said or, when it
// said nothing, a wait that doubles with every refusal in a row.
func (s *PullRequestSyncer) recordThrottle(ctx context.Context, host, connectionID string, retryAt time.Time) {
	until := retryAt
	if until.IsZero() {
		wait := pullRequestThrottleFloor
		for range s.refusals[host] {
			wait = min(wait*2, pullRequestThrottleCeiling)
		}
		s.refusals[host]++
		until = s.now().Add(wait)
	}

	if err := s.forges.SetThrottledUntil(ctx, connectionID, &until); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to record the forge's rate limit", logging.Err(err)...)

		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge is rate limiting the connection",
		slog.String(logging.ForgeHost, host),
		slog.Time(logging.ThrottledUntil, until))
}

// toUpsert is a pull request as the forge described it, in the store's terms.
func toUpsert(pullRequest forge.PullRequest) pullrequeststore.UpsertParams {
	head := ""
	if !pullRequest.Head.IsZero() {
		head = pullRequest.Head.Canonical()
	}

	return pullrequeststore.UpsertParams{
		Host:              pullRequest.Repository.Host,
		Forge:             pullRequest.Forge,
		ProviderID:        pullRequest.ProviderID,
		URL:               pullRequest.URL,
		RepositoryURL:     pullRequest.Repository.Canonical(),
		HeadRepositoryURL: head,
		Number:            pullRequest.Number,
		Title:             pullRequest.Title,
		HeadBranch:        pullRequest.HeadBranch,
		HeadSHA:           pullRequest.HeadSHA,
		BaseBranch:        pullRequest.BaseBranch,
		BaseSHA:           pullRequest.BaseSHA,
		Draft:             pullRequest.Draft,
		State:             pullRequest.State,
		OpenedAt:          pullRequest.CreatedAt,
		ChangedAt:         pullRequest.UpdatedAt,
		MergedAt:          pullRequest.MergedAt,
		ClosedAt:          pullRequest.ClosedAt,
	}
}
