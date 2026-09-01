package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePRStates struct {
	state  string
	branch string
	repo   string
	origin string
	err    error
	refs   []string
}

func (f *fakePRStates) Resolve(_ context.Context, _, ref string) (*PullRequest, error) {
	f.refs = append(f.refs, ref)
	if f.err != nil {
		return nil, f.err
	}

	return &PullRequest{
		URL:        ref,
		State:      f.state,
		Branch:     f.branch,
		Repository: f.repo,
	}, nil
}

// Repository stands in for the workflow's own origin, which is what a resolved pull request is
// checked against.
func (f *fakePRStates) Repository(context.Context, string) (string, error) {
	return f.origin, nil
}

type fakeArchiver struct {
	archived []string
	err      error
}

// ArchiveUnchangedAgents stands in for the guarded archive, which the real one performs one
// row at a time.
func (f *fakeArchiver) ArchiveUnchangedAgents(_ context.Context, observed []agentstore.Agent) ([]agentstore.Agent, error) {
	if f.err != nil {
		return nil, f.err
	}

	for _, agent := range observed {
		f.archived = append(f.archived, agent.ID)
	}

	return observed, nil
}

func seedPRWorkflow(t *testing.T, st *testStore, archiveOnPRClosed, worktree bool) string {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
		Name:              "worktrees",
		Cwd:               t.TempDir(),
		Worktree:          workflowstore.WorktreeConfig{Enabled: worktree},
		ArchiveOnPRClosed: archiveOnPRClosed,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

const prBranch = "harry-fix"

const prRepo = "harrisoncramer/weave"

func seedPRAgent(t *testing.T, st *testStore, workflowID, state, pane, url string) agentstore.Agent {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:       "agent",
		Pane:       pane,
		State:      state,
		WorkflowID: workflowID,
		Cwd:        t.TempDir(),
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, "agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.NoError(t, st.Agents.SetBranchByID(ctx, got.ID, xptr.Ptr(prBranch)))

	if url != "" {
		_, err = st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{
			ID:    got.ID,
			Key:   prMetadataKey,
			Value: &url,
		})
		require.NoError(t, err)

		got, err = st.Agents.GetInWorkflow(ctx, workflowID, "agent")
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	return *got
}

func reread(t *testing.T, st *testStore) agentstore.Agent {
	t.Helper()

	got, err := st.Agents.GetByID(t.Context(), agentIDOf(t, st))
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func agentIDOf(t *testing.T, st *testStore) string {
	t.Helper()

	agents, err := st.Agents.ListAll(t.Context())
	require.NoError(t, err)
	require.Len(t, agents, 1)

	return agents[0].ID
}

func newPRArchiver(st *testStore, prs pullRequestResolver, archiver prArchiveAgentArchiver) *PRArchiver {
	return NewPRArchiver(PRArchiverParams{
		Agents:       st.Agents,
		Workflows:    st.Workflows,
		Archiver:     archiver,
		PullRequests: prs,
	})
}

const prURL = "https://github.com/harrisoncramer/weave/pull/1"

// TestPRArchiverSync covers which agents a pass archives and which it leaves for somebody to
// look at: the state of the pull request, the state of the agent, and whether the workflow
// asked for this at all.
func TestPRArchiverSync(t *testing.T) {
	tests := []struct {
		name              string
		worktreeDisabled  bool
		archiveOnPRClosed bool
		agentState        string
		pane              string
		url               string
		prState           string
		wantArchived      bool
		wantLookups       int
	}{
		{
			name:              "a merged pull request archives the idle agent that opened it",
			archiveOnPRClosed: true,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               prURL,
			prState:           "MERGED",
			wantArchived:      true,
			wantLookups:       1,
		},
		{
			name:              "a closed pull request archives too, since neither state is coming back",
			archiveOnPRClosed: true,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               prURL,
			prState:           "CLOSED",
			wantArchived:      true,
			wantLookups:       1,
		},
		{
			name:              "an open pull request leaves the agent alone",
			archiveOnPRClosed: true,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               prURL,
			prState:           "OPEN",
			wantArchived:      false,
			wantLookups:       1,
		},
		{
			name:              "an agent mid-turn is left for a later pass rather than losing its pane",
			archiveOnPRClosed: true,
			agentState:        agentstore.StateThinking,
			pane:              "%1",
			url:               prURL,
			prState:           "MERGED",
			wantArchived:      false,
			wantLookups:       0,
		},
		{
			name:              "a dormant agent has no pane to lose, so a merge archives it",
			archiveOnPRClosed: true,
			agentState:        agentstore.StateThinking,
			pane:              "",
			url:               prURL,
			prState:           "MERGED",
			wantArchived:      true,
			wantLookups:       1,
		},
		{
			name:              "a workflow that did not ask for this is never looked up",
			archiveOnPRClosed: false,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               prURL,
			prState:           "MERGED",
			wantArchived:      false,
			wantLookups:       0,
		},
		{
			name:              "a workflow with no worktree archives on the branch its agent's own checkout is on",
			worktreeDisabled:  true,
			archiveOnPRClosed: true,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               prURL,
			prState:           "MERGED",
			wantArchived:      true,
			wantLookups:       1,
		},
		{
			name:              "an agent with no recorded pull request is nothing to ask about",
			archiveOnPRClosed: true,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               "",
			prState:           "MERGED",
			wantArchived:      false,
			wantLookups:       0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)

			workflowID := seedPRWorkflow(t, st, tt.archiveOnPRClosed, !tt.worktreeDisabled)
			agent := seedPRAgent(t, st, workflowID, tt.agentState, tt.pane, tt.url)

			prs := &fakePRStates{
				state:  tt.prState,
				branch: prBranch,
				repo:   prRepo,
				origin: prRepo,
			}
			archiver := &fakeArchiver{}

			require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

			assert.Len(t, prs.refs, tt.wantLookups)
			if tt.wantArchived {
				assert.Equal(t, []string{agent.ID}, archiver.archived)

				return
			}
			assert.Empty(t, archiver.archived)
		})
	}
}

