package agents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/forge"
	"weave/internal/git"
	"weave/internal/logging"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	forgestore "weave/internal/store/forge"
	pullrequeststore "weave/internal/store/pullrequest"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const syncRepository = "https://github.com/harrisoncramer/weave"

const otherRepository = "https://github.com/harrisoncramer/weave-forge-fixture"

var syncNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

// scriptedForge answers the sync's listings from pages a test wrote, records every request,
// and can refuse one.
type scriptedForge struct {
	// updated is the sync repository's listing by cursor, and elsewhere the same for any
	// other repository by path.
	updated   map[string]*forge.ListUpdatedResult
	elsewhere map[string]map[string]*forge.ListUpdatedResult
	// branches is each branch's listing by cursor.
	branches map[string]map[string]*forge.ListResult
	// changed is the search by cursor.
	changed map[string]*forge.ListChangedResult
	// decisions is what the forge says about each pull request's reviews, by number, none
	// when a number is not here, and unknown the numbers the forge has no pull request for.
	decisions map[int]forge.ReviewDecision
	unknown   map[int]bool
	// refuse is answered instead of any listing while set, refuseUpdated instead of the
	// repository's own listing alone, and refuseDecisions instead of any batch of decisions.
	refuse          error
	refuseUpdated   error
	refuseDecisions error
	// requests is every request in order, spelled "updated:<cursor>", "branch:<name>:<cursor>",
	// "changed:<since>:<cursor>" or "decisions:<numbers>".
	requests []string
	// searches is the parameters of every search, in order.
	searches []forge.ListChangedParams
}

func (f *scriptedForge) Kind() string { return forge.KindGitHub }

func (f *scriptedForge) Verify(context.Context) (string, error) { return "harrisoncramer", nil }

func (f *scriptedForge) Get(context.Context, string) (forge.PullRequest, error) {
	return forge.PullRequest{}, errors.New("not scripted")
}

func (f *scriptedForge) ListUpdated(_ context.Context, params forge.ListUpdatedParams) (*forge.ListUpdatedResult, error) {
	f.requests = append(f.requests, "updated:"+params.Cursor)
	if f.refuse != nil {
		return nil, f.refuse
	}
	if f.refuseUpdated != nil {
		return nil, f.refuseUpdated
	}

	pages := f.updated
	if elsewhere, ok := f.elsewhere[params.Repo.Path]; ok {
		pages = elsewhere
	}
	page, ok := pages[params.Cursor]
	if !ok {
		return &forge.ListUpdatedResult{}, nil
	}

	copied := *page

	return &copied, nil
}

func (f *scriptedForge) ListChanged(_ context.Context, params forge.ListChangedParams) (*forge.ListChangedResult, error) {
	f.requests = append(f.requests, "changed:"+params.Since.UTC().Format(time.RFC3339)+":"+params.Cursor)
	f.searches = append(f.searches, params)
	if f.refuse != nil {
		return nil, f.refuse
	}

	page, ok := f.changed[params.Cursor]
	if !ok {
		return &forge.ListChangedResult{}, nil
	}

	copied := *page

	return &copied, nil
}

func (f *scriptedForge) ReviewDecisions(_ context.Context, _ git.RemoteURL, numbers []int) (map[int]forge.ReviewDecision, error) {
	spelled := make([]string, 0, len(numbers))
	for _, number := range numbers {
		spelled = append(spelled, strconv.Itoa(number))
	}
	f.requests = append(f.requests, "decisions:"+strings.Join(spelled, ","))
	if f.refuse != nil {
		return nil, f.refuse
	}
	if f.refuseDecisions != nil {
		return nil, f.refuseDecisions
	}

	answered := map[int]forge.ReviewDecision{}
	for _, number := range numbers {
		if f.unknown[number] {
			continue
		}

		answered[number] = f.decisions[number]
	}

	return answered, nil
}

func (f *scriptedForge) List(_ context.Context, params forge.ListParams) (*forge.ListResult, error) {
	f.requests = append(f.requests, "branch:"+params.Branch+":"+params.Cursor)
	if f.refuse != nil {
		return nil, f.refuse
	}

	page, ok := f.branches[params.Branch][params.Cursor]
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
		updated:   map[string]*forge.ListUpdatedResult{},
		elsewhere: map[string]map[string]*forge.ListUpdatedResult{},
		branches:  map[string]map[string]*forge.ListResult{},
		changed:   map[string]*forge.ListChangedResult{},
		decisions: map[int]forge.ReviewDecision{},
		unknown:   map[int]bool{},
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
	require.NoError(t, f.agents.SetRepositoryURLByID(ctx, created.ID, &repository))
	require.NoError(t, f.agents.SetBranchByID(ctx, created.ID, &branch))

	return created.ID
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

func (f *syncFixture) getSync(t *testing.T) *pullrequeststore.Sync {
	t.Helper()

	return f.getSyncOf(t, syncRepository)
}

func (f *syncFixture) getSyncOf(t *testing.T, repository string) *pullrequeststore.Sync {
	t.Helper()
	sync, err := f.pullRequests.GetSync(t.Context(), repository)
	require.NoError(t, err)

	return sync
}

// search is the request a search made from a mark, less the overlap, on its first page.
func search(mark time.Time) string {
	return "changed:" + mark.Add(-pullRequestSyncOverlap).UTC().Format(time.RFC3339) + ":"
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
		Path:   "harrisoncramer/weave",
	}, providerID, branch, updatedAt)
}

