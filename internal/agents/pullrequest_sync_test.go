package agents

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	forgestore "weave/internal/store/forge"
	pullrequeststore "weave/internal/store/pullrequest"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const syncRepository = "https://github.com/harrisoncramer/weave"

var syncNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// scriptedForge answers the sync's two listings from pages a test wrote, records every request,
// and can refuse one.
type scriptedForge struct {
	updated  map[int]*forge.ListUpdatedResult
	branches map[string]map[int]*forge.ListResult
	// decisions is what the forge says about each pull request's reviews, by number, and
	// decisionErrors what it refuses instead.
	decisions      map[int]forge.ReviewDecision
	decisionErrors map[int]error
	// refuse is answered instead of any page while set.
	refuse error
	// requests is every request in order, spelled "updated:<page>:<etag>",
	// "branch:<name>:<page>" or "decision:<number>".
	requests []string
}

func (f *scriptedForge) Kind() string { return forge.KindGitHub }

func (f *scriptedForge) Verify(context.Context) (string, error) { return "harrisoncramer", nil }

func (f *scriptedForge) Get(context.Context, string) (forge.PullRequest, error) {
	return forge.PullRequest{}, errors.New("not scripted")
}

func (f *scriptedForge) ListUpdated(_ context.Context, params forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	f.requests = append(f.requests, fmt.Sprintf("updated:%d:%s", params.Page, params.ETag))
	if f.refuse != nil {
		return nil, f.refuse
	}

	page, ok := f.updated[params.Page]
	if !ok {
		return &forge.ListUpdatedResult{}, nil
	}
	if params.ETag != "" && params.ETag == page.ETag {
		return &forge.ListUpdatedResult{
			ETag:        page.ETag,
			NotModified: true,
			RateLimit:   page.RateLimit,
		}, nil
	}

	copied := *page

	return &copied, nil
}

func (f *scriptedForge) GetReviewDecisionByNumber(_ context.Context, _ git.RemoteURL, pullRequestNumber int) (forge.ReviewDecision, error) {
	f.requests = append(f.requests, fmt.Sprintf("decision:%d", pullRequestNumber))
	if f.refuse != nil {
		return forge.ReviewDecisionNone, f.refuse
	}
	if err := f.decisionErrors[pullRequestNumber]; err != nil {
		return forge.ReviewDecisionNone, err
	}

	return f.decisions[pullRequestNumber], nil
}

func (f *scriptedForge) List(_ context.Context, params forge.ListParams) (*forge.ListResult, error) {
	f.requests = append(f.requests, fmt.Sprintf("branch:%s:%d", params.Branch, params.Page))
	if f.refuse != nil {
		return nil, f.refuse
	}

	page, ok := f.branches[params.Branch][params.Page]
	if !ok {
		return &forge.ListResult{}, nil
	}

	copied := *page

	return &copied, nil
}

// scriptedFinder answers the connection from the real store and always the one scripted forge.
type scriptedFinder struct {
	connections *forgestore.Store
	forge       *scriptedForge
}

func (f *scriptedFinder) ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error) {
	return f.connections.GetByHost(ctx, host)
}

func (f *scriptedFinder) ForConnection(forgestore.Connection) (forge.Forge, error) {
	return f.forge, nil
}

func (f *scriptedFinder) SetThrottledUntil(ctx context.Context, id string, until *time.Time) error {
	return f.connections.SetThrottledUntil(ctx, id, until)
}

type syncFixture struct {
	syncer       *PullRequestSyncer
	forge        *scriptedForge
	connections  *forgestore.Store
	pullRequests *pullrequeststore.Store
	agents       *agentstore.Store
	workflow     string
	now          time.Time
}

