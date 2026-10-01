package pullrequests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/internal/events/eventstest"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/logging"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	forgestore "komrad/internal/store/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/waketest"
	"komrad/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const syncRepository = "https://github.com/harrisoncramer/komrad"

const otherRepository = "https://github.com/harrisoncramer/komrad-forge-fixture"

var syncNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// scriptedForge answers the sync's reads from pull requests a test put on it, records every
// request, and can refuse one.
type scriptedForge struct {
	// pullRequests is what the forge holds, by the path of the repository asked about.
	pullRequests map[string][]forge.PullRequest
	// rateLimit is what every read says of the budget.
	rateLimit forge.RateLimit
	// refuse is answered instead of any request while set.
	refuse error
	// requests is every request in order, spelled "read:<path>:<branches>" or
	// "threads:<number>".
	requests []string
	threads  map[int]*forge.Threads
	// reading runs inside every read, which is how a test makes one take time.
	reading func()
}

func (f *scriptedForge) Kind() string { return forge.KindGitHub }

func (f *scriptedForge) Verify(context.Context) (string, error) { return "harrisoncramer", nil }

func (f *scriptedForge) Get(context.Context, string) (forge.PullRequest, error) {
	return forge.PullRequest{}, errors.New("not scripted")
}

func (f *scriptedForge) List(context.Context, forge.ListParams) (*forge.Page, error) {
	return nil, errors.New("not scripted")
}

func (f *scriptedForge) ReadBranches(_ context.Context, repo git.RemoteURL, branches []string) (*forge.Reading, error) {
	f.requests = append(f.requests, "read:"+repo.Path+":"+strings.Join(branches, ","))
	if f.reading != nil {
		f.reading()
	}
	if f.refuse != nil {
		return nil, f.refuse
	}

	reading := &forge.Reading{RateLimit: f.rateLimit}
	for _, pullRequest := range f.pullRequests[repo.Path] {
		for _, branch := range branches {
			if pullRequest.HeadBranch == branch {
				reading.PullRequests = append(reading.PullRequests, pullRequest)
			}
		}
	}

	return reading, nil
}

func (f *scriptedForge) Merge(context.Context, string) error { return nil }

func (f *scriptedForge) SetDraft(context.Context, string, bool) error { return nil }

func (f *scriptedForge) SetAutoMerge(context.Context, forge.SetAutoMergeParams) error { return nil }

func (f *scriptedForge) ReviewThreads(_ context.Context, _ git.RemoteURL, number int) (*forge.Threads, error) {
	f.requests = append(f.requests, "threads:"+strconv.Itoa(number))
	if f.refuse != nil {
		return nil, f.refuse
	}

	if answer, ok := f.threads[number]; ok {
		return answer, nil
	}

	return &forge.Threads{}, nil
}