// TestPRArchiverSyncLeavesAnAgentAloneWhenTheLookupFails covers the failure that matters most:
// gh not answering must never read as a finished pull request.
func TestPRArchiverSyncLeavesAnAgentAloneWhenTheLookupFails(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		err:    errors.New("gh: no network"),
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}

	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

	assert.Len(t, prs.refs, 1)
	assert.Empty(t, archiver.archived)
}

// TestPRArchiverSyncBacksOffAfterARateLimit covers what a rate-limited lookup buys: the next
// pass asks nothing at all rather than spending the same refused quota again.
func TestPRArchiverSyncBacksOffAfterARateLimit(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		err:    errors.New("gh: API rate limit exceeded"),
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}
	archiverUnderTest := newPRArchiver(st, prs, archiver)

	now := time.Now()
	archiverUnderTest.now = func() time.Time { return now }

	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	assert.Len(t, prs.refs, 1, "the second pass is still inside the backoff")

	now = now.Add(prStateQuiet + time.Minute)
	prs.err = nil
	prs.state = "MERGED"

	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	assert.Len(t, prs.refs, 2, "the backoff has expired, so the agent is asked about again")
	assert.Len(t, archiver.archived, 1)
}

// TestPRArchiverSyncSkipsArchivedAgents covers that a pass walks live agents only, so an
// agent it archived is not archived again on every pass afterwards.
func TestPRArchiverSyncSkipsArchivedAgents(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	agent := seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)
	_, archiveErr := st.Agents.Archive(t.Context(), []string{agent.ID})
	require.NoError(t, archiveErr)

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}

	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

	assert.Empty(t, prs.refs)
	assert.Empty(t, archiver.archived)
}

