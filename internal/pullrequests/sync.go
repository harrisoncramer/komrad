package pullrequests

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"komrad/internal/events"
	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/clock"
	"komrad/lib/xslice"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// pullRequestSyncBudget is how many requests one pass may make for one repository, its first
// sweep, hourly listing, branch scans and standings together, and how many pages one search
// may read for one host.
const pullRequestSyncBudget = 10

// checksPendingAfter is how long a read of a pending run is trusted before it is read again.
const checksPendingAfter = time.Minute

// checksSettleWithin is how long after a change a head with no checks or no computed
// mergeability is read again.
const checksSettleWithin = 10 * time.Minute

// checksSettledAfter is how long any read of the checks and mergeability is trusted before it
// is read again.
const checksSettledAfter = 10 * time.Minute

// pullRequestSyncOverlap is how far below the high-water mark a search reads.
const pullRequestSyncOverlap = 15 * time.Minute

// pullRequestSyncListing is how often page one of a repository's own listing is read behind
// the search.
const pullRequestSyncListing = time.Hour

// pullRequestSyncHorizon is how far back a repository's first sweep reads.
const pullRequestSyncHorizon = 30 * 24 * time.Hour

// pullRequestSyncStale is how far behind a repository's mark may fall before it is swept again
// rather than searched.
const pullRequestSyncStale = 24 * time.Hour

// pullRequestSearchCap is the most rows a forge's search answers, at which the repositories
// searched go back to a first sweep.
const pullRequestSearchCap = 1000

// pullRequestScanPageSize is the page a branch scan asks for, the most a forge hands over.
const pullRequestScanPageSize = 100

// pullRequestThrottleFloor is the wait after a rate limit refusal that named no time, doubled
// on each refusal in a row from the same host up to pullRequestThrottleCeiling.
const (
	pullRequestThrottleFloor   = time.Minute
	pullRequestThrottleCeiling = time.Hour
)

// errRateLimitSpent says a host's budget ran out part way through a pass, after the reset was
// already recorded on its connection.
var errRateLimitSpent = fail.Exhausted("the forge's rate limit budget is spent")

type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

// pullRequestForgeFinder is what the sync asks of the forge service.
type pullRequestForgeFinder interface {
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
	ForConnection(connection forgestore.Connection) (forge.Forge, error)
	SetThrottledUntil(ctx context.Context, id string, until *time.Time) error
}

// onceLog remembers what has been said once already, by what kind of thing it was about and
// which one.
type onceLog map[string]bool

// first says whether nothing has been said about the key yet, and remembers that it has now.
func (l onceLog) first(kind, key string) bool {
	if l[kind+":"+key] {
		return false
	}

	l[kind+":"+key] = true

	return true
}

// forget lets the key be said about again.
func (l onceLog) forget(kind, key string) {
	delete(l, kind+":"+key)
}

// The kinds of thing the syncer says once.
const (
	saidUnconnected = "unconnected"
	saidMissing     = "missing"
	saidRefused     = "refused"
	saidRenamed     = "renamed"
)