type syncFixture struct {
	syncer       *PullRequestSyncer
	forge        *scriptedForge
	connections  *forgestore.ForgeStore
	pullRequests *pullrequeststore.PullRequestStore
	syncs        *pullrequeststore.PullRequestSyncStore
	agents       *agentstore.AgentStore
	events       *eventstest.Queue
	loop         *waketest.Counter
	stores       *storetest.Stores
	workflow     string
	now          time.Time
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	ctx := t.Context()

	stores := storetest.Open(t)
	pool := stores.Pool

	workflows := workflowstore.NewWorkflowStore(pool.GetDB())
	created, err := workflows.Create(ctx, workflowstore.Config{
		Name:     "komrad",
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	connections := forgestore.NewForgeStore(pool.GetDB())
	_, err = connections.Create(ctx, forgestore.CreateParams{
		Kind:       forge.KindGitHub,
		Host:       "github.com",
		Method:     forge.MethodToken,
		Login:      "harrisoncramer",
		VerifiedAt: syncNow,
	})
	require.NoError(t, err)

	scripted := &scriptedForge{pullRequests: map[string][]forge.PullRequest{}}
	f := &syncFixture{
		forge:        scripted,
		connections:  connections,
		pullRequests: pullrequeststore.NewPullRequestStore(pool.GetDB()),
		syncs:        pullrequeststore.NewPullRequestSyncStore(pool.GetDB()),
		agents:       agentstore.NewAgentStore(pool.GetDB()),
		events:       eventstest.NewQueue(t, stores, NewEventHandler()),
		loop:         &waketest.Counter{},
		stores:       stores,
		workflow:     created.ID,
		now:          syncNow,
	}
	f.syncer = NewPullRequestSyncer(NewPullRequestSyncerParams{
		Forges: forge.NewService(forge.NewServiceParams{
			Connections: connections,
			Credentials: forge.NewFileStore(t.TempDir()),
			Builders: map[string]forge.Builder{
				forge.KindGitHub: func(string, forge.Authenticator) forge.Forge {
					return scripted
				},
			},
		}),
		PullRequests: f.pullRequests,
		Syncs:        f.syncs,
		Events:       f.events,
		Loop:         f.loop,
		Now:          func() time.Time { return f.now },
	})

	return f
}

// agentOn puts a live agent on a branch of the sync repository.
func (f *syncFixture) agentOn(t *testing.T, name, branch string) string {
	t.Helper()

	return f.agentIn(t, name, syncRepository, branch)
}

// agentIn puts a live agent on a branch of a repository.
func (f *syncFixture) agentIn(t *testing.T, name, repository, branch string) string {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, f.agents.Create(ctx, agentstore.Agent{
		Name:       name,
		Pane:       "%1",
		State:      agentstore.StateIdle,
		WorkflowID: f.workflow,
	}))
	created, err := f.agents.GetInWorkflow(ctx, f.workflow, name)
	require.NoError(t, err)
	require.NoError(t, f.agents.SetCheckout(ctx, created.ID, agentstore.SetCheckoutParams{RepositoryURL: &repository, Branch: &branch}))

	return created.ID
}

// hold puts pull requests on the sync repository at the forge.
func (f *syncFixture) hold(pullRequests ...forge.PullRequest) {
	f.forge.pullRequests["harrisoncramer/komrad"] = append(f.forge.pullRequests["harrisoncramer/komrad"], pullRequests...)
}

func (f *syncFixture) sync(t *testing.T) {
	t.Helper()
	require.NoError(t, f.syncer.Sync(t.Context()))
}

// pass runs one pass at a later time with the request log cleared.
func (f *syncFixture) pass(t *testing.T, at time.Time) {
	t.Helper()
	f.now = at
	f.forge.requests = nil
	f.sync(t)
}

func (f *syncFixture) getSyncOf(t *testing.T, repository string) *pullrequeststore.Sync {
	t.Helper()
	sync, err := f.syncs.GetSync(t.Context(), repository)
	require.NoError(t, err)

	return sync
}

// numberOf is the number a scripted pull request goes by: the digits its provider id ends in,
// or its length when it ends in none.
func numberOf(providerID string) int {
	digits := strings.TrimLeft(providerID, "_ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if number, err := strconv.Atoi(digits); err == nil {
		return number
	}

	return len(providerID)
}

// pullRequestAt is a forge pull request from the sync repository's branch, last changed then.
func pullRequestAt(providerID, branch string, updatedAt time.Time) forge.PullRequest {
	return pullRequestIn(git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/komrad",
	}, providerID, branch, updatedAt)
}

// pullRequestIn is a forge pull request from a branch of a repository, last changed then, with
// no checks and nothing in the way of merging.
func pullRequestIn(repository git.RemoteURL, providerID, branch string, updatedAt time.Time) forge.PullRequest {
	return forge.PullRequest{
		Forge:        forge.KindGitHub,
		ProviderID:   providerID,
		URL:          repository.Canonical() + "/pull/" + providerID,
		Repository:   repository,
		Head:         repository,
		Number:       numberOf(providerID),
		Title:        providerID,
		HeadBranch:   branch,
		HeadSHA:      scriptedHeadSHA,
		BaseBranch:   "main",
		BaseSHA:      "def",
		State:        "OPEN",
		CreatedAt:    updatedAt.Add(-time.Hour),
		UpdatedAt:    updatedAt,
		Decision:     xptr.Ptr(forge.ReviewDecisionPending),
		Checks:       xptr.Ptr(forge.ChecksNone),
		Mergeability: xptr.Ptr(forge.MergeabilityClean),
	}
}