// TestPRArchiverSyncLeavesARestoredAgentAlone pins the reason an archive is stamped, which is
// that a restored agent still reads idle with the same pull request on it.
func TestPRArchiverSyncLeavesARestoredAgentAlone(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	agent := seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}
	archiverUnderTest := newPRArchiver(st, prs, archiver)

	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	require.Equal(t, []string{agent.ID}, archiver.archived)
	assert.Equal(t, prURL, metadataFor(reread(t, st), prArchivedMetadataKey), "the archive says which pull request it was for")

	_, archiveErr := st.Agents.Archive(t.Context(), []string{agent.ID})

	require.NoError(t, archiveErr)
	require.NoError(t, st.Agents.Restore(t.Context(), []string{agent.ID}))

	require.NoError(t, archiverUnderTest.Sync(t.Context()))

	assert.Len(t, prs.refs, 1, "the restored agent is not asked about again")
	assert.Len(t, archiver.archived, 1, "and it is not archived again")
}

// TestPRArchiverSyncStampsAnAgentItReallyArchived drives the real archive, since a store that
// refused metadata to archived rows would leave every archive unstamped.
func TestPRArchiverSyncStampsAnAgentItReallyArchived(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	require.NoError(t, newPRArchiver(st, prs, newManager(newFakeTmux(), st, "").lifecycle).Sync(ctx))

	archived := reread(t, st)
	require.NotNil(t, archived.ArchivedAt, "the agent was archived")
	assert.Equal(t, prURL, metadataFor(archived, prArchivedMetadataKey), "and the archive says which pull request it was for")
}

// TestPRArchiverSyncLeavesAnAgentRestoredByHandAlone pins the stamp being written on restore,
// since an agent archived by hand carries none of its own.
func TestPRArchiverSyncLeavesAnAgentRestoredByHandAlone(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	workflowID := seedPRWorkflow(t, st, true, true)
	agent := seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	_, archiveErr := st.Agents.Archive(ctx, []string{agent.ID})
	require.NoError(t, archiveErr)

	restored, err := newManager(newFakeTmux(), st, "").lifecycle.RestoreAgents(ctx, []string{agent.ID})
	require.NoError(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, prURL, metadataFor(reread(t, st), prArchivedMetadataKey), "the restore says which pull request it was for")

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}
	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(ctx))

	assert.Empty(t, prs.refs, "the restored agent is not asked about")
	assert.Empty(t, archiver.archived, "and it is not archived again")
}

// TestRestoreWorkflowLeavesItsAgentsAlone pins that agents brought back with their workflow
// count as deliberately restored too.
func TestRestoreWorkflowLeavesItsAgentsAlone(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	ctx := t.Context()

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	workflows := workflowsFor(st, tmux)
	require.NoError(t, workflows.ArchiveWorkflow(ctx, workflowID))

	restored, err := workflows.RestoreWorkflow(ctx, workflowID)
	require.NoError(t, err)
	require.NotNil(t, restored)

	assert.Equal(t, prURL, metadataFor(reread(t, st), prArchivedMetadataKey))

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}
	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(ctx))

	assert.Empty(t, archiver.archived)
}

// TestPRArchiverSyncRefusesAPullRequestThatIsNotTheAgentsOwn pins that a stale recorded URL
// does not hide an agent whose real pull request is still open.
func TestPRArchiverSyncRefusesAPullRequestThatIsNotTheAgentsOwn(t *testing.T) {
	tests := []struct {
		name   string
		branch string
		repo   string
	}{
		{
			name:   "a closed pull request on some other branch is not this agent's",
			branch: "somebody-elses-branch",
			repo:   prRepo,
		},
		{
			name:   "a closed pull request in some other repository is not this agent's",
			branch: prBranch,
			repo:   "harrisoncramer/other",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)

			workflowID := seedPRWorkflow(t, st, true, true)
			seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

			prs := &fakePRStates{
				state:  "MERGED",
				branch: tt.branch,
				repo:   tt.repo,
				origin: prRepo,
			}
			archiver := &fakeArchiver{}

			require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

			assert.Empty(t, archiver.archived)
		})
	}
}