// pullRequestIn is a forge pull request from a branch of a repository, last changed then.
func pullRequestIn(repository git.RemoteURL, providerID, branch string, updatedAt time.Time) forge.PullRequest {
	return forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: providerID,
		URL:        repository.Canonical() + "/pull/" + providerID,
		Repository: repository,
		Head:       repository,
		Number:     numberOf(providerID),
		Title:      providerID,
		HeadBranch: branch,
		HeadSHA:    "abc",
		BaseBranch: "main",
		BaseSHA:    "def",
		State:      "OPEN",
		CreatedAt:  updatedAt.Add(-time.Hour),
		UpdatedAt:  updatedAt,
	}
}

// decided is the pull request as a search answers it, carrying its decision.
func decided(pullRequest forge.PullRequest, decision forge.ReviewDecision) forge.PullRequest {
	pullRequest.Decision = xptr.Ptr(decision)

	return pullRequest
}

func (f *syncFixture) stored(t *testing.T, providerID string) *pullrequeststore.PullRequest {
	t.Helper()
	stored, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", providerID)
	require.NoError(t, err)

	return stored
}

// sweptFixture is a fixture past its first sweep at syncNow: one agent on agent/x, one open
// pull request PR_1 from that branch changed an hour before, its decision read, and the mark
// at syncNow, where the pass's empty search left it.
func sweptFixture(t *testing.T) (*syncFixture, time.Time) {
	t.Helper()

	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	changed := syncNow.Add(-time.Hour)
	f.forge.updated[""] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", changed)},
	}
	f.forge.decisions[1] = forge.ReviewDecisionPending
	f.sync(t)

	require.Equal(t, []string{"updated:", search(syncNow), "branch:agent/x:", "decisions:1"}, f.forge.requests)
	require.True(t, f.getSync(t).UpdatedThrough.Equal(syncNow))

	return f, syncNow
}

// TestSyncFirstSweepStopsAtTheHorizon covers a repository never read before: page one is
// read, every row is written, a row older than the horizon ends the sweep on that page, the
// page that stopped is the one that promoted the mark to the moment page one was read, and the
// repository joins the search in the same pass from that mark.
func TestSyncFirstSweepStopsAtTheHorizon(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	newest := syncNow.Add(-time.Hour)
	f.forge.updated[""] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{
			pullRequestAt("PR_3", "agent/x", newest),
			pullRequestAt("PR_2", "agent/y", syncNow.Add(-2*time.Hour)),
			pullRequestAt("PR_1", "agent/z", syncNow.Add(-40*24*time.Hour)),
		},
		HasMore: true,
		Cursor:  "c2",
	}
	f.forge.updated["c2"] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{pullRequestAt("PR_0", "old", syncNow.Add(-50*24*time.Hour))}}

	f.sync(t)

	assert.Equal(t, []string{"updated:", search(syncNow), "branch:agent/x:", "decisions:3"}, f.forge.requests, "page two is never asked for, the search starts from when page one was read, the agent's branch is scanned, and only its open pull request costs a decision")
	sync := f.getSync(t)
	assert.False(t, sync.IsSweeping())
	require.True(t, sync.IsSwept())
	assert.True(t, sync.UpdatedThrough.Equal(syncNow), "the empty search moved the mark to its own time")
	require.NotNil(t, sync.ListedAt)
	assert.True(t, sync.ListedAt.Equal(syncNow))
	assert.Equal(t, 4, sync.Calls)
	f.stored(t, "PR_1")
	_, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", "PR_0")
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncSearchWritesOnlyChangedRows covers the pass after the first sweep: one search from
// the mark less the overlap, its rows written with their decisions and nothing else touched,
// the mark moved to the moment of the search, and no decision read for a row the search
// already decided.
func TestSyncSearchWritesOnlyChangedRows(t *testing.T) {
	f, mark := sweptFixture(t)
	before := f.stored(t, "PR_1")
	changedAt := syncNow.Add(30 * time.Second)
	f.forge.changed[""] = &forge.ListChangedResult{PullRequests: []forge.PullRequest{
		decided(pullRequestAt("PR_2", "agent/x", changedAt), forge.ReviewDecisionApproved),
	}}

	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(mark)}, f.forge.requests, "the search is the whole pass")
	written := f.stored(t, "PR_2")
	require.NotNil(t, written.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionApproved, *written.ReviewDecision)
	require.NotNil(t, written.DecisionChangedAt)
	assert.True(t, written.DecisionChangedAt.Equal(changedAt))
	assert.Equal(t, before, f.stored(t, "PR_1"), "a row the search did not answer is not touched")
	sync := f.getSync(t)
	assert.True(t, sync.UpdatedThrough.Equal(syncNow.Add(time.Minute)), "the mark is the moment of the search, which covered up to then")
	assert.Equal(t, 1, sync.Calls)
}