// standing is the pull request as the forge answers it standing so.
func standing(pullRequest forge.PullRequest, standing forge.Standing) forge.PullRequest {
	pullRequest.Decision = xptr.Ptr(standing.Decision)
	pullRequest.Checks = xptr.Ptr(standing.Checks)
	pullRequest.Mergeability = xptr.Ptr(standing.Mergeability)
	pullRequest.AutoMerge = xptr.Ptr(standing.AutoMerge)
	if standing.HeadSHA != "" {
		pullRequest.HeadSHA = standing.HeadSHA
	}

	return pullRequest
}

// scriptedHeadSHA is the head every scripted pull request carries.
const scriptedHeadSHA = "abc"

func (f *syncFixture) stored(t *testing.T, providerID string) *pullrequeststore.PullRequest {
	t.Helper()
	stored, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", providerID)
	require.NoError(t, err)

	return stored
}

// changes is every pull request change the sync recorded.
func (f *syncFixture) changes(t *testing.T) []Change {
	t.Helper()

	var changes []Change
	for _, event := range f.events.ListEvents() {
		var change Change
		require.NoError(t, json.Unmarshal(event.Payload, &change))
		changes = append(changes, change)
	}

	return changes
}

// TestSyncReadsEveryLiveBranchOnTheFirstPass covers a repository never read before: every
// branch a live agent sits on, less the base branch, is read in one request, what the forge
// answers is written, and the repository is marked read.
func TestSyncReadsEveryLiveBranchOnTheFirstPass(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.agentOn(t, "b", "agent/y")
	f.agentOn(t, "c", "agent/y")
	f.agentOn(t, "d", "main")
	f.hold(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)))

	f.sync(t)

	assert.Equal(t, []string{"read:harrisoncramer/komrad:agent/x,agent/y"}, f.forge.requests)
	assert.Equal(t, "agent/x", f.stored(t, "PR_1").HeadBranch)
	f.getSyncOf(t, syncRepository)
}

// TestSyncMarksARepositoryWithNoBranchesRead covers a repository whose agents all sit on the
// base branch: nothing is asked of the forge, and the repository is still marked read.
func TestSyncMarksARepositoryWithNoBranchesRead(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "main")

	f.sync(t)

	assert.Empty(t, f.forge.requests)
	f.getSyncOf(t, syncRepository)
}

// TestSyncReadsAQuietRepositoryOnceAMinute covers the slow lane: a repository nothing in is
// likely to change is not read again until a minute after its last read.
func TestSyncReadsAQuietRepositoryOnceAMinute(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.hold(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)))
	f.forge.reading = func() { f.now = f.now.Add(2 * time.Second) }
	f.sync(t)

	f.pass(t, syncNow.Add(5*time.Second))
	assert.Empty(t, f.forge.requests)

	f.pass(t, syncNow.Add(58*time.Second))
	assert.Empty(t, f.forge.requests)

	f.pass(t, syncNow.Add(59*time.Second))
	assert.Len(t, f.forge.requests, 1, "a read is paced from when its pass began, and a pass a moment short of the gap still reads")
}