// TestPRArchiverSyncJudgesABlankBranchByTheWorktree pins that a blank branch is judged by the
// worktree, since a timeout and a detached HEAD read the same as a directory that has gone.
func TestPRArchiverSyncJudgesABlankBranchByTheWorktree(t *testing.T) {
	tests := []struct {
		name            string
		worktreeRemoved bool
		wantArchived    bool
	}{
		{
			name:            "a worktree that is really gone leaves nothing to check, so the merge stands",
			worktreeRemoved: true,
			wantArchived:    true,
		},
		{
			name:            "a worktree still on disk means git simply could not answer, so the agent is left alone",
			worktreeRemoved: false,
			wantArchived:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			workflowID := seedPRWorkflow(t, st, true, true)
			agent := seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

			require.NoError(t, st.Agents.SetBranchByID(ctx, agent.ID, nil))
			require.NoError(t, st.Agents.SetWorktreeRemovedByID(ctx, agent.ID, tt.worktreeRemoved))

			prs := &fakePRStates{
				state:  "MERGED",
				branch: prBranch,
				repo:   prRepo,
				origin: prRepo,
			}
			archiver := &fakeArchiver{}

			require.NoError(t, newPRArchiver(st, prs, archiver).Sync(ctx))

			assert.Equal(t, tt.wantArchived, len(archiver.archived) == 1)
		})
	}
}

// TestPRArchiverSyncMatchesTheRepositoryWhateverItsCasing covers the two casings meeting: one
// comes from a pull request URL and the other from a git remote, so comparing them exactly
// leaves an agent that is never archived and says nothing about why.
func TestPRArchiverSyncMatchesTheRepositoryWhateverItsCasing(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: "HarrisonCramer/Weave",
	}
	archiver := &fakeArchiver{}

	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

	assert.Len(t, archiver.archived, 1)
}

// TestPRArchiverSyncIgnoresAPullRequestReferenceThatIsNotAURL pins that a bare number is
// ignored, since gh resolves it against whichever repository it runs in.
func TestPRArchiverSyncIgnoresAPullRequestReferenceThatIsNotAURL(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", "42")

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}

	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

	assert.Empty(t, prs.refs, "nothing is asked about a reference weave cannot check")
	assert.Empty(t, archiver.archived)
}

// TestPRArchiverSyncGoesQuietOnAnOpenPullRequest covers the cost of an agent that sits idle for
// days on an open pull request: without a pause it is asked about on every pass forever.
func TestPRArchiverSyncGoesQuietOnAnOpenPullRequest(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		state:  "OPEN",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{}
	archiverUnderTest := newPRArchiver(st, prs, archiver)

	now := time.Now()
	archiverUnderTest.now = func() time.Time { return now }

	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	assert.Len(t, prs.refs, 1, "the second pass is still inside the pause")

	now = now.Add(prStateQuiet + time.Minute)
	prs.state = "MERGED"

	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	assert.Len(t, archiver.archived, 1, "once the pause is over the merge is noticed")
}

// TestPRArchiverSyncReportsAFailedArchive covers the pass surfacing what it could not do,
// since the loop's own logging is the only place a failed archive shows up.
func TestPRArchiverSyncReportsAFailedArchive(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{
		state:  "MERGED",
		branch: prBranch,
		repo:   prRepo,
		origin: prRepo,
	}
	archiver := &fakeArchiver{err: errors.New("nope")}

	err := newPRArchiver(st, prs, archiver).Sync(t.Context())

	require.ErrorContains(t, err, "failed to archive 1 agents whose pull requests closed")
	assert.Empty(t, metadataFor(reread(t, st), prArchivedMetadataKey), "an archive that failed stamps nothing, so a later pass tries again")
}