func newSyncFixture(t *testing.T) *syncFixture {
	t.Helper()
	ctx := t.Context()

	pool, err := store.Open(ctx, dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	workflows := workflowstore.New(pool.GetDB())
	created, err := workflows.Create(ctx, workflowstore.Config{
		Name:     "weave",
		Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	})
	require.NoError(t, err)

	connections := forgestore.New(pool.GetDB())
	_, err = connections.Create(ctx, forgestore.CreateParams{
		Kind:       forge.KindGitHub,
		Host:       "github.com",
		Method:     forge.MethodToken,
		Login:      "harrisoncramer",
		VerifiedAt: syncNow,
	})
	require.NoError(t, err)

	scripted := &scriptedForge{
		updated:        map[int]*forge.ListUpdatedResult{},
		branches:       map[string]map[int]*forge.ListResult{},
		decisions:      map[int]forge.ReviewDecision{},
		decisionErrors: map[int]error{},
	}
	f := &syncFixture{
		forge:        scripted,
		connections:  connections,
		pullRequests: pullrequeststore.New(pool.GetDB()),
		agents:       agentstore.New(pool.GetDB()),
		workflow:     created.ID,
		now:          syncNow,
	}
	f.syncer = NewPullRequestSyncer(NewPullRequestSyncerParams{
		Forges:       &scriptedFinder{connections: connections, forge: scripted},
		PullRequests: f.pullRequests,
		Tx:           pool,
		Now:          func() time.Time { return f.now },
	})

	return f
}

// agentOn puts a live agent on a branch of the sync repository.
func (f *syncFixture) agentOn(t *testing.T, name, branch string) string {
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
	repository := syncRepository
	require.NoError(t, f.agents.SetRepositoryURLByID(ctx, created.ID, &repository))
	require.NoError(t, f.agents.SetBranchByID(ctx, created.ID, &branch))

	return created.ID
}

func (f *syncFixture) sync(t *testing.T) {
	t.Helper()
	require.NoError(t, f.syncer.Sync(t.Context()))
}

func (f *syncFixture) getSync(t *testing.T) *pullrequeststore.Sync {
	t.Helper()
	sync, err := f.pullRequests.GetSync(t.Context(), syncRepository)
	require.NoError(t, err)

	return sync
}

// numberOf is the number a scripted pull request goes by: the digits its provider id ends in,
// or its length when it ends in none.
func numberOf(providerID string) int {
	digits := strings.TrimLeft(providerID, "PR_ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	if number, err := strconv.Atoi(digits); err == nil {
		return number
	}

	return len(providerID)
}

// pullRequestAt is a forge pull request from the sync repository's branch, last changed then.
func pullRequestAt(providerID, branch string, updatedAt time.Time) forge.PullRequest {
	repository := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave",
	}

	return forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: providerID,
		URL:        "https://github.com/harrisoncramer/weave/pull/" + providerID,
		Repository: repository,
		Head:       repository,
		Number:     numberOf(providerID),
		Title:      providerID,
		HeadBranch: branch,
		HeadSHA:    "abc",
		BaseBranch: "main",
		BaseSHA:    "def",
		State:      "open",
		CreatedAt:  updatedAt.Add(-time.Hour),
		UpdatedAt:  updatedAt,
	}
}

func (f *syncFixture) stored(t *testing.T, providerID string) *pullrequeststore.PullRequest {
	t.Helper()
	stored, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", providerID)
	require.NoError(t, err)

	return stored
}

// TestSyncFirstSweepStopsAtTheHorizon covers a repository never read before: page one is read
// without an ETag, every row is written, a row older than the horizon ends the sweep on that
// page, and the page that stopped is the one that promoted the mark.
func TestSyncFirstSweepStopsAtTheHorizon(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{
			pullRequestAt("PR_3", "agent/x", syncNow.Add(-time.Hour)),
			pullRequestAt("PR_2", "agent/y", syncNow.Add(-2*time.Hour)),
			pullRequestAt("PR_1", "agent/z", syncNow.Add(-40*24*time.Hour)),
		},
		HasMore: true,
		ETag:    `"one"`,
	}
	f.forge.updated[2] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{pullRequestAt("PR_0", "old", syncNow.Add(-50*24*time.Hour))}}

	f.sync(t)

	assert.Equal(t, []string{"updated:1:", "branch:agent/x:1", "decision:3"}, f.forge.requests, "page two is never asked for, the agent's branch is scanned, and only its open pull request costs a decision")
	sync := f.getSync(t)
	assert.False(t, sync.IsSweeping())
	require.NotNil(t, sync.UpdatedThrough)
	assert.True(t, sync.UpdatedThrough.Equal(syncNow.Add(-time.Hour)), "the mark is page one's newest stamp")
	assert.Equal(t, `"one"`, sync.ETag)
	assert.NotNil(t, sync.ValidatedAt)
	assert.Equal(t, 3, sync.Calls)
	f.stored(t, "PR_1")
	_, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", "PR_0")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncNotModifiedShortCircuits covers the ETag round trip: page one is asked with the ETag