// NewPullRequestSyncerParams is what a pull request syncer is built over.
type NewPullRequestSyncerParams struct {
	Forges       pullRequestForgeFinder
	PullRequests *pullrequeststore.PullRequestStore
	Syncs        *pullrequeststore.PullRequestSyncStore
	Events       eventRecorder
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// PullRequestSyncer keeps the pull requests table in step with the forges, one pass at a time.
type PullRequestSyncer struct {
	forges       pullRequestForgeFinder
	pullRequests *pullrequeststore.PullRequestStore
	syncs        *pullrequeststore.PullRequestSyncStore
	events       eventRecorder
	now          func() time.Time
	// refusals counts rate limit refusals in a row per host that named no time to wait.
	refusals map[string]int
	said     onceLog
}

// NewPullRequestSyncer builds a syncer over the forge service and the stores.
func NewPullRequestSyncer(params NewPullRequestSyncerParams) *PullRequestSyncer {
	now := params.Now
	if now == nil {
		now = clock.Now
	}

	return &PullRequestSyncer{
		forges:       params.Forges,
		pullRequests: params.PullRequests,
		syncs:        params.Syncs,
		events:       params.Events,
		now:          now,
		refusals:     map[string]int{},
		said:         onceLog{},
	}
}

// Sync lists every repository live agents sit on, reads what changed on its forge, and records
// which agents were seen beside which pull requests.
func (s *PullRequestSyncer) Sync(ctx context.Context) error {
	repositories, err := s.pullRequests.ListLiveRepositories(ctx)
	if err != nil {
		return err
	}

	for _, host := range s.groupByHost(ctx, repositories) {
		s.syncHost(ctx, host)
	}

	observed, err := s.pullRequests.Observe(ctx)
	if err != nil {
		return err
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "pull request sync pass",
		slog.Int(logging.Observations, int(observed)))

	return nil
}

// hostRead is one forge host being read in a pass, with the connection and forge it is read
// through and the repositories live agents sit on there.
type hostRead struct {
	host         string
	connection   *forgestore.Connection
	forge        forge.Forge
	repositories []*repositoryRead
	// throttled says nothing more is asked of the host this pass.
	throttled bool
}

// repositoryRead is one repository being read in a pass, with what the pass has done for it
// and the error that stopped it.
type repositoryRead struct {
	remote git.RemoteURL
	tally  *passCount
	err    error
}

// passCount is what a pass did against a budget, for the log.
type passCount struct {
	budget    int
	inserted  int
	updated   int
	standings int
}

// groupByHost resolves the connection and forge for each host the repositories live on,
// leaving out the hosts nobody connected and the ones waiting on a rate limit.
func (s *PullRequestSyncer) groupByHost(ctx context.Context, repositories []string) []*hostRead {
	hosts := []*hostRead{}
	byHost := map[string]*hostRead{}
	skipped := map[string]bool{}
	for _, repositoryURL := range repositories {
		remote, err := git.ParseRemoteURL(repositoryURL)
		if err != nil || remote.Host == "" || skipped[remote.Host] {
			continue
		}

		host, ok := byHost[remote.Host]
		if !ok {
			host = s.openHost(ctx, remote.Host, repositoryURL)
			if host == nil {
				skipped[remote.Host] = true

				continue
			}

			byHost[remote.Host] = host
			hosts = append(hosts, host)
		}

		host.repositories = append(host.repositories, &repositoryRead{
			remote: remote,
			tally:  &passCount{budget: pullRequestSyncBudget},
		})
	}

	return hosts
}

// openHost is the host with its connection and forge, nil when it cannot be read this pass.
func (s *PullRequestSyncer) openHost(ctx context.Context, hostname, repositoryURL string) *hostRead {
	logger := logging.From(ctx).With(slog.String(logging.ForgeHost, hostname))

	connection, err := s.forges.ConnectionFor(ctx, hostname)
	if errors.Is(err, store.ErrNotFound) {
		if s.said.first(saidUnconnected, repositoryURL) {
			logger.LogAttrs(ctx, slog.LevelInfo, "no forge connection for the repository, so its pull requests are not read",
				slog.String(logging.RepositoryURL, repositoryURL))
		}

		return nil
	}
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to find the forge connection", logging.Err(err)...)

		return nil
	}
	if connection.ThrottledUntil != nil && connection.ThrottledUntil.After(s.now()) {
		return nil
	}

	f, err := s.forges.ForConnection(*connection)
	if err != nil {
		logger.LogAttrs(ctx, slog.LevelError, "failed to build the forge", logging.Err(err)...)

		return nil
	}

	return &hostRead{
		host:       hostname,
		connection: connection,
		forge:      f,
	}
}