// TestSyncSearchMovesTheMarkToTheSearchTime covers the mark after a search: it moves to the
// moment of the search whether the search answered nothing or only rows older than that, and
// past the search time only when the forge's clock is ahead.
func TestSyncSearchMovesTheMarkToTheSearchTime(t *testing.T) {
	f, mark := sweptFixture(t)

	later := syncNow.Add(time.Minute)
	f.pass(t, later)

	assert.Equal(t, []string{search(mark)}, f.forge.requests)
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(later))

	f.forge.changed[""] = &forge.ListChangedResult{PullRequests: []forge.PullRequest{
		decided(pullRequestAt("PR_2", "agent/x", later.Add(-5*time.Minute)), forge.ReviewDecisionNone),
	}}
	f.pass(t, later.Add(time.Minute))
	assert.Equal(t, []string{search(later)}, f.forge.requests, "the next search starts from the moved mark")
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(later.Add(time.Minute)), "an answered row older than the search does not hold the mark back")

	ahead := later.Add(10 * time.Minute)
	f.forge.changed[""] = &forge.ListChangedResult{PullRequests: []forge.PullRequest{
		decided(pullRequestAt("PR_3", "agent/x", ahead), forge.ReviewDecisionNone),
	}}
	f.pass(t, later.Add(2*time.Minute))
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(ahead), "a row stamped ahead of the search moves the mark to it")
}

// TestSyncStaleMarkGoesBackToAFirstSweep covers a repository whose mark has fallen more than a
// day behind, which is swept again from page one.
func TestSyncStaleMarkGoesBackToAFirstSweep(t *testing.T) {
	f, _ := sweptFixture(t)
	f.agentIn(t, "b", otherRepository, "agent/y")
	other := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave-forge-fixture",
	}
	f.forge.elsewhere[other.Path] = map[string]*forge.ListUpdatedResult{"": {}}
	f.pass(t, syncNow.Add(time.Minute))
	require.True(t, f.getSyncOf(t, otherRepository).IsSwept())

	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))}}
	later := syncNow.Add(25 * time.Hour)
	f.pass(t, later)

	assert.Equal(t, "updated:", f.forge.requests[0], "the stale repository is swept again before the search")
	require.NotEmpty(t, f.forge.searches)
	last := f.forge.searches[len(f.forge.searches)-1]
	assert.Len(t, last.Repos, 2)
	assert.True(t, last.Since.Equal(later.Add(-pullRequestSyncOverlap)), "the search starts from the pass, not a day back")
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(later))
}

// TestSyncSearchPagesAndMovesTheMarkOnlyAtTheEnd covers a search with more than one page:
// every page is written, and the marks move in the last page's commit rather than the first.
func TestSyncSearchPagesAndMovesTheMarkOnlyAtTheEnd(t *testing.T) {
	f, mark := sweptFixture(t)
	newest := syncNow.Add(30 * time.Second)
	f.forge.changed[""] = &forge.ListChangedResult{
		PullRequests: []forge.PullRequest{decided(pullRequestAt("PR_3", "agent/x", newest), forge.ReviewDecisionNone)},
		HasMore:      true,
		Cursor:       "s2",
	}
	colliding := decided(pullRequestAt("PR_9", "agent/x", syncNow.Add(-2*time.Minute)), forge.ReviewDecisionNone)
	colliding.URL = "https://github.com/harrisoncramer/weave/pull/PR_1"
	f.forge.changed["s2"] = &forge.ListChangedResult{PullRequests: []forge.PullRequest{colliding}}

	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(mark), "changed:" + mark.Add(-pullRequestSyncOverlap).Format(time.RFC3339) + ":s2"}, f.forge.requests)
	f.stored(t, "PR_3")
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(mark), "a search whose last page failed leaves the mark where it was")

	f.forge.changed["s2"] = &forge.ListChangedResult{PullRequests: []forge.PullRequest{decided(pullRequestAt("PR_4", "agent/x", syncNow.Add(-2*time.Minute)), forge.ReviewDecisionNone)}}
	f.pass(t, syncNow.Add(2*time.Minute))

	f.stored(t, "PR_4")
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(syncNow.Add(2*time.Minute)), "the mark is the moment of the search once every page is written")
	assert.Equal(t, 2, f.getSync(t).Calls, "each page counts against the repository")
}

// TestSyncSearchLeavesOutAnUnsweptRepository covers two repositories on one host, one past
// its first sweep and one still in it: the search names only the first.
func TestSyncSearchLeavesOutAnUnsweptRepository(t *testing.T) {
	f, mark := sweptFixture(t)
	f.agentIn(t, "b", otherRepository, "agent/y")
	other := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave-forge-fixture",
	}
	pages := map[string]*forge.ListUpdatedResult{}
	for page := 1; page <= 12; page++ {
		cursor := ""
		if page > 1 {
			cursor = fmt.Sprintf("c%d", page)
		}
		pages[cursor] = &forge.ListUpdatedResult{
			PullRequests: []forge.PullRequest{pullRequestIn(other, fmt.Sprintf("PR_%d", page), "agent/y", syncNow.Add(-time.Duration(page)*time.Minute))},
			HasMore:      page < 12,
			Cursor:       fmt.Sprintf("c%d", page+1),
		}
	}
	f.forge.elsewhere[other.Path] = pages

	f.pass(t, syncNow.Add(time.Minute))

	require.Len(t, f.forge.searches, 2)
	require.Len(t, f.forge.searches[1].Repos, 1, "the repository still sweeping is not searched")
	assert.Equal(t, "harrisoncramer/weave", f.forge.searches[1].Repos[0].Path)
	assert.True(t, f.forge.searches[1].Since.Equal(mark.Add(-pullRequestSyncOverlap)))
	assert.True(t, f.getSyncOf(t, otherRepository).IsSweeping())

	f.pass(t, syncNow.Add(2*time.Minute))

	require.Len(t, f.forge.searches, 3)
	assert.Len(t, f.forge.searches[2].Repos, 2, "once swept, the repository joins the search")
	assert.True(t, f.forge.searches[2].Since.Equal(syncNow.Add(time.Minute).Add(-pullRequestSyncOverlap)), "the search starts from the oldest mark, which both repositories reached in the pass before")
}