// TestSyncReadsAHotRepositoryEveryFiveSeconds covers what puts a repository in the fast lane
// after a read, and what keeps it out.
func TestSyncReadsAHotRepositoryEveryFiveSeconds(t *testing.T) {
	pending := forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksPending, Mergeability: forge.MergeabilityClean}
	blank := forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksNone, Mergeability: forge.MergeabilityUnknown}

	tests := []struct {
		name      string
		held      forge.PullRequest
		rateLimit forge.RateLimit
		after     func(t *testing.T, f *syncFixture, agentID string)
		wantRead  bool
	}{
		{
			name:     "pending checks on a fresh change",
			held:     standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), pending),
			wantRead: true,
		},
		{
			name: "pending checks on a change older than twenty minutes",
			held: standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-21*time.Minute)), pending),
		},
		{
			name: "pending checks on a finished pull request",
			held: func() forge.PullRequest {
				merged := standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), pending)
				merged.State = "MERGED"
				merged.MergedAt = xptr.Ptr(syncNow.Add(-time.Minute))

				return merged
			}(),
		},
		{
			name:     "a mergeability the forge has not computed",
			held:     standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)), blank),
			wantRead: true,
		},
		{
			name: "a moved head",
			held: pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)),
			after: func(t *testing.T, f *syncFixture, agentID string) {
				t.Helper()
				repository, branch, head := syncRepository, "agent/x", "def"
				require.NoError(t, f.agents.SetCheckout(t.Context(), agentID, agentstore.SetCheckoutParams{
					RepositoryURL: &repository,
					Branch:        &branch,
					HeadSHA:       &head,
				}))
			},
			wantRead: true,
		},
		{
			name: "a write on the host",
			held: pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)),
			after: func(_ *testing.T, f *syncFixture, _ string) {
				f.syncer.Heat("github.com")
			},
			wantRead: true,
		},
		{
			name: "a write on another host",
			held: pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)),
			after: func(_ *testing.T, f *syncFixture, _ string) {
				f.syncer.Heat("gitlab.com")
			},
		},
		{
			name: "pending checks on a host whose budget runs low",
			held: standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)), pending),
			rateLimit: forge.RateLimit{
				Known:     true,
				Limit:     5000,
				Remaining: 999,
				ResetAt:   syncNow.Add(30 * time.Minute),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			agentID := f.agentOn(t, "a", "agent/x")
			f.hold(tt.held)
			f.forge.rateLimit = tt.rateLimit
			f.sync(t)
			f.pass(t, syncNow.Add(time.Minute+time.Second))
			require.Len(t, f.forge.requests, 1)
			if tt.after != nil {
				tt.after(t, f, agentID)
			}

			f.pass(t, syncNow.Add(time.Minute+6*time.Second))

			if tt.wantRead {
				assert.Len(t, f.forge.requests, 1)
			} else {
				assert.Empty(t, f.forge.requests)
			}
		})
	}
}

// TestSyncStopsHurryingABlankThatStaysBlank covers a mergeability the forge never computes,
// which keeps its repository in the fast lane only for two minutes after the blank began.
func TestSyncStopsHurryingABlankThatStaysBlank(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.hold(standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)), forge.Standing{
		Decision:     forge.ReviewDecisionPending,
		Checks:       forge.ChecksNone,
		Mergeability: forge.MergeabilityUnknown,
	}))
	f.sync(t)

	f.pass(t, syncNow.Add(70*time.Second))
	assert.Len(t, f.forge.requests, 1)

	f.pass(t, syncNow.Add(115*time.Second))
	assert.Len(t, f.forge.requests, 1)

	f.pass(t, syncNow.Add(125*time.Second))
	assert.Empty(t, f.forge.requests)
}

// TestSyncWakeReadsEveryRepository covers a wake: every repository is read on the next pass
// whether it was due or not, and the loop is asked to run.
func TestSyncWakeReadsEveryRepository(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.agentIn(t, "b", otherRepository, "agent/y")
	f.sync(t)

	f.syncer.Wake()
	f.pass(t, syncNow.Add(time.Second))

	assert.Equal(t, 1, f.loop.Wakes)
	assert.ElementsMatch(t, []string{
		"read:harrisoncramer/komrad:agent/x",
		"read:harrisoncramer/komrad-forge-fixture:agent/y",
	}, f.forge.requests)

	f.pass(t, syncNow.Add(2*time.Second))
	assert.Empty(t, f.forge.requests, "a wake is spent by the pass it woke")
}