// the last sweep stored, a 304 costs one request and writes nothing, and the validation clock
// is left where the unconditional read put it.
func TestSyncNotModifiedShortCircuits(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))},
		ETag:         `"one"`,
	}
	f.sync(t)
	validated := f.getSync(t).ValidatedAt
	require.NotNil(t, validated)

	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`}, f.forge.requests, "the scanned branch is not asked again")
	sync := f.getSync(t)
	assert.Equal(t, 1, sync.Calls)
	assert.True(t, sync.ValidatedAt.Equal(*validated), "a 304 does not validate")
	assert.False(t, sync.IsSweeping())
}

// TestSyncReadsUnconditionallyAfterTheBackstop covers the hourly re-read behind an unchanged
// ETag, on its own clock.
func TestSyncReadsUnconditionallyAfterTheBackstop(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))},
		ETag:         `"one"`,
	}
	f.sync(t)
	validated := *f.getSync(t).ValidatedAt

	f.now = syncNow.Add(2 * time.Hour)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{"updated:1:"}, f.forge.requests, "no ETag once the backstop is due")
	assert.True(t, f.getSync(t).ValidatedAt.After(validated))
}

// TestSyncIncrementalSweepStopsAtTheMarkLessTheOverlap covers the second sweep: a changed page
// one is read down to fifteen minutes below the mark, so a row sharing the mark's second is
// written and a row below the overlap ends the sweep.
func TestSyncIncrementalSweepStopsAtTheMarkLessTheOverlap(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	mark := syncNow.Add(-time.Hour)
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", mark)},
		ETag:         `"one"`,
	}
	f.sync(t)

	f.now = syncNow.Add(time.Minute)
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{
			pullRequestAt("PR_3", "agent/x", syncNow),
			pullRequestAt("PR_2", "agent/x", mark),
			pullRequestAt("PR_1", "agent/x", mark.Add(-20*time.Minute)),
		},
		HasMore: true,
		ETag:    `"two"`,
	}
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`, "decision:3", "decision:2"}, f.forge.requests, "the two new pull requests cost a decision each and the re-read one does not")
	f.stored(t, "PR_2")
	f.stored(t, "PR_3")
	sync := f.getSync(t)
	assert.False(t, sync.IsSweeping())
	assert.True(t, sync.UpdatedThrough.Equal(syncNow), "the mark moved to the new page one's newest")
	assert.Equal(t, `"two"`, sync.ETag)
}

// TestSyncBudgetPausesAndResumes covers a sweep longer than one pass: ten pages are read, the
// resume position and page one's stamp and ETag are held, the mark does not move, and the next
// pass carries on from page eleven and completes.
func TestSyncBudgetPausesAndResumes(t *testing.T) {
	f := newSyncFixture(t)
	for page := 1; page <= 12; page++ {
		f.forge.updated[page] = &forge.ListUpdatedResult{
			PullRequests: []forge.PullRequest{pullRequestAt(fmt.Sprintf("PR_%d", page), "agent/x", syncNow.Add(-time.Duration(page)*time.Minute))},
			HasMore:      page < 12,
			ETag:         fmt.Sprintf(`"%d"`, page),
		}
	}
	f.agentOn(t, "a", "agent/x")
	f.forge.branches["agent/x"] = map[int]*forge.ListResult{1: {}}

	f.sync(t)

	sync := f.getSync(t)
	require.True(t, sync.IsSweeping())
	assert.Equal(t, 11, *sync.SweepNextPage)
	assert.Nil(t, sync.UpdatedThrough)
	assert.True(t, sync.SweepNewest.Equal(syncNow.Add(-time.Minute)))
	assert.Equal(t, `"1"`, *sync.SweepETag)
	assert.Equal(t, 10, sync.Calls)
	assert.Len(t, f.forge.requests, 10, "the branch scan waits for the sweep")

	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{
		"updated:11:", "updated:12:", "branch:agent/x:1",
		"decision:1", "decision:2", "decision:3", "decision:4", "decision:5", "decision:6", "decision:7",
	}, f.forge.requests, "the decisions take what is left of the budget, newest change first")
	sync = f.getSync(t)
	assert.False(t, sync.IsSweeping())
	assert.True(t, sync.UpdatedThrough.Equal(syncNow.Add(-time.Minute)))
	assert.Equal(t, `"1"`, sync.ETag)
	f.stored(t, "PR_12")
}

// TestSyncFailedPageRollsBackAndStillCounts covers a page whose write fails: the resume
// position stays on it, nothing from it is stored, the request was still counted, and the next
// pass reads it again.
func TestSyncFailedPageRollsBackAndStillCounts(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	colliding := pullRequestAt("PR_9", "agent/x", syncNow.Add(-2*time.Minute))
	colliding.URL = "https://github.com/harrisoncramer/weave/pull/PR_1"
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))},
		HasMore:      true,
		ETag:         `"one"`,
	}
	f.forge.updated[2] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_2", "agent/x", syncNow.Add(-90*time.Second)), colliding},
	}

	f.sync(t)

	sync := f.getSync(t)
	require.True(t, sync.IsSweeping())
	assert.Equal(t, 2, *sync.SweepNextPage)
	assert.Equal(t, 2, sync.Calls)
	f.stored(t, "PR_1")
	_, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", "PR_2")
	require.ErrorIs(t, err, store.ErrNotFound, "the page that failed left nothing behind")

	f.forge.updated[2].PullRequests = f.forge.updated[2].PullRequests[:1]
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, "updated:2:", f.forge.requests[0])
	f.stored(t, "PR_2")
	assert.False(t, f.getSync(t).IsSweeping())
}