// syncHost reads one host in the pass's order, the first sweeps, then the search, then each
// repository's hourly listing, branch scans and decisions.
func (s *PullRequestSyncer) syncHost(ctx context.Context, host *hostRead) {
	for _, read := range host.repositories {
		if err := s.prepare(ctx, read); err != nil {
			read.err = err
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to prepare the repository's sync row",
				append([]slog.Attr{slog.String(logging.RepositoryURL, read.remote.Canonical())}, logging.Err(err)...)...)

			continue
		}

		s.step(ctx, host, read, s.sweep)
	}

	s.search(ctx, host)

	for _, read := range host.repositories {
		s.step(ctx, host, read, s.list)
		s.step(ctx, host, read, s.scanBranches)
		s.step(ctx, host, read, s.readStandings)
		if read.err == nil {
			s.said.forget(saidMissing, read.remote.Canonical())
		}
		s.logRepository(ctx, read)
	}
	if !host.throttled {
		s.said.forget(saidRefused, host.host)
	}
}

// step runs one part of a repository's pass, unless the host is spent or an earlier part
// failed, and records what stopped it.
func (s *PullRequestSyncer) step(ctx context.Context, host *hostRead, read *repositoryRead, run func(context.Context, *hostRead, *repositoryRead) error) {
	if host.throttled || read.err != nil {
		return
	}

	repositoryURL := read.remote.Canonical()
	err := run(ctx, host, read)
	switch {
	case err == nil:
	case s.settleHostError(ctx, host, err):
	case errors.Is(err, forge.ErrNotFound):
		read.err = err
		if s.said.first(saidMissing, repositoryURL) {
			logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge has no such repository, so its pull requests are not read until it does",
				append([]slog.Attr{slog.String(logging.RepositoryURL, repositoryURL)}, logging.Err(err)...)...)
		}
	default:
		read.err = err
		logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to sync the repository's pull requests",
			append([]slog.Attr{slog.String(logging.RepositoryURL, repositoryURL)}, logging.Err(err)...)...)
	}
}

// settleHostError stops the host for the pass when the error says its rate limit or its
// credential did, and says whether it was one of those.
func (s *PullRequestSyncer) settleHostError(ctx context.Context, host *hostRead, err error) bool {
	var throttled *forge.ThrottledError
	switch {
	case errors.Is(err, errRateLimitSpent):
		host.throttled = true
	case errors.As(err, &throttled):
		s.throttleHost(ctx, host, throttled.RetryAt)
	case errors.Is(err, forge.ErrUnauthorized):
		s.refuseHost(ctx, host, err)
	default:
		return false
	}

	return true
}

// throttleHost stops a host for the pass after a rate limit refusal, records when to try it
// again, and says so.
func (s *PullRequestSyncer) throttleHost(ctx context.Context, host *hostRead, retryAt time.Time) {
	host.throttled = true
	until := s.recordThrottle(ctx, host.host, host.connection.ID, retryAt)
	if until.IsZero() {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge is rate limiting the connection",
		slog.String(logging.ForgeHost, host.host),
		slog.Time(logging.ThrottledUntil, until))
}

// refuseHost stops a host for the pass after the forge refused its credential, backs off the
// way a rate limit refusal that named no time does, and says so once per host.
func (s *PullRequestSyncer) refuseHost(ctx context.Context, host *hostRead, err error) {
	host.throttled = true
	s.recordThrottle(ctx, host.host, host.connection.ID, time.Time{})
	if !s.said.first(saidRefused, host.host) {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge refused the connection's credential, so its pull requests wait for it to be accepted again",
		append([]slog.Attr{slog.String(logging.ForgeHost, host.host)}, logging.Err(err)...)...)
}

// prepare makes sure the repository has a sync row and starts its count of requests at zero.
func (s *PullRequestSyncer) prepare(ctx context.Context, read *repositoryRead) error {
	repositoryURL := read.remote.Canonical()
	if err := s.syncs.EnsureSync(ctx, repositoryURL); err != nil {
		return err
	}

	return s.syncs.ResetSyncCalls(ctx, repositoryURL)
}