// TestSyncRecordsWhatMovedOnAPullRequest covers the events a read records: a pull request
// first seen standing badly, a standing that moved, the same verdict on a new head, and nothing
// for an answer that did not change.
func TestSyncRecordsWhatMovedOnAPullRequest(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	merged := standing(pullRequestAt("PR_4", "agent/x", syncNow.Add(-4*time.Hour)), forge.Standing{
		Decision:     forge.ReviewDecisionChangesRequested,
		Checks:       forge.ChecksFailing,
		Mergeability: forge.MergeabilityClean,
	})
	merged.State = "MERGED"
	f.hold(
		standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)), forge.Standing{
			Decision:     forge.ReviewDecisionPending,
			Checks:       forge.ChecksPending,
			Mergeability: forge.MergeabilityClean,
		}),
		standing(pullRequestAt("PR_2", "agent/x", syncNow.Add(-2*time.Hour)), forge.Standing{
			Decision:     forge.ReviewDecisionApproved,
			Checks:       forge.ChecksPassing,
			Mergeability: forge.MergeabilityClean,
		}),
		standing(pullRequestAt("PR_3", "agent/x", syncNow.Add(-3*time.Hour)), forge.Standing{
			Decision:     forge.ReviewDecisionChangesRequested,
			Checks:       forge.ChecksFailing,
			Mergeability: forge.MergeabilityConflicting,
		}),
		merged,
	)

	f.sync(t)
	badly := f.stored(t, "PR_3").ID
	firstSeen := Change{ID: badly, Moves: []Move{
		{Field: FieldDecision, To: string(forge.ReviewDecisionChangesRequested)},
		{Field: FieldChecks, To: string(forge.ChecksFailing)},
		{Field: FieldMergeability, To: string(forge.MergeabilityConflicting)},
	}}
	assert.Equal(t, []Change{firstSeen}, f.changes(t), "only the open pull request first seen standing badly moved")
	id := f.stored(t, "PR_1").ID

	f.syncer.Wake()
	f.pass(t, syncNow.Add(time.Minute))
	assert.Equal(t, []Change{firstSeen}, f.changes(t), "an unchanged answer records nothing")

	failing := forge.Standing{Decision: forge.ReviewDecisionPending, Checks: forge.ChecksFailing, Mergeability: forge.MergeabilityClean}
	f.forge.pullRequests["harrisoncramer/komrad"][0] = standing(f.forge.pullRequests["harrisoncramer/komrad"][0], failing)
	f.syncer.Wake()
	f.pass(t, syncNow.Add(2*time.Minute))

	pushed := standing(pullRequestAt("PR_1", "agent/x", syncNow.Add(time.Hour)), failing)
	pushed.HeadSHA = "def"
	f.forge.pullRequests["harrisoncramer/komrad"][0] = pushed
	f.syncer.Wake()
	f.pass(t, syncNow.Add(time.Hour+time.Minute))

	assert.Equal(t, []Change{
		firstSeen,
		{ID: id, Moves: []Move{{
			Field: FieldChecks,
			From:  string(forge.ChecksPending),
			To:    string(forge.ChecksFailing),
		}}},
		{ID: id, Moves: []Move{{
			Field: FieldChecks,
			From:  string(forge.ChecksFailing),
			To:    string(forge.ChecksFailing),
		}}},
	}, f.changes(t))
}

// TestSyncThrottlesTheConnection covers the forge refusing a read for its rate limit: the
// connection waits, the wait doubles on the next refusal, and a permission answer does not
// throttle.
func TestSyncThrottlesTheConnection(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.refuse = &forge.ThrottledError{}
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	require.NoError(t, f.syncer.Sync(ctx))
	assert.Equal(t, 1, strings.Count(logged.String(), "the forge is rate limiting the connection"))

	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(syncNow.Add(time.Minute)))
	assert.Len(t, f.forge.requests, 1)

	f.now = syncNow.Add(30 * time.Second)
	f.sync(t)
	assert.Len(t, f.forge.requests, 1, "nothing is asked while the connection waits")

	f.now = syncNow.Add(2 * time.Minute)
	f.sync(t)
	assert.Len(t, f.forge.requests, 2)
	connection, err = f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	assert.True(t, connection.ThrottledUntil.Equal(f.now.Add(2*time.Minute)), "the wait doubles")

	f.forge.refuse = errors.New("github answered 403 for /graphql: Resource not accessible")
	f.now = syncNow.Add(10 * time.Minute)
	f.sync(t)
	connection, err = f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	assert.True(t, connection.ThrottledUntil.Before(f.now), "a permission answer does not throttle")
}