// TestSyncQuietRepositoryDoesNotDragTheSearchBack covers a repository whose newest pull
// request is older than the horizon beside a busy one: its mark is the moment its page one was
// read rather than that row's stamp, so the host's search starts from the pass and not from
// forty days back.
func TestSyncQuietRepositoryDoesNotDragTheSearchBack(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.agentIn(t, "b", otherRepository, "agent/y")
	other := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave-forge-fixture",
	}
	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))}}
	f.forge.elsewhere[other.Path] = map[string]*forge.ListUpdatedResult{"": {
		PullRequests: []forge.PullRequest{pullRequestIn(other, "PR_2", "agent/y", syncNow.Add(-40*24*time.Hour))},
	}}

	f.sync(t)

	require.Len(t, f.forge.searches, 1)
	assert.Len(t, f.forge.searches[0].Repos, 2)
	assert.True(t, f.forge.searches[0].Since.Equal(syncNow.Add(-pullRequestSyncOverlap)), "the quiet repository's mark is when it was listed, not its forty day old row")
	assert.True(t, f.getSync(t).IsSwept())
	assert.True(t, f.getSyncOf(t, otherRepository).IsSwept())
}

// TestSyncSearchCapHandsBackToAFirstSweep covers a search that reaches the forge's cap: what
// it answered is written, no mark moves, and the repositories go back to a first sweep on the
// next pass.
func TestSyncSearchCapHandsBackToAFirstSweep(t *testing.T) {
	f, mark := sweptFixture(t)
	for page := 1; page <= 10; page++ {
		cursor := ""
		if page > 1 {
			cursor = fmt.Sprintf("s%d", page)
		}
		rows := make([]forge.PullRequest, 0, 100)
		for i := range 100 {
			rows = append(rows, decided(pullRequestAt(fmt.Sprintf("PR_%d", 1000+page*100+i), "agent/x", syncNow.Add(-time.Duration(page*100+i)*time.Second)), forge.ReviewDecisionNone))
		}
		f.forge.changed[cursor] = &forge.ListChangedResult{
			PullRequests: rows,
			HasMore:      page < 10,
			Cursor:       fmt.Sprintf("s%d", page+1),
		}
	}

	f.pass(t, syncNow.Add(time.Minute))

	assert.Len(t, f.forge.requests, 10)
	assert.Equal(t, search(mark), f.forge.requests[0])
	f.stored(t, "PR_2099")
	sync := f.getSync(t)
	assert.False(t, sync.IsSwept(), "a thousand rows is the cap, so the repository is back to a first sweep")
	assert.False(t, sync.IsSweeping())

	f.forge.changed = map[string]*forge.ListChangedResult{}
	f.pass(t, syncNow.Add(2*time.Minute))
	assert.Equal(t, "updated:", f.forge.requests[0], "the next pass sweeps from page one")
}

// TestSyncSearchStillPagingAfterTheBudgetHandsBack covers a forge whose search still has pages
// after the pass's budget of them, which is the cap by another name.
func TestSyncSearchStillPagingAfterTheBudgetHandsBack(t *testing.T) {
	f, _ := sweptFixture(t)
	for page := 1; page <= 10; page++ {
		cursor := ""
		if page > 1 {
			cursor = fmt.Sprintf("s%d", page)
		}
		f.forge.changed[cursor] = &forge.ListChangedResult{
			PullRequests: []forge.PullRequest{decided(pullRequestAt(fmt.Sprintf("PR_%d", 100+page), "agent/x", syncNow.Add(-time.Duration(page)*time.Second)), forge.ReviewDecisionNone)},
			HasMore:      true,
			Cursor:       fmt.Sprintf("s%d", page+1),
		}
	}

	f.pass(t, syncNow.Add(time.Minute))

	assert.Len(t, f.forge.requests, 10)
	f.stored(t, "PR_110")
	assert.False(t, f.getSync(t).IsSwept())
}