func (s *PullRequestSyncer) logRepository(ctx context.Context, read *repositoryRead) {
	repositoryURL := read.remote.Canonical()
	sync, err := s.syncs.GetSync(ctx, repositoryURL)
	if err != nil {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "pull request sync",
		slog.String(logging.RepositoryURL, repositoryURL),
		slog.Int(logging.ForgeCalls, sync.Calls),
		slog.Int(logging.RowsInserted, read.tally.inserted),
		slog.Int(logging.RowsUpdated, read.tally.updated),
		slog.Int(logging.StandingsRead, read.tally.standings),
		slog.Bool(logging.SweepInProgress, sync.IsSweeping()),
	)
}

// sweep reads a repository that has had no first sweep, or whose mark has fallen a day behind,
// by its listing, newest change first, page by page from where the cursor row says, until it
// reaches the horizon or the budget runs out.
func (s *PullRequestSyncer) sweep(ctx context.Context, host *hostRead, read *repositoryRead) error {
	remote := read.remote
	repositoryURL := remote.Canonical()
	sync, err := s.syncs.GetSync(ctx, repositoryURL)
	if err != nil {
		return err
	}
	if sync.IsSwept() {
		if s.now().Sub(*sync.UpdatedThrough) < pullRequestSyncStale {
			return nil
		}

		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "the repository's mark is more than a day behind, so it is swept again rather than searched",
			slog.String(logging.RepositoryURL, repositoryURL),
			slog.Time(logging.UpdatedThrough, *sync.UpdatedThrough))
		if err := s.syncs.ResetSweeps(ctx, []string{repositoryURL}); err != nil {
			return err
		}

		sync.SweepCursor = nil
	}

	cursor := ""
	started := sync.IsSweeping()
	if started {
		cursor = *sync.SweepCursor
	}

	var listedAt time.Time

	return s.pages(ctx, host, pagesParams{
		urls:   []string{repositoryURL},
		counts: read.tally,
		cursor: cursor,
		read: func(cursor string) (*forge.Page, error) {
			listedAt = s.now()
			listed, err := host.forge.ListUpdated(ctx, forge.ListUpdatedParams{Repo: remote, Cursor: cursor})
			if err != nil {
				return nil, err
			}
			s.noteRename(ctx, repositoryURL, listed.PullRequests)

			return listed, nil
		},
		last: func(listed *forge.Page) bool { return s.reachesHorizon(listed.PullRequests) },
		advance: func(listed *forge.Page, done bool, st *pullrequeststore.PullRequestSyncStore) error {
			if !started {
				newest := listedAt
				if len(listed.PullRequests) > 0 && listed.PullRequests[0].UpdatedAt.After(listedAt) {
					newest = listed.PullRequests[0].UpdatedAt
				}
				if err := st.StartSweep(ctx, pullrequeststore.StartSweepParams{
					RepositoryURL: repositoryURL,
					Newest:        newest,
					ListedAt:      listedAt,
				}); err != nil {
					return err
				}

				started = true
			}
			if done {
				return st.CompleteSweep(ctx, repositoryURL)
			}

			return st.AdvanceSweep(ctx, repositoryURL, listed.Cursor)
		},
	})
}

// reachesHorizon says a page holds a pull request older than a first sweep reads to.
func (s *PullRequestSyncer) reachesHorizon(pullRequests []forge.PullRequest) bool {
	cutoff := s.now().Add(-pullRequestSyncHorizon)
	for _, pullRequest := range pullRequests {
		if pullRequest.UpdatedAt.Before(cutoff) {
			return true
		}
	}

	return false
}