// TestSyncStopsWhenTheBudgetHeaderIsSpent covers a read whose headers say the budget is gone:
// the answer is written, the reset is recorded on the connection, and the host is left alone
// for the rest of the pass and until the reset.
func TestSyncStopsWhenTheBudgetHeaderIsSpent(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.agentIn(t, "b", otherRepository, "agent/y")
	reset := syncNow.Add(20 * time.Minute)
	f.hold(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute)))
	f.forge.rateLimit = forge.RateLimit{
		Known:     true,
		Limit:     5000,
		Remaining: 0,
		ResetAt:   reset,
	}

	f.sync(t)

	assert.Len(t, f.forge.requests, 1)
	f.stored(t, "PR_1")
	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(reset))
}

// TestSyncSaysAMissingRepositoryOnce covers a repository the forge does not have: it is said
// once as a warning rather than as an error every pass, asked about again a minute later, and
// said again only once it has been found and lost again.
func TestSyncSaysAMissingRepositoryOnce(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.refuse = fmt.Errorf("%w: gone", forge.ErrNotFound)
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	for _, at := range []time.Duration{0, 5 * time.Second, time.Minute} {
		f.now = syncNow.Add(at)
		require.NoError(t, f.syncer.Sync(ctx))
	}

	assert.Len(t, f.forge.requests, 2, "a repository the forge cannot find is asked about again a minute later")
	assert.Equal(t, 1, strings.Count(logged.String(), "the forge has no such repository"))
	assert.NotContains(t, logged.String(), "failed to sync the repository")
	_, err := f.syncs.GetSync(t.Context(), syncRepository)
	require.ErrorIs(t, err, store.ErrNotFound)

	f.forge.refuse = nil
	f.syncer.Wake()
	require.NoError(t, f.syncer.Sync(ctx))
	f.forge.refuse = fmt.Errorf("%w: gone again", forge.ErrNotFound)
	f.syncer.Wake()
	require.NoError(t, f.syncer.Sync(ctx))

	assert.Equal(t, 2, strings.Count(logged.String(), "the forge has no such repository"), "a repository found and lost again is said again")
}

// TestSyncRetriesAFailedReadOnceAMinute covers a read the forge refuses: the repository waits out the
// slow lane before it is asked again, even when its branches had just moved.
func TestSyncRetriesAFailedReadOnceAMinute(t *testing.T) {
	f := newSyncFixture(t)
	agentID := f.agentOn(t, "a", "agent/x")
	f.sync(t)

	repository, branch, head := syncRepository, "agent/x", "def"
	require.NoError(t, f.agents.SetCheckout(t.Context(), agentID, agentstore.SetCheckoutParams{
		RepositoryURL: &repository,
		Branch:        &branch,
		HeadSHA:       &head,
	}))
	f.forge.refuse = errors.New("resource not accessible")
	f.pass(t, syncNow.Add(time.Minute))
	require.Len(t, f.forge.requests, 1)

	f.pass(t, syncNow.Add(time.Minute+5*time.Second))
	assert.Empty(t, f.forge.requests)

	f.pass(t, syncNow.Add(2*time.Minute))
	assert.Len(t, f.forge.requests, 1)
}

// TestSyncSaysARenamedRepositoryOnce covers a read answered under another name: the rows are
// stored as the forge names them and the rename is said once.
func TestSyncSaysARenamedRepositoryOnce(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	renamed := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/komrad-renamed",
	}
	f.hold(pullRequestIn(renamed, "PR_1", "agent/x", syncNow.Add(-time.Hour)))
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	require.NoError(t, f.syncer.Sync(ctx))
	f.syncer.Wake()
	require.NoError(t, f.syncer.Sync(ctx))

	assert.Len(t, f.forge.requests, 2)
	assert.Equal(t, 1, strings.Count(logged.String(), "under another name"))
	assert.Equal(t, renamed.Canonical(), f.stored(t, "PR_1").RepositoryURL)
}