// TestSyncListsPageOneHourly covers the listing behind the search: an hour after page one was
// last read it is read again and written, on its own clock, without moving the mark.
func TestSyncListsPageOneHourly(t *testing.T) {
	f, mark := sweptFixture(t)
	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{
		pullRequestAt("PR_2", "agent/x", syncNow.Add(-30*time.Minute)),
		pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)),
	}}

	f.pass(t, syncNow.Add(30*time.Minute))
	assert.Equal(t, []string{search(mark)}, f.forge.requests, "half an hour in, the listing is not due")

	relisted := syncNow.Add(61 * time.Minute)
	f.pass(t, relisted)

	assert.Equal(t, []string{search(syncNow.Add(30 * time.Minute)), "updated:", "decisions:2"}, f.forge.requests, "the listing is due, and the row it wrote costs a decision")
	f.stored(t, "PR_2")
	sync := f.getSync(t)
	assert.True(t, sync.ListedAt.Equal(relisted))
	assert.True(t, sync.UpdatedThrough.Equal(relisted), "the mark is the search's, not the listing's")

	f.pass(t, relisted.Add(time.Minute))
	assert.Equal(t, []string{search(relisted)}, f.forge.requests, "the listing waits another hour")
}

// TestSyncBudgetPausesAndResumes covers a first sweep longer than one pass: ten pages are
// read, the cursor and the moment page one was read are held, the mark does not move, nothing
// else is asked, and the next pass carries on from the cursor and completes.
func TestSyncBudgetPausesAndResumes(t *testing.T) {
	f := newSyncFixture(t)
	for page := 1; page <= 12; page++ {
		cursor := ""
		if page > 1 {
			cursor = fmt.Sprintf("c%d", page)
		}
		f.forge.updated[cursor] = &forge.ListUpdatedResult{
			PullRequests: []forge.PullRequest{pullRequestAt(fmt.Sprintf("PR_%d", page), "agent/x", syncNow.Add(-time.Duration(page)*time.Minute))},
			HasMore:      page < 12,
			Cursor:       fmt.Sprintf("c%d", page+1),
		}
	}
	f.agentOn(t, "a", "agent/x")
	f.forge.branches["agent/x"] = map[string]*forge.ListResult{"": {}}

	f.sync(t)

	sync := f.getSync(t)
	require.True(t, sync.IsSweeping())
	assert.Equal(t, "c11", *sync.SweepCursor)
	assert.Nil(t, sync.UpdatedThrough)
	assert.True(t, sync.SweepNewest.Equal(syncNow), "page one's newest is the moment it was read, its rows being older")
	assert.Equal(t, 10, sync.Calls)
	assert.Len(t, f.forge.requests, 10, "the search, the branch scan and the decisions wait for the sweep")
	assert.Equal(t, "updated:c10", f.forge.requests[9])

	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{
		"updated:c11", "updated:c12", search(syncNow), "branch:agent/x:",
		"decisions:1,2,3,4,5,6,7,8,9,10,11,12",
	}, f.forge.requests, "the decisions go in one request, newest change first, and the search starts from when page one was read")
	sync = f.getSync(t)
	assert.False(t, sync.IsSweeping())
	assert.True(t, sync.UpdatedThrough.Equal(syncNow.Add(time.Minute)), "the sweep promoted the mark and the empty search moved it on")
	f.stored(t, "PR_12")
}

// TestSyncFailedPageRollsBackAndStillCounts covers a page whose write fails: the cursor stays
// on it, nothing from it is stored, the request was still counted, and the next pass reads it
// again.
func TestSyncFailedPageRollsBackAndStillCounts(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	colliding := pullRequestAt("PR_9", "agent/x", syncNow.Add(-2*time.Minute))
	colliding.URL = "https://github.com/harrisoncramer/weave/pull/PR_1"
	f.forge.updated[""] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))},
		HasMore:      true,
		Cursor:       "c2",
	}
	f.forge.updated["c2"] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_2", "agent/x", syncNow.Add(-90*time.Second)), colliding},
	}

	f.sync(t)

	sync := f.getSync(t)
	require.True(t, sync.IsSweeping())
	assert.Equal(t, "c2", *sync.SweepCursor)
	assert.Equal(t, 2, sync.Calls)
	f.stored(t, "PR_1")
	_, err := f.pullRequests.GetByIdentity(t.Context(), "github.com", "PR_2")
	require.ErrorIs(t, err, store.ErrNotFound, "the page that failed left nothing behind")

	f.forge.updated["c2"].PullRequests = f.forge.updated["c2"].PullRequests[:1]
	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, "updated:c2", f.forge.requests[0])
	f.stored(t, "PR_2")
	assert.False(t, f.getSync(t).IsSweeping())
}

// TestSyncScansANewBranchOnceAndAttributesLocally covers the branch scan and the attribution
// that follows it: a branch a live agent sits on is listed to completion once by cursor, its
// pull requests are attributed to that agent, and a second agent adopting the branch later is
// attributed with no forge call at all.
func TestSyncScansANewBranchOnceAndAttributesLocally(t *testing.T) {
	f := newSyncFixture(t)
	first := f.agentOn(t, "a", "agent/old")
	f.forge.updated[""] = &forge.ListUpdatedResult{}
	old := pullRequestAt("PR_OLD", "agent/old", syncNow.Add(-60*24*time.Hour))
	f.forge.branches["agent/old"] = map[string]*forge.ListResult{
		"": {
			PullRequests: []forge.PullRequest{old},
			HasMore:      true,
			Cursor:       "b2",
		},
		"b2": {PullRequests: []forge.PullRequest{pullRequestAt("PR_OLDER", "agent/old", syncNow.Add(-70*24*time.Hour))}},
	}

	f.sync(t)

	assert.Equal(t, []string{"updated:", search(syncNow), "branch:agent/old:", "branch:agent/old:b2", "decisions:6,8"}, f.forge.requests)
	scan, err := f.pullRequests.GetBranchScan(t.Context(), syncRepository, "agent/old")
	require.NoError(t, err)
	assert.NotNil(t, scan.CompletedAt)
	seen, err := f.pullRequests.ListObservations(t.Context(), first)
	require.NoError(t, err)
	assert.Len(t, seen, 2, "the agent is seen beside both pull requests from its branch")

	second := f.agentOn(t, "b", "agent/old")
	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(syncNow)}, f.forge.requests, "the branch is not scanned again")
	seen, err = f.pullRequests.ListObservations(t.Context(), second)
	require.NoError(t, err)
	assert.Len(t, seen, 2, "the second agent is attributed from what is stored")
}