// TestSyncScansANewBranchOnceAndAttributesLocally covers the branch scan and the attribution
// that follows it: a branch a live agent sits on is listed to completion once, its pull request
// is attributed to that agent, and a second agent adopting the branch later is attributed with
// no forge call at all.
func TestSyncScansANewBranchOnceAndAttributesLocally(t *testing.T) {
	f := newSyncFixture(t)
	first := f.agentOn(t, "a", "agent/old")
	f.forge.updated[1] = &forge.ListUpdatedResult{ETag: `"one"`}
	old := pullRequestAt("PR_OLD", "agent/old", syncNow.Add(-60*24*time.Hour))
	f.forge.branches["agent/old"] = map[int]*forge.ListResult{
		1: {PullRequests: []forge.PullRequest{old}, HasMore: true},
		2: {PullRequests: []forge.PullRequest{pullRequestAt("PR_OLDER", "agent/old", syncNow.Add(-70*24*time.Hour))}},
	}

	f.sync(t)

	assert.Equal(t, []string{"updated:1:", "branch:agent/old:1", "branch:agent/old:2", "decision:6", "decision:8"}, f.forge.requests)
	scan, err := f.pullRequests.GetBranchScan(t.Context(), syncRepository, "agent/old")
	require.NoError(t, err)
	assert.NotNil(t, scan.CompletedAt)
	seen, err := f.pullRequests.ListObservations(t.Context(), first)
	require.NoError(t, err)
	assert.Len(t, seen, 2, "the agent is seen beside both pull requests from its branch")

	second := f.agentOn(t, "b", "agent/old")
	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`}, f.forge.requests, "the branch is not scanned again")
	seen, err = f.pullRequests.ListObservations(t.Context(), second)
	require.NoError(t, err)
	assert.Len(t, seen, 2, "the second agent is attributed from what is stored")
}

// TestSyncThrottlesTheConnection covers a rate limit refusal that named no time: the connection
// waits a minute, then two after a second refusal in a row, and no request is made while it
// waits; a bare permission answer throttles nothing.
func TestSyncThrottlesTheConnection(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.refuse = &forge.ThrottledError{}

	f.sync(t)

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

	f.forge.refuse = errors.New("github answered 403 for /repos/harrisoncramer/weave/pulls: Resource not accessible")
	f.now = syncNow.Add(10 * time.Minute)
	f.sync(t)
	connection, err = f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	assert.True(t, connection.ThrottledUntil.Before(f.now), "a permission answer does not throttle")
}

// TestSyncStopsWhenTheBudgetHeaderIsSpent covers a success whose headers say the budget is gone:
// the page is written, the reset is recorded on the connection, and the host is left alone for
// the rest of the pass and until the reset.
func TestSyncStopsWhenTheBudgetHeaderIsSpent(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	reset := syncNow.Add(20 * time.Minute)
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))},
		HasMore:      true,
		ETag:         `"one"`,
		RateLimit: forge.RateLimit{
			Known:     true,
			Remaining: 0,
			ResetAt:   reset,
		},
	}

	f.sync(t)

	assert.Equal(t, []string{"updated:1:"}, f.forge.requests)
	f.stored(t, "PR_1")
	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(reset))
	assert.True(t, f.getSync(t).IsSweeping())
}

// TestSyncSkipsARepositoryWithNoConnection covers a host nobody connected: nothing is asked and
// nothing is written.
func TestSyncSkipsARepositoryWithNoConnection(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	elsewhere := "https://gitlab.com/harrisoncramer/weave"
	agent := f.agentOn(t, "b", "agent/y")
	require.NoError(t, f.agents.SetRepositoryURLByID(t.Context(), agent, &elsewhere))
	f.forge.updated[1] = &forge.ListUpdatedResult{ETag: `"one"`}

	f.sync(t)
	f.sync(t)

	for _, request := range f.forge.requests {
		assert.NotContains(t, request, "gitlab")
	}
	_, err := f.pullRequests.GetSync(t.Context(), elsewhere)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncReadsTheDecisionOfAChangedOpenPullRequest covers which pull requests cost a decision
// read: an open one on a live branch when it lands, and again only once it changes; never a
// draft, a finished one, or one on a branch no live agent sits on.
func TestSyncReadsTheDecisionOfAChangedOpenPullRequest(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	open := pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))
	draft := pullRequestAt("PR_2", "agent/x", syncNow.Add(-2*time.Hour))
	draft.Draft = true
	merged := pullRequestAt("PR_3", "agent/x", syncNow.Add(-3*time.Hour))
	mergedAt := syncNow.Add(-3 * time.Hour)
	merged.MergedAt = &mergedAt
	merged.ClosedAt = &mergedAt
	elsewhere := pullRequestAt("PR_4", "agent/nobody", syncNow.Add(-4*time.Hour))
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{open, draft, merged, elsewhere},
		ETag:         `"one"`,
	}
	f.forge.decisions[1] = forge.ReviewDecisionApproved

	f.sync(t)

	assert.Equal(t, []string{"updated:1:", "branch:agent/x:1", "decision:1"}, f.forge.requests)
	stored := f.stored(t, "PR_1")
	require.NotNil(t, stored.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionApproved, *stored.ReviewDecision)
	assert.NotNil(t, stored.DecisionReadAt)
	for _, providerID := range []string{"PR_2", "PR_3", "PR_4"} {
		assert.Nil(t, f.stored(t, providerID).DecisionReadAt, "%s costs no decision read", providerID)
	}

	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`}, f.forge.requests, "a decision read since the last change is not read again")

	f.now = syncNow.Add(2 * time.Minute)
	changed := open
	changed.UpdatedAt = time.Now().Add(time.Hour)
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{changed},
		ETag:         `"two"`,
	}
	f.forge.decisions[1] = forge.ReviewDecisionChangesRequested
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`, "decision:1"}, f.forge.requests, "a change to the pull request makes its decision stale")
	stored = f.stored(t, "PR_1")
	require.NotNil(t, stored.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionChangesRequested, *stored.ReviewDecision)
}

// TestSyncDecisionsWaitForTheBudget covers more stale decisions than the budget has room for:
// the newest changes are read first, the rest stay stale, and the next pass reads them.
func TestSyncDecisionsWaitForTheBudget(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	var pullRequests []forge.PullRequest
	for number := 1; number <= 12; number++ {
		pullRequests = append(pullRequests, pullRequestAt(fmt.Sprintf("PR_%d", number), "agent/x", syncNow.Add(-time.Duration(number)*time.Minute)))
	}
	f.forge.updated[1] = &forge.ListUpdatedResult{PullRequests: pullRequests, ETag: `"one"`}

	f.sync(t)

	assert.Equal(t, []string{
		"updated:1:", "branch:agent/x:1",
		"decision:1", "decision:2", "decision:3", "decision:4", "decision:5", "decision:6", "decision:7", "decision:8",
	}, f.forge.requests)
	assert.Equal(t, 10, f.getSync(t).Calls)
	assert.Nil(t, f.stored(t, "PR_9").DecisionReadAt)

	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`, "decision:9", "decision:10", "decision:11", "decision:12"}, f.forge.requests)
	assert.NotNil(t, f.stored(t, "PR_12").DecisionReadAt)
}