// TestSyncBacksOffARefusedCredential covers the forge refusing the credential: the pass stops
// asking the host, the connection waits the way a rate limit refusal with no time makes it
// wait, the refusal is said once, and it is said again after a later refusal.
func TestSyncBacksOffARefusedCredential(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.agentIn(t, "b", otherRepository, "agent/y")
	f.forge.refuse = fmt.Errorf("%w: bad credentials", forge.ErrUnauthorized)
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	require.NoError(t, f.syncer.Sync(ctx))

	assert.Len(t, f.forge.requests, 1, "the first refusal stops the host for the pass")
	assert.Equal(t, 1, strings.Count(logged.String(), "the forge refused the connection's credential"))
	connection, err := f.connections.GetByHost(ctx, "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(f.now.Add(time.Minute)))

	f.now = f.now.Add(30 * time.Second)
	f.forge.requests = nil
	require.NoError(t, f.syncer.Sync(ctx))
	assert.Empty(t, f.forge.requests, "nothing is asked while the connection waits")

	f.now = f.now.Add(time.Minute)
	require.NoError(t, f.syncer.Sync(ctx))
	assert.Len(t, f.forge.requests, 1, "the host is asked again once the wait is over")
	assert.Equal(t, 1, strings.Count(logged.String(), "the forge refused the connection's credential"), "a refusal in a row is not said again")
	connection, err = f.connections.GetByHost(ctx, "github.com")
	require.NoError(t, err)
	assert.True(t, connection.ThrottledUntil.Equal(f.now.Add(2*time.Minute)), "the wait doubles")

	f.forge.refuse = nil
	f.now = f.now.Add(3 * time.Minute)
	f.forge.requests = nil
	require.NoError(t, f.syncer.Sync(ctx))
	assert.NotEmpty(t, f.forge.requests, "an accepted credential is read again")

	f.forge.refuse = fmt.Errorf("%w: bad credentials", forge.ErrUnauthorized)
	f.now = f.now.Add(time.Minute)
	f.syncer.Wake()
	require.NoError(t, f.syncer.Sync(ctx))
	assert.Equal(t, 2, strings.Count(logged.String(), "the forge refused the connection's credential"), "a later refusal is said again")
	assert.NotContains(t, logged.String(), "rate limiting", "a refused credential is never called a rate limit")
}

// TestSyncSkipsARepositoryWithNoConnection covers a host nobody connected: nothing is asked and
// nothing is written.
func TestSyncSkipsARepositoryWithNoConnection(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	elsewhere := "https://gitlab.com/harrisoncramer/komrad"
	f.agentIn(t, "b", elsewhere, "agent/y")

	f.sync(t)

	assert.Equal(t, []string{"read:harrisoncramer/komrad:agent/x"}, f.forge.requests)
	_, err := f.syncs.GetSync(t.Context(), elsewhere)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncMatchesARemoteSpelledWithCapitals covers a checkout whose remote spells the
// repository with capitals: it is read, and its agent is observed beside the pull request.
func TestSyncMatchesARemoteSpelledWithCapitals(t *testing.T) {
	f := newSyncFixture(t)
	spelled, err := git.ParseRemoteURL("git@github.com:HarrisonCramer/Komrad.git")
	require.NoError(t, err)
	agent := f.agentIn(t, "a", spelled.Canonical(), "agent/x")
	f.forge.pullRequests[spelled.Path] = []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))}
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	require.NoError(t, f.syncer.Sync(ctx))

	f.getSyncOf(t, spelled.Canonical())
	seen, err := f.pullRequests.ListObservations(t.Context(), []string{agent})
	require.NoError(t, err)
	assert.Len(t, seen, 1, "the agent is observed beside its pull request")
	assert.NotContains(t, logged.String(), "under another name")
}