// search reads every pull request that changed across the host's swept repositories since the
// oldest of their marks less the overlap, writes each with its decision, and moves each
// repository's mark to what the search covered for it.
func (s *PullRequestSyncer) search(ctx context.Context, host *hostRead) {
	if host.throttled {
		return
	}

	searched := s.pickSwept(ctx, host)
	if len(searched) == 0 {
		return
	}

	pass := &searchPass{
		searchedAt: s.now(),
		counts:     &passCount{budget: pullRequestSyncBudget},
		newest:     map[string]time.Time{},
	}
	for _, read := range searched {
		pass.repos = append(pass.repos, read.remote)
		pass.urls = append(pass.urls, read.remote.Canonical())
		if pass.since.IsZero() || read.mark.Before(pass.since) {
			pass.since = read.mark
		}
	}
	pass.since = pass.since.Add(-pullRequestSyncOverlap)

	logger := logging.From(ctx).With(slog.String(logging.ForgeHost, host.host))
	err := s.searchPages(ctx, host, pass)
	if err != nil && !s.settleHostError(ctx, host, err) {
		logger.LogAttrs(ctx, slog.LevelError, "failed to search the host's pull requests", logging.Err(err)...)
	}

	logger.LogAttrs(ctx, slog.LevelInfo, "pull request search",
		slog.Int(logging.Repositories, len(pass.urls)),
		slog.Int(logging.RowsInserted, pass.counts.inserted),
		slog.Int(logging.RowsUpdated, pass.counts.updated),
		slog.Int(logging.RowsListed, pass.rows),
		slog.Bool(logging.SearchCapped, pass.capped),
	)
}

// searchPass is one search over a host's swept repositories, and what it has answered so far.
type searchPass struct {
	repos      []git.RemoteURL
	urls       []string
	since      time.Time
	searchedAt time.Time
	counts     *passCount
	// newest is the newest change the search answered for each repository.
	newest map[string]time.Time
	rows   int
	// capped says the search reached the forge's cap.
	capped bool
}

// searchPages reads the search page by page, writing each, and in the last page's commit moves
// the marks, or hands the repositories back to a first sweep when the search was capped.
func (s *PullRequestSyncer) searchPages(ctx context.Context, host *hostRead, pass *searchPass) error {
	return s.pages(ctx, host, pagesParams{
		urls:   pass.urls,
		counts: pass.counts,
		read: func(cursor string) (*forge.Page, error) {
			return host.forge.ListChanged(ctx, forge.ListChangedParams{
				Repos:  pass.repos,
				Since:  pass.since,
				Cursor: cursor,
			})
		},
		last: func(*forge.Page) bool { return pass.counts.budget == 0 },
		advance: func(listed *forge.Page, done bool, st *pullrequeststore.PullRequestSyncStore) error {
			pass.rows += len(listed.PullRequests)
			for _, pullRequest := range listed.PullRequests {
				repositoryURL := pullRequest.Repository.Canonical()
				if pullRequest.UpdatedAt.After(pass.newest[repositoryURL]) {
					pass.newest[repositoryURL] = pullRequest.UpdatedAt
				}
			}

			pass.capped = pass.rows >= pullRequestSearchCap || (done && listed.HasMore)
			if !done {
				return nil
			}

			return pass.settle(ctx, st)
		},
	})
}

// settle moves each repository's mark to the moment of the search, or to the newest change it
// answered when the forge's clock is ahead, or hands them all back to a first sweep when the
// search was capped.
func (p *searchPass) settle(ctx context.Context, st *pullrequeststore.PullRequestSyncStore) error {
	if p.capped {
		return st.ResetSweeps(ctx, p.urls)
	}

	for _, repositoryURL := range p.urls {
		through := p.searchedAt
		if answered, ok := p.newest[repositoryURL]; ok && answered.After(through) {
			through = answered
		}
		if err := st.AdvanceMark(ctx, repositoryURL, through); err != nil {
			return err
		}
	}

	return nil
}

// sweptRead is a repository whose first sweep has completed, with its high-water mark.
type sweptRead struct {
	*repositoryRead
	mark time.Time
}