// TestSyncBranchScanPausesOnTheBudget covers a scan that runs out of budget part way: the
// cursor is kept and the next pass carries on from it.
func TestSyncBranchScanPausesOnTheBudget(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/long")
	f.forge.updated[""] = &forge.ListUpdatedResult{}
	pages := map[string]*forge.ListResult{}
	for page := 1; page <= 11; page++ {
		cursor := ""
		if page > 1 {
			cursor = fmt.Sprintf("b%d", page)
		}
		pages[cursor] = &forge.ListResult{
			PullRequests: []forge.PullRequest{pullRequestAt(fmt.Sprintf("PR_%d", page), "agent/long", syncNow.Add(-time.Duration(page)*24*time.Hour))},
			HasMore:      page < 11,
			Cursor:       fmt.Sprintf("b%d", page+1),
		}
	}
	f.forge.branches["agent/long"] = pages

	f.sync(t)

	assert.Len(t, f.forge.requests, 11, "the sweep, the search and nine pages of the scan")
	scan, err := f.pullRequests.GetBranchScan(t.Context(), syncRepository, "agent/long")
	require.NoError(t, err)
	assert.Nil(t, scan.CompletedAt)
	assert.Equal(t, "b10", scan.NextCursor)

	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(syncNow), "branch:agent/long:b10", "branch:agent/long:b11", "decisions:1,2,3,4,5,6,7,8,9,10,11"}, f.forge.requests)
	scan, err = f.pullRequests.GetBranchScan(t.Context(), syncRepository, "agent/long")
	require.NoError(t, err)
	assert.NotNil(t, scan.CompletedAt)
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

	f.forge.refuse = errors.New("github answered 403 for /graphql: Resource not accessible")
	f.now = syncNow.Add(10 * time.Minute)
	f.sync(t)
	connection, err = f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	assert.True(t, connection.ThrottledUntil.Before(f.now), "a permission answer does not throttle")
}

// TestSyncSearchRefusalThrottlesTheHost covers the search itself being refused for the rate
// limit: the connection waits, and nothing more is asked of the host in the pass.
func TestSyncSearchRefusalThrottlesTheHost(t *testing.T) {
	f, mark := sweptFixture(t)
	f.forge.refuse = &forge.ThrottledError{RetryAt: syncNow.Add(20 * time.Minute)}
	f.forge.updated[""] = &forge.ListUpdatedResult{}

	f.pass(t, syncNow.Add(61*time.Minute))

	assert.Equal(t, []string{search(mark)}, f.forge.requests, "the hourly listing is not asked once the host is spent")
	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(syncNow.Add(20*time.Minute)))
	assert.True(t, f.getSync(t).UpdatedThrough.Equal(mark), "a refused search moves no mark")
}

// TestSyncStopsWhenTheBudgetHeaderIsSpent covers a success whose headers say the budget is gone:
// the page is written, the reset is recorded on the connection, and the host is left alone for
// the rest of the pass and until the reset.
func TestSyncStopsWhenTheBudgetHeaderIsSpent(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	reset := syncNow.Add(20 * time.Minute)
	f.forge.updated[""] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Minute))},
		HasMore:      true,
		Cursor:       "c2",
		RateLimit: forge.RateLimit{
			Known:     true,
			Remaining: 0,
			ResetAt:   reset,
		},
	}

	f.sync(t)

	assert.Equal(t, []string{"updated:"}, f.forge.requests)
	f.stored(t, "PR_1")
	connection, err := f.connections.GetByHost(t.Context(), "github.com")
	require.NoError(t, err)
	require.NotNil(t, connection.ThrottledUntil)
	assert.True(t, connection.ThrottledUntil.Equal(reset))
	assert.True(t, f.getSync(t).IsSweeping())
}