// TestSyncDecisionTheForgeDoesNotHaveIsRead covers a pull request GitHub answers not found
// for, which is stamped as read with nothing said rather than asked for again every pass.
func TestSyncDecisionTheForgeDoesNotHaveIsRead(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))},
		ETag:         `"one"`,
	}
	f.forge.decisionErrors[1] = forge.ErrNotFound

	f.sync(t)

	stored := f.stored(t, "PR_1")
	assert.Nil(t, stored.ReviewDecision)
	assert.NotNil(t, stored.DecisionReadAt)

	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`}, f.forge.requests)
}

// TestSyncDecisionRefusalStopsTheRepository covers any other refusal, which is reported and
// leaves the decision stale for the next pass.
func TestSyncDecisionRefusalStopsTheRepository(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[1] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))},
		ETag:         `"one"`,
	}
	f.forge.decisionErrors[1] = errors.New("github answered 502")

	f.sync(t)

	assert.Nil(t, f.stored(t, "PR_1").DecisionReadAt)
	assert.Equal(t, 3, f.getSync(t).Calls, "the refused request is still counted")

	delete(f.forge.decisionErrors, 1)
	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
	f.sync(t)

	assert.Equal(t, []string{`updated:1:"one"`, "decision:1"}, f.forge.requests)
	assert.NotNil(t, f.stored(t, "PR_1").DecisionReadAt)
}