// pickSwept is every repository on the host whose first sweep has completed and nothing has
// stopped this pass.
func (s *PullRequestSyncer) pickSwept(ctx context.Context, host *hostRead) []sweptRead {
	swept := []sweptRead{}
	for _, read := range host.repositories {
		if read.err != nil {
			continue
		}

		sync, err := s.syncs.GetSync(ctx, read.remote.Canonical())
		if err != nil || !sync.IsSwept() {
			continue
		}

		swept = append(swept, sweptRead{repositoryRead: read, mark: *sync.UpdatedThrough})
	}

	return swept
}

// list reads page one of a swept repository's own listing once an hour.
func (s *PullRequestSyncer) list(ctx context.Context, host *hostRead, read *repositoryRead) error {
	remote := read.remote
	repositoryURL := remote.Canonical()
	sync, err := s.syncs.GetSync(ctx, repositoryURL)
	if err != nil {
		return err
	}
	if !sync.IsSwept() {
		return nil
	}
	if sync.ListedAt != nil && s.now().Sub(*sync.ListedAt) < pullRequestSyncListing {
		return nil
	}

	var listedAt time.Time

	return s.pages(ctx, host, pagesParams{
		urls:   []string{repositoryURL},
		counts: read.tally,
		read: func(string) (*forge.Page, error) {
			listedAt = s.now()
			listed, err := host.forge.ListUpdated(ctx, forge.ListUpdatedParams{Repo: remote})
			if err != nil {
				return nil, err
			}
			s.noteRename(ctx, repositoryURL, listed.PullRequests)

			return listed, nil
		},
		last: func(*forge.Page) bool { return true },
		advance: func(_ *forge.Page, _ bool, st *pullrequeststore.PullRequestSyncStore) error {
			return st.MarkListed(ctx, repositoryURL, listedAt)
		},
	})
}

// noteRename says once, per repository, that the forge answered its listing under another
// name.
func (s *PullRequestSyncer) noteRename(ctx context.Context, repositoryURL string, pullRequests []forge.PullRequest) {
	if len(pullRequests) == 0 {
		return
	}

	answered := pullRequests[0].Repository.Canonical()
	if pullRequests[0].Repository.IsZero() || answered == repositoryURL || !s.said.first(saidRenamed, repositoryURL) {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge now answers the repository under another name, so its pull requests are stored under that one and the search misses it until the checkout's remote is updated",
		slog.String(logging.RepositoryURL, repositoryURL),
		slog.String(logging.RenamedRepositoryURL, answered))
}