// TestSyncSaysAMissingRepositoryOnce covers a repository the forge does not have: it is said
// once as a warning rather than as an error every pass, asked about again each pass, and said
// again only once it has been found and lost again.
func TestSyncSaysAMissingRepositoryOnce(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.refuse = fmt.Errorf("%w: gone", forge.ErrNotFound)
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	for range 3 {
		require.NoError(t, f.syncer.Sync(ctx))
	}

	assert.Len(t, f.forge.requests, 3, "the repository is still asked about each pass")
	assert.Equal(t, 1, strings.Count(logged.String(), "the forge has no such repository"))
	assert.NotContains(t, logged.String(), "failed to sync the repository")
	assert.False(t, f.getSync(t).IsSwept())

	f.forge.refuse = nil
	f.forge.updated[""] = &forge.ListUpdatedResult{}
	require.NoError(t, f.syncer.Sync(ctx))
	f.agentOn(t, "b", "agent/y")
	f.forge.refuse = fmt.Errorf("%w: gone again", forge.ErrNotFound)
	require.NoError(t, f.syncer.Sync(ctx))

	assert.Equal(t, 2, strings.Count(logged.String(), "the forge has no such repository"), "a repository found and lost again is said again")
}

// TestSyncSaysASweptRepositoryThatGoesMissingOnce covers a repository lost after its first
// sweep, where only its hourly listing meets the refusal: it is still said once.
func TestSyncSaysASweptRepositoryThatGoesMissingOnce(t *testing.T) {
	f, _ := sweptFixture(t)
	f.forge.refuseUpdated = fmt.Errorf("%w: gone", forge.ErrNotFound)
	f.forge.requests = nil
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	for minute := 61; minute <= 63; minute++ {
		f.now = syncNow.Add(time.Duration(minute) * time.Minute)
		require.NoError(t, f.syncer.Sync(ctx))
	}

	assert.Equal(t, 3, strings.Count(strings.Join(f.forge.requests, " "), "updated:"), "the listing is due every pass while it keeps failing")
	assert.Equal(t, 1, strings.Count(logged.String(), "the forge has no such repository"))
}

// TestSyncSaysARenamedRepositoryOnce covers a listing answered under another name: the rows
// are stored as the forge names them and the rename is said once.
func TestSyncSaysARenamedRepositoryOnce(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	renamed := git.RemoteURL{
		Scheme: "https",
		Host:   "github.com",
		Path:   "harrisoncramer/weave-renamed",
	}
	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{pullRequestIn(renamed, "PR_1", "agent/x", syncNow.Add(-time.Hour))}}
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	require.NoError(t, f.syncer.Sync(ctx))
	f.now = syncNow.Add(61 * time.Minute)
	require.NoError(t, f.syncer.Sync(ctx))

	assert.Equal(t, 1, strings.Count(logged.String(), "under another name"), "the sweep says it, the hourly listing does not repeat it")
	assert.Equal(t, renamed.Canonical(), f.stored(t, "PR_1").RepositoryURL)
}

// TestSyncBacksOffARefusedCredential covers the forge refusing the credential: the pass stops
// asking the host, the connection waits the way a rate limit refusal with no time makes it
// wait, the refusal is said once, and it is said again after a later refusal.
func TestSyncBacksOffARefusedCredential(t *testing.T) {
	f, _ := sweptFixture(t)
	f.agentIn(t, "b", otherRepository, "agent/y")
	f.forge.refuse = fmt.Errorf("%w: bad credentials", forge.ErrUnauthorized)
	var logged bytes.Buffer
	ctx := logging.WithLogger(t.Context(), slog.New(slog.NewTextHandler(&logged, nil)))

	f.now = syncNow.Add(time.Minute)
	f.forge.requests = nil
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
	require.NoError(t, f.syncer.Sync(ctx))
	assert.Equal(t, 2, strings.Count(logged.String(), "the forge refused the connection's credential"), "a later refusal is said again")
}

// TestSyncSkipsARepositoryWithNoConnection covers a host nobody connected: nothing is asked and
// nothing is written.
func TestSyncSkipsARepositoryWithNoConnection(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	elsewhere := "https://gitlab.com/harrisoncramer/weave"
	f.agentIn(t, "b", elsewhere, "agent/y")
	f.forge.updated[""] = &forge.ListUpdatedResult{}

	f.sync(t)
	f.sync(t)

	for _, request := range f.forge.requests {
		assert.NotContains(t, request, "gitlab")
	}
	for _, params := range f.forge.searches {
		assert.Len(t, params.Repos, 1)
	}
	_, err := f.pullRequests.GetSync(t.Context(), elsewhere)
	require.ErrorIs(t, err, store.ErrNotFound)
}

// TestSyncReadsTheDecisionOfAChangedOpenPullRequest covers which pull requests cost a decision
// read: an open one on a live branch when a listing wrote it, and again only once a listing
// wrote it changed; never a draft, a finished one, one on a branch no live agent sits on, or
// one the search wrote with its decision.
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
	merged.State = "MERGED"
	elsewhere := pullRequestAt("PR_4", "agent/nobody", syncNow.Add(-4*time.Hour))
	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{open, draft, merged, elsewhere}}
	f.forge.decisions[1] = forge.ReviewDecisionApproved

	f.sync(t)

	assert.Equal(t, []string{"updated:", search(syncNow), "branch:agent/x:", "decisions:1"}, f.forge.requests)
	stored := f.stored(t, "PR_1")
	require.NotNil(t, stored.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionApproved, *stored.ReviewDecision)
	assert.NotNil(t, stored.DecisionChangedAt)
	for _, providerID := range []string{"PR_2", "PR_3", "PR_4"} {
		assert.Nil(t, f.stored(t, providerID).DecisionChangedAt, "%s costs no decision read", providerID)
	}

	f.pass(t, syncNow.Add(time.Minute))
	assert.Equal(t, []string{search(syncNow)}, f.forge.requests, "a decision read since the last change is not read again")

	changed := decided(open, forge.ReviewDecisionChangesRequested)
	changed.UpdatedAt = syncNow.Add(time.Minute)
	f.forge.changed[""] = &forge.ListChangedResult{PullRequests: []forge.PullRequest{changed}}
	f.pass(t, syncNow.Add(2*time.Minute))

	assert.Equal(t, []string{search(syncNow.Add(time.Minute))}, f.forge.requests, "the search carried the decision, so none is read")
	stored = f.stored(t, "PR_1")
	require.NotNil(t, stored.ReviewDecision)
	assert.Equal(t, forge.ReviewDecisionChangesRequested, *stored.ReviewDecision)
	assert.True(t, stored.DecisionChangedAt.Equal(changed.UpdatedAt))
}

