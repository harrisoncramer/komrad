package pullrequests

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"komrad/internal/events"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/internal/wake"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type eventRecorder interface {
	InsertManyTx(ctx context.Context, build func(tx pgx.Tx) ([]river.JobArgs, error)) error
}

// NewPullRequestSyncerParams is what a pull request syncer is built over.
type NewPullRequestSyncerParams struct {
	Forges       *forge.Service
	PullRequests *pullrequeststore.PullRequestStore
	Syncs        *pullrequeststore.PullRequestSyncStore
	Events       eventRecorder
	// Loop wakes the sync's own loop.
	Loop wake.Waker
	// Now is the clock, time.Now when unset.
	Now func() time.Time
}

// PullRequestSyncer keeps the pull requests on the branches live agents sit on in step with the
// forges, reading each repository again soon when something in it is likely to change.
type PullRequestSyncer struct {
	pullRequests *pullrequeststore.PullRequestStore
	syncs        *pullrequeststore.PullRequestSyncStore
	events       eventRecorder
	loop         wake.Waker
	now          func() time.Time
	hosts        *hostGate
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
		pullRequests: params.PullRequests,
		syncs:        params.Syncs,
		events:       params.Events,
		loop:         params.Loop,
		now:          now,
		hosts:        newHostGate(params.Forges, now),
		pacing:       newPacing(),
		said:         onceLog{},
	}
}

// Wake makes every repository due and wakes the sync's loop to read them.
func (s *PullRequestSyncer) Wake() {
	s.pacing.wake()
	s.loop.Wake()
}

// Heat keeps every repository on the host in the fast lane for a while.
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

	read := false
	for _, host := range s.groupByHost(ctx, due) {
		read = s.syncHost(ctx, host) || read
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

// candidatesOf is every repository live agents sit on, with their branches and the fingerprint
// of those branches and heads.
func candidatesOf(repositories []string, targets []pullrequeststore.Target) []candidate {
	type sitting struct {
		branches    []string
		fingerprint strings.Builder
	}
	byRepository := map[string]*sitting{}
	for _, target := range targets {
		found, ok := byRepository[target.RepositoryURL]
		if !ok {
			found = &sitting{}
			byRepository[target.RepositoryURL] = found
		}
		if !slices.Contains(found.branches, target.Branch) {
			found.branches = append(found.branches, target.Branch)
		}
		found.fingerprint.WriteString(target.Branch + "@" + target.HeadSHA + "\n")
	}

	candidates := make([]candidate, 0, len(repositories))
	for _, repositoryURL := range repositories {
		remote, err := git.ParseRemoteURL(repositoryURL)
		if err != nil || remote.Host == "" {
			continue
		}

		next := candidate{repositoryURL: repositoryURL, remote: remote}
		if found, ok := byRepository[repositoryURL]; ok {
			next.branches = found.branches
			next.fingerprint = found.fingerprint.String()
		}
		candidates = append(candidates, next)
	}

	return candidates
}

// hostRead is one forge host being read in a pass, with its due repositories.
type hostRead struct {
	*forgeHost
	repositories []candidate
}

// groupByHost resolves the connection and forge for each host the repositories live on,
// leaving out the hosts nobody connected and the ones waiting on a rate limit.
func (s *PullRequestSyncer) groupByHost(ctx context.Context, repositories []candidate) []*hostRead {
	hosts := []*hostRead{}
	byHost := map[string]*hostRead{}
	skipped := map[string]bool{}
	for _, repository := range repositories {
		if skipped[repository.remote.Host] {
			continue
		}

		host, ok := byHost[repository.remote.Host]
		if !ok {
			host = s.openHost(ctx, repository.remote.Host, repository.repositoryURL)
			if host == nil {
				skipped[repository.remote.Host] = true

				continue
			}

			byHost[repository.remote.Host] = host
			hosts = append(hosts, host)
		}

		host.repositories = append(host.repositories, repository)
	}

	return hosts
}

// openHost is the host with its connection and forge, nil when it cannot be read this pass.
func (s *PullRequestSyncer) openHost(ctx context.Context, hostname, repositoryURL string) *hostRead {
	opened, unconnected := s.hosts.open(ctx, hostname)
	if unconnected && s.said.first(saidUnconnected, repositoryURL) {
		logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "no forge connection for the repository, so its pull requests are not read",
			slog.String(logging.ForgeHost, hostname),
			slog.String(logging.RepositoryURL, repositoryURL))
	}
	if opened == nil {
		return nil
	}

	return &hostRead{forgeHost: opened}
}

// syncHost reads each of the host's due repositories until the host's rate limit or credential
// stops it, and says whether it read any.
func (s *PullRequestSyncer) syncHost(ctx context.Context, host *hostRead) bool {
	read := false
	for _, repository := range host.repositories {
		if host.throttled {
			break
		}

		err := s.readRepository(ctx, host, repository)
		switch {
		case err == nil:
			read = true
			s.said.forget(saidMissing, repository.repositoryURL)
		case s.hosts.settle(ctx, host.forgeHost, err):
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
		s.hosts.forgetRefusal(host.forgeHost)
	}

	return read
}

// readRepository reads the newest pull requests on each of the repository's branches, writes
// them, marks the repository read, and paces its next read from what the forge answered.
func (s *PullRequestSyncer) readRepository(ctx context.Context, host *hostRead, repository candidate) error {
	reading := &forge.Reading{}
	if len(repository.branches) > 0 {
		var err error
		reading, err = host.forge.ReadBranches(ctx, repository.remote, repository.branches)
		if err != nil {
			return fmt.Errorf("failed to read the pull requests on %d branches: %w", len(repository.branches), err)
		}
		s.noteRename(ctx, repository.repositoryURL, reading.PullRequests)
		s.noteRateLimit(ctx, host, reading.RateLimit)
	}

	written, err := s.write(ctx, reading.PullRequests)
	if err != nil {
		return err
	}
	if err := s.syncs.EnsureSync(ctx, repository.repositoryURL); err != nil {
		return err
	}

	s.pacing.read(repository, written.hotUntil)

	logging.From(ctx).LogAttrs(ctx, slog.LevelDebug, "pull request sync",
		slog.String(logging.RepositoryURL, repository.repositoryURL),
		slog.Int(logging.Branches, len(repository.branches)),
		slog.Int(logging.RowsInserted, written.inserted),
		slog.Int(logging.RowsUpdated, written.updated),
	)

	return nil
}

// noteRateLimit stops the host for the pass and records the reset on its connection once its
// budget is spent, and keeps it in the slow lane until the reset once the budget runs low.
func (s *PullRequestSyncer) noteRateLimit(ctx context.Context, host *hostRead, limit forge.RateLimit) {
	if limit.Known {
		s.hosts.resetBackOff(host.forgeHost)
	}
	if limit.IsExhausted() {
		s.hosts.throttle(ctx, host.forgeHost, limit.ResetAt)

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

			change := &pullrequeststore.StandingChange{}
			if pullRequest.Standing != nil {
				change, err = st.SetStanding(ctx, pullrequeststore.SetStandingParams{
					ID:       outcome.ID,
					Standing: *pullRequest.Standing,
					HeadSHA:  pullRequest.HeadSHA,
					ReadAt:   readAt,
				})
				if err != nil {
					return nil, err
				}
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