// scanBranches reads, once, the pull requests of every branch a live agent sits on that the
// sweep's horizon may have missed.
func (s *PullRequestSyncer) scanBranches(ctx context.Context, host *hostRead, read *repositoryRead) error {
	remote := read.remote
	repositoryURL := remote.Canonical()
	branches, err := s.syncs.ListUnscannedBranches(ctx, repositoryURL)
	if err != nil {
		return err
	}

	for _, branch := range branches {
		cursor := ""
		scan, err := s.syncs.GetBranchScan(ctx, repositoryURL, branch)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err == nil {
			cursor = scan.NextCursor
		}

		err = s.pages(ctx, host, pagesParams{
			urls:   []string{repositoryURL},
			counts: read.tally,
			cursor: cursor,
			read: func(cursor string) (*forge.Page, error) {
				return host.forge.List(ctx, forge.ListParams{
					Repo:    remote,
					Branch:  branch,
					Cursor:  cursor,
					PerPage: pullRequestScanPageSize,
				})
			},
			advance: func(listed *forge.Page, done bool, st *pullrequeststore.PullRequestSyncStore) error {
				if done {
					return st.CompleteBranchScan(ctx, repositoryURL, branch, listed.Cursor)
				}

				return st.SetBranchScanCursor(ctx, repositoryURL, branch, listed.Cursor)
			},
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// readStandings reads the review decision, checks and mergeability of every open pull request
// on a live branch whose decision is stale or whose checks were read for another head or too
// long ago, newest change first and fifty to a request, until the budget runs out.
func (s *PullRequestSyncer) readStandings(ctx context.Context, host *hostRead, read *repositoryRead) error {
	remote, counts := read.remote, read.tally
	repositoryURL := remote.Canonical()
	now := s.now()
	stale, err := s.syncs.ListStaleStandings(ctx, pullrequeststore.ListStaleStandingsParams{
		RepositoryURL: repositoryURL,
		Now:           now,
		PendingAfter:  checksPendingAfter,
		SettleWithin:  checksSettleWithin,
		SettledAfter:  checksSettledAfter,
	})
	if err != nil {
		return err
	}

	for len(stale) > 0 && counts.budget > 0 {
		counts.budget--

		batch := stale[:min(len(stale), forge.MaxStandings)]
		stale = stale[len(batch):]
		numbers := xslice.Map(batch, func(pullRequest pullrequeststore.PullRequest) int {
			return pullRequest.Number
		})

		standings, err := host.forge.Standings(ctx, remote, numbers)
		if recordErr := s.syncs.RecordSyncRequest(ctx, repositoryURL); recordErr != nil {
			return recordErr
		}
		if err != nil {
			return err
		}

		for _, pullRequest := range batch {
			standing, answered := standings[pullRequest.Number]
			if !answered {
				logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge has no pull request to read a standing for, so none is recorded until it changes",
					slog.String(logging.RepositoryURL, repositoryURL),
					slog.Int(logging.PullRequestNumber, pullRequest.Number))
				standing = forge.Standing{HeadSHA: pullRequest.HeadSHA}
			}

			if err := s.setStanding(ctx, pullRequest.ID, standing, now); err != nil {
				return err
			}
			counts.standings++
		}
	}

	return nil
}

// pagesParams is one listing to read page by page.
type pagesParams struct {
	// urls are the repositories each request is counted against.
	urls   []string
	counts *passCount
	// cursor is where the first page is read from, empty for page one.
	cursor string
	// read asks the forge for the page at a cursor.
	read func(cursor string) (*forge.Page, error)
	// last says the page is the last one wanted even when the forge has more, nil when only
	// the forge decides.
	last func(*forge.Page) bool
	// advance records where the listing stands after a page, in the page's transaction, told
	// whether the page was the last.
	advance func(listed *forge.Page, done bool, st *pullrequeststore.PullRequestSyncStore) error
}

// pages reads a listing page by page against the budget, counting each request and writing
// each page with where the listing then stands, until the listing is done, the budget runs out,
// or the host's rate limit is spent.
func (s *PullRequestSyncer) pages(ctx context.Context, host *hostRead, params pagesParams) error {
	cursor := params.cursor
	for params.counts.budget > 0 {
		params.counts.budget--

		listed, err := params.read(cursor)
		for _, repositoryURL := range params.urls {
			if recordErr := s.syncs.RecordSyncRequest(ctx, repositoryURL); recordErr != nil {
				return recordErr
			}
		}
		if err != nil {
			return err
		}
		spent := s.noteRateLimit(ctx, host, listed.RateLimit)

		done := !listed.HasMore || (params.last != nil && params.last(listed))
		if err := s.writePage(ctx, listed.PullRequests, params.counts, func(st *pullrequeststore.PullRequestSyncStore) error {
			return params.advance(listed, done, st)
		}); err != nil {
			return err
		}
		if done {
			return nil
		}
		if spent {
			return errRateLimitSpent
		}

		cursor = listed.Cursor
	}

	return nil
}

// writePage upserts a page's pull requests, the standing of each that carries one, the position
// after it, and an event for each pull request that moved, in one transaction.
func (s *PullRequestSyncer) writePage(ctx context.Context, pullRequests []forge.PullRequest, counts *passCount, advance func(*pullrequeststore.PullRequestSyncStore) error) error {
	inserted, updated := 0, 0
	err := s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		st := pullrequeststore.NewPullRequestStore(tx)
		var recorded []river.JobArgs
		for _, pullRequest := range pullRequests {
			upsert := toUpsert(pullRequest)
			outcome, err := st.Upsert(ctx, upsert)
			if err != nil {
				return nil, err
			}
			switch {
			case outcome.Inserted:
				inserted++
			case outcome.Changed:
				updated++
			}

			moved := []*Move{
				Moved(FieldState, outcome.OldState, &upsert.State),
			}

			if pullRequest.Decision != nil {
				standing := forge.Standing{Decision: *pullRequest.Decision, HeadSHA: pullRequest.HeadSHA}
				if pullRequest.Checks != nil {
					standing.Checks = *pullRequest.Checks
				}
				if pullRequest.Mergeability != nil {
					standing.Mergeability = *pullRequest.Mergeability
				}
				change, err := st.SetStanding(ctx, outcome.ID, standing, s.now())
				if err != nil {
					return nil, err
				}
				moved = append(moved, standingMoves(change)...)
			}

			if change := NewChange(outcome.ID, moved...); change != nil {
				event, err := events.NewEvent(change)
				if err != nil {
					return nil, err
				}
				recorded = append(recorded, *event)
			}
		}

		return recorded, advance(pullrequeststore.NewPullRequestSyncStore(tx))
	})
	if err != nil {
		return err
	}

	counts.inserted += inserted
	counts.updated += updated

	return nil
}

// setStanding records one pull request's standing and the event for whatever it moved in one
// transaction.
func (s *PullRequestSyncer) setStanding(ctx context.Context, id string, standing forge.Standing, readAt time.Time) error {
	return s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		change, err := pullrequeststore.NewPullRequestStore(tx).SetStanding(ctx, id, standing, readAt)
		if err != nil {
			return nil, err
		}

		moved := NewChange(id, standingMoves(change)...)
		if moved == nil {
			return nil, nil
		}
		event, err := events.NewEvent(moved)
		if err != nil {
			return nil, err
		}

		return []river.JobArgs{*event}, nil
	})
}