// TestSyncDecisionsGoFiftyToARequest covers more stale decisions than one request takes: they
// go newest change first, fifty at a time, each request costing one of the budget.
func TestSyncDecisionsGoFiftyToARequest(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	var pullRequests []forge.PullRequest
	for number := 1; number <= 60; number++ {
		pullRequests = append(pullRequests, pullRequestAt(fmt.Sprintf("PR_%d", number), "agent/x", syncNow.Add(-time.Duration(number)*time.Minute)))
	}
	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: pullRequests}

	f.sync(t)

	require.Len(t, f.forge.requests, 5)
	first := strings.Split(strings.TrimPrefix(f.forge.requests[3], "decisions:"), ",")
	assert.Len(t, first, forge.MaxReviewDecisions)
	assert.Equal(t, "1", first[0])
	assert.Equal(t, "50", first[49])
	assert.Equal(t, "decisions:51,52,53,54,55,56,57,58,59,60", f.forge.requests[4])
	assert.Equal(t, 5, f.getSync(t).Calls)
	assert.NotNil(t, f.stored(t, "PR_60").DecisionChangedAt)
}

// TestSyncDecisionsWaitForTheBudget covers a pass whose sweep and scan spend the budget: the
// decisions stay stale, and the next pass reads them.
func TestSyncDecisionsWaitForTheBudget(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	for page := 1; page <= 9; page++ {
		cursor := ""
		if page > 1 {
			cursor = fmt.Sprintf("c%d", page)
		}
		f.forge.updated[cursor] = &forge.ListUpdatedResult{
			PullRequests: []forge.PullRequest{pullRequestAt(fmt.Sprintf("PR_%d", page), "agent/x", syncNow.Add(-time.Duration(page)*time.Minute))},
			HasMore:      page < 9,
			Cursor:       fmt.Sprintf("c%d", page+1),
		}
	}

	f.sync(t)

	assert.Len(t, f.forge.requests, 11, "nine pages, the search and the scan leave nothing for the decisions")
	assert.Equal(t, "branch:agent/x:", f.forge.requests[10])
	assert.Nil(t, f.stored(t, "PR_1").DecisionChangedAt)

	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(syncNow), "decisions:1,2,3,4,5,6,7,8,9"}, f.forge.requests)
	assert.NotNil(t, f.stored(t, "PR_9").DecisionChangedAt)
}

// TestSyncDecisionTheForgeDoesNotHaveIsRead covers a pull request the forge leaves out of its
// answer, which is stamped as read with nothing said rather than asked for again every pass.
func TestSyncDecisionTheForgeDoesNotHaveIsRead(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[""] = &forge.ListUpdatedResult{PullRequests: []forge.PullRequest{
		pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)),
		pullRequestAt("PR_2", "agent/x", syncNow.Add(-2*time.Hour)),
	}}
	f.forge.unknown[1] = true
	f.forge.decisions[2] = forge.ReviewDecisionApproved

	f.sync(t)

	stored := f.stored(t, "PR_1")
	assert.Nil(t, stored.ReviewDecision)
	assert.NotNil(t, stored.DecisionChangedAt)
	assert.Equal(t, forge.ReviewDecisionApproved, *f.stored(t, "PR_2").ReviewDecision, "the rest of the batch is still read")

	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(syncNow)}, f.forge.requests)
}

// TestSyncDecisionRefusalStopsTheRepository covers any other refusal, which is reported and
// leaves the decisions stale for the next pass.
func TestSyncDecisionRefusalStopsTheRepository(t *testing.T) {
	f := newSyncFixture(t)
	f.agentOn(t, "a", "agent/x")
	f.forge.updated[""] = &forge.ListUpdatedResult{
		PullRequests: []forge.PullRequest{pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour))},
	}
	f.forge.refuseDecisions = errors.New("the forge answered 502")

	f.sync(t)

	assert.Nil(t, f.stored(t, "PR_1").DecisionChangedAt)
	assert.Equal(t, 4, f.getSync(t).Calls, "the refused request is still counted")

	f.forge.refuseDecisions = nil
	f.pass(t, syncNow.Add(time.Minute))

	assert.Equal(t, []string{search(syncNow), "decisions:1"}, f.forge.requests)
	assert.NotNil(t, f.stored(t, "PR_1").DecisionChangedAt)
}
