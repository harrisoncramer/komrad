package pullrequests

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	"komrad/internal/events"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

// waker asks the sync's own loop to run before its next tick.
type waker interface {
	Wake()
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
	// Loop wakes the sync's own loop.
	Loop waker
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// PullRequestSyncer keeps the pull requests on the branches live agents sit on in step with the
// forges, reading each repository again soon when something in it is likely to change.
type PullRequestSyncer struct {
	forges       pullRequestForgeFinder
	pullRequests *pullrequeststore.PullRequestStore
	syncs        *pullrequeststore.PullRequestSyncStore
	events       eventRecorder
	loop         waker
	now          func() time.Time
	backoff      *hostBackoff
	pacing       *pacing
	said         onceLog
}

// NewPullRequestSyncer builds a syncer over the forge service and the stores.
func NewPullRequestSyncer(params NewPullRequestSyncerParams) *PullRequestSyncer {
	now := params.Now
	if now == nil {
		now = time.Now
	}

	return &PullRequestSyncer{
		forges:       params.Forges,
		pullRequests: params.PullRequests,
		syncs:        params.Syncs,
		events:       params.Events,
		loop:         params.Loop,
		now:          now,
		backoff:      newHostBackoff(params.Forges, now),
		pacing:       newPacing(),
		said:         onceLog{},
	}
}

// Wake makes every repository due and wakes the sync's loop to read them.
func (s *PullRequestSyncer) Wake() {
	s.pacing.wake()
	s.loop.Wake()
}

// Heat keeps every repository on the host in the fast lane for a while, after komrad changed a
// pull request there.
func (s *PullRequestSyncer) Heat(host string) {
	s.pacing.heat(host, s.now())
}

// Sync reads every due repository live agents sit on, and records which agents were seen beside
// which pull requests when it read any.
func (s *PullRequestSyncer) Sync(ctx context.Context) error {
	repositories, err := s.pullRequests.ListLiveRepositories(ctx)
	if err != nil {
		return err
	}
	targets, err := s.syncs.ListTargets(ctx)
	if err != nil {
		return err
	}

	due := s.pacing.pick(candidatesOf(repositories, targets), s.now())
	if len(due) == 0 {
		return nil
	}

	branches := branchesOf(targets)
	read := false
	for _, host := range s.groupByHost(ctx, due) {
		read = s.syncHost(ctx, host, branches) || read
	}
	if !read {
		return nil
	}

	observed, err := s.pullRequests.Observe(ctx)
	if err != nil {
		return err
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "pull request sync pass",
		slog.Int(logging.Repositories, len(due)),
		slog.Int(logging.Observations, int(observed)))

	return nil
}

// candidatesOf is every repository with the fingerprint of its agents' branches and heads.
func candidatesOf(repositories []string, targets []pullrequeststore.Target) []dueParams {
	fingerprints := map[string]*strings.Builder{}
	for _, target := range targets {
		fingerprint, ok := fingerprints[target.RepositoryURL]
		if !ok {
			fingerprint = &strings.Builder{}
			fingerprints[target.RepositoryURL] = fingerprint
		}
		fingerprint.WriteString(target.Branch + "@" + target.HeadSHA + "\n")
	}

	candidates := make([]dueParams, 0, len(repositories))
	for _, repositoryURL := range repositories {
		remote, err := git.ParseRemoteURL(repositoryURL)
		if err != nil || remote.Host == "" {
			continue
		}

		candidate := dueParams{repositoryURL: repositoryURL, host: remote.Host}
		if fingerprint, ok := fingerprints[repositoryURL]; ok {
			candidate.fingerprint = fingerprint.String()
		}
		candidates = append(candidates, candidate)
	}

	return candidates
}

// branchesOf is the distinct branches of each repository, in the order the targets name them.
func branchesOf(targets []pullrequeststore.Target) map[string][]string {
	branches := map[string][]string{}
	for _, target := range targets {
		named := branches[target.RepositoryURL]
		if len(named) == 0 || named[len(named)-1] != target.Branch {
			branches[target.RepositoryURL] = append(named, target.Branch)
		}
	}

	return branches
}

// hostRead is one forge host being read in a pass, with the connection and forge it is read
// through and its due repositories.
type hostRead struct {
	host         string
	connection   *forgestore.Connection
	forge        forge.Forge
	repositories []dueParams
	// throttled says nothing more is asked of the host this pass.
	throttled bool
}

// groupByHost resolves the connection and forge for each host the repositories live on,
// leaving out the hosts nobody connected and the ones waiting on a rate limit.
func (s *PullRequestSyncer) groupByHost(ctx context.Context, repositories []dueParams) []*hostRead {
	hosts := []*hostRead{}
	byHost := map[string]*hostRead{}
	skipped := map[string]bool{}
	for _, repository := range repositories {
		if skipped[repository.host] {
			continue
		}

		host, ok := byHost[repository.host]
		if !ok {
			host = s.openHost(ctx, repository.host, repository.repositoryURL)
			if host == nil {
				skipped[repository.host] = true

				continue
			}

			byHost[repository.host] = host
			hosts = append(hosts, host)
		}

		host.repositories = append(host.repositories, repository)
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

// syncHost reads each of the host's due repositories until the host's rate limit or credential
// stops it, and says whether it read any.
func (s *PullRequestSyncer) syncHost(ctx context.Context, host *hostRead, branches map[string][]string) bool {
	read := false
	for _, repository := range host.repositories {
		if host.throttled {
			break
		}

		err := s.readRepository(ctx, host, repository, branches[repository.repositoryURL])
		switch {
		case err == nil:
			read = true
			s.said.forget(saidMissing, repository.repositoryURL)
		case s.settleHostError(ctx, host, err):
		case errors.Is(err, forge.ErrNotFound):
			s.pacing.fail(repository)
			if s.said.first(saidMissing, repository.repositoryURL) {
				logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge has no such repository, so its pull requests are not read until it does",
					append([]slog.Attr{slog.String(logging.RepositoryURL, repository.repositoryURL)}, logging.Err(err)...)...)
			}
		default:
			s.pacing.fail(repository)
			logging.From(ctx).LogAttrs(ctx, slog.LevelError, "failed to sync the repository's pull requests",
				append([]slog.Attr{slog.String(logging.RepositoryURL, repository.repositoryURL)}, logging.Err(err)...)...)
		}
	}
	if !host.throttled {
		s.said.forget(saidRefused, host.host)
	}

	return read
}

// readRepository reads the newest pull requests on each of the repository's branches, writes
// them, marks the repository read, and paces its next read from what the forge answered.
func (s *PullRequestSyncer) readRepository(ctx context.Context, host *hostRead, params dueParams, branches []string) error {
	remote, err := git.ParseRemoteURL(params.repositoryURL)
	if err != nil {
		return err
	}

	reading := &forge.Reading{}
	if len(branches) > 0 {
		reading, err = host.forge.ReadBranches(ctx, remote, branches)
		if err != nil {
			return err
		}
		s.noteRename(ctx, params.repositoryURL, reading.PullRequests)
		s.noteRateLimit(ctx, host, reading.RateLimit)
	}

	written, err := s.write(ctx, reading.PullRequests)
	if err != nil {
		return err
	}
	if err := s.syncs.EnsureSync(ctx, params.repositoryURL); err != nil {
		return err
	}

	s.pacing.read(readParams{
		repositoryURL: params.repositoryURL,
		fingerprint:   params.fingerprint,
		hotUntil:      written.hotUntil,
		askedAt:       params.askedAt,
	})

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "pull request sync",
		slog.String(logging.RepositoryURL, params.repositoryURL),
		slog.Int(logging.Branches, len(branches)),
		slog.Int(logging.RowsInserted, written.inserted),
		slog.Int(logging.RowsUpdated, written.updated),
	)

	return nil
}

// settleHostError stops the host for the pass when the error says its rate limit or its
// credential did, and says whether it was one of those.
func (s *PullRequestSyncer) settleHostError(ctx context.Context, host *hostRead, err error) bool {
	var throttled *forge.ThrottledError
	switch {
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
	until := s.backoff.record(ctx, host.host, host.connection.ID, retryAt)
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
	s.backoff.record(ctx, host.host, host.connection.ID, time.Time{})
	if !s.said.first(saidRefused, host.host) {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge refused the connection's credential, so its pull requests wait for it to be accepted again",
		append([]slog.Attr{slog.String(logging.ForgeHost, host.host)}, logging.Err(err)...)...)
}

// noteRateLimit stops the host for the pass and records the reset on its connection once its
// budget is spent, and keeps it in the slow lane until the reset once the budget runs low.
func (s *PullRequestSyncer) noteRateLimit(ctx context.Context, host *hostRead, limit forge.RateLimit) {
	if limit.Known {
		s.backoff.reset(host.host)
	}
	if limit.IsExhausted() {
		s.throttleHost(ctx, host, limit.ResetAt)

		return
	}
	if !limit.IsLow() {
		return
	}

	until := limit.ResetAt
	if until.IsZero() {
		until = s.now().Add(coldGap)
	}
	s.pacing.slow(host.host, until)
}

// noteRename says once, per repository, that the forge answered it under another name.
func (s *PullRequestSyncer) noteRename(ctx context.Context, repositoryURL string, pullRequests []forge.PullRequest) {
	if len(pullRequests) == 0 {
		return
	}

	answered := pullRequests[0].Repository.Canonical()
	if pullRequests[0].Repository.IsZero() || answered == repositoryURL || !s.said.first(saidRenamed, repositoryURL) {
		return
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "the forge now answers the repository under another name, so its pull requests are stored under that one until the checkout's remote is updated",
		slog.String(logging.RepositoryURL, repositoryURL),
		slog.String(logging.RenamedRepositoryURL, answered))
}

// written is what writing one repository's answers did, and how long they keep it hot.
type written struct {
	inserted int
	updated  int
	hotUntil time.Time
}

// write upserts each pull request with its standing, and an event for each that moved, in one
// transaction.
func (s *PullRequestSyncer) write(ctx context.Context, pullRequests []forge.PullRequest) (*written, error) {
	result := &written{}
	if len(pullRequests) == 0 {
		return result, nil
	}

	readAt := s.now()
	err := s.events.InsertManyTx(ctx, func(tx pgx.Tx) ([]river.JobArgs, error) {
		*result = written{}
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
				result.inserted++
			case outcome.Changed:
				result.updated++
			}

			change, err := st.SetStanding(ctx, pullrequeststore.SetStandingParams{
				ID:       outcome.ID,
				Standing: standingOf(pullRequest),
				ReadAt:   readAt,
			})
			if err != nil {
				return nil, err
			}
			if until := hotUntil(pullRequest, change); until.After(result.hotUntil) {
				result.hotUntil = until
			}

			standingMoved := standingMoves(change)
			if upsert.State != "OPEN" {
				standingMoved = slices.DeleteFunc(standingMoved, func(move *Move) bool {
					return move != nil && move.From == ""
				})
			}
			moved := append([]*Move{Moved(FieldState, outcome.OldState, &upsert.State)}, standingMoved...)

			if change := NewChange(outcome.ID, moved...); change != nil {
				event, err := events.NewEvent(change)
				if err != nil {
					return nil, err
				}
				recorded = append(recorded, *event)
			}
		}

		return recorded, nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// hotUntil is how long a pull request keeps its repository hot: an unfinished one with pending
// checks until a while after its last change, and one whose mergeability the forge has not
// computed until a while after the blank began.
func hotUntil(pullRequest forge.PullRequest, change *pullrequeststore.StandingChange) time.Time {
	if pullRequest.IsFinished() {
		return time.Time{}
	}

	until := time.Time{}
	if xptr.Deref(pullRequest.Checks) == forge.ChecksPending {
		until = pullRequest.UpdatedAt.Add(pendingHotWithin)
	}
	if change.BlankSince != nil {
		if blank := change.BlankSince.Add(blankHotWithin); blank.After(until) {
			until = blank
		}
	}

	return until
}

// standingOf is the standing a pull request was answered with.
func standingOf(pullRequest forge.PullRequest) forge.Standing {
	return forge.Standing{
		Decision:     xptr.Deref(pullRequest.Decision),
		Checks:       xptr.Deref(pullRequest.Checks),
		Mergeability: xptr.Deref(pullRequest.Mergeability),
		HeadSHA:      pullRequest.HeadSHA,
		AutoMerge:    xptr.Deref(pullRequest.AutoMerge),
	}
}

func standingMoves(change *pullrequeststore.StandingChange) []*Move {
	return []*Move{
		Moved(FieldDecision, change.OldDecision, change.KnownDecision),
		checksMoved(change),
		Moved(FieldMergeability, change.OldMergeability, change.KnownMergeability),
		Moved(FieldAutoMerge, autoMergeWord(change.OldAutoMerge), autoMergeWord(change.KnownAutoMerge)),
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

	return &Move{
		Field: FieldChecks,
		From:  *change.OldChecks,
		To:    *change.KnownChecks,
	}
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