func standingMoves(change *pullrequeststore.StandingChange) []*Move {
	return []*Move{
		Moved(FieldDecision, change.OldDecision, change.KnownDecision),
		checksMoved(change),
		Moved(FieldMergeability, change.OldMergeability, change.KnownMergeability),
	}
}

// checksMoved is the move of the checks, counting the same verdict on a new head as a move so
// that a push the checks fail again is not taken for no change.
func checksMoved(change *pullrequeststore.StandingChange) *Move {
	if move := Moved(FieldChecks, change.OldChecks, change.KnownChecks); move != nil {
		return move
	}
	if change.OldChecks == nil || change.KnownChecks == nil || Moved(FieldChecks, change.OldChecksSHA, change.KnownChecksSHA) == nil {
		return nil
	}

	return &Move{Field: FieldChecks, From: *change.OldChecks, To: *change.KnownChecks}
}

// noteRateLimit records a spent budget on the connection so the next pass waits for the reset
// rather than spending a request to be refused, and says whether it was spent.
func (s *PullRequestSyncer) noteRateLimit(ctx context.Context, host *hostRead, limit forge.RateLimit) bool {
	if limit.Known {
		delete(s.refusals, host.host)
	}
	if !limit.IsExhausted() {
		return false
	}

	s.throttleHost(ctx, host, limit.ResetAt)

	return true
}

// recordThrottle writes when to try the host again, which is what the forge said or, when it
// said nothing, a wait that doubles with every refusal in a row, and answers it, zero when
// the write failed.
func (s *PullRequestSyncer) recordThrottle(ctx context.Context, host, connectionID string, retryAt time.Time) time.Time {
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

		return time.Time{}
	}

	return until
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
