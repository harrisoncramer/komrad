package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePRStates struct {
	state string
	err   error
	refs  []string
}

func (f *fakePRStates) Resolve(_ context.Context, _, ref string) (*PullRequest, error) {
	f.refs = append(f.refs, ref)
	if f.err != nil {
		return nil, f.err
	}

	return &PullRequest{URL: ref, State: f.state}, nil
}

type fakeArchiver struct {
	archived []string
	err      error
}

func (f *fakeArchiver) ArchiveAgents(_ context.Context, ids []string) error {
	f.archived = append(f.archived, ids...)

	return f.err
}

func seedPRWorkflow(t *testing.T, st *testStore, archiveOnPRClosed, worktree bool) string {
	t.Helper()

	created, err := st.Workflows.Create(t.Context(), workflowstore.Config{
		Name: "worktrees",
		Cwd:  t.TempDir(),
		Worktree: workflowstore.WorktreeConfig{
			Enabled:           worktree,
			ArchiveOnPRClosed: archiveOnPRClosed,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	return created.ID
}

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

	if url != "" {
		_, err = st.Agents.SetMetadata(ctx, agentstore.SetMetadataParams{ID: got.ID, Key: prMetadataKey, Value: &url})
		require.NoError(t, err)

		got, err = st.Agents.GetInWorkflow(ctx, workflowID, "agent")
		require.NoError(t, err)
		require.NotNil(t, got)
	}

	return *got
}

func newPRArchiver(st *testStore, prs prStateResolver, archiver prArchiveAgentArchiver) *PRArchiver {
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
			name:              "a workflow that is not a worktree one carries the setting dormant",
			worktreeDisabled:  true,
			archiveOnPRClosed: true,
			agentState:        agentstore.StateIdle,
			pane:              "%1",
			url:               prURL,
			prState:           "MERGED",
			wantArchived:      false,
			wantLookups:       0,
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

			prs := &fakePRStates{state: tt.prState}
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

	prs := &fakePRStates{err: errors.New("gh: no network")}
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

	prs := &fakePRStates{err: errors.New("gh: API rate limit exceeded")}
	archiver := &fakeArchiver{}
	archiverUnderTest := newPRArchiver(st, prs, archiver)

	now := time.Now()
	archiverUnderTest.now = func() time.Time { return now }

	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	require.NoError(t, archiverUnderTest.Sync(t.Context()))
	assert.Len(t, prs.refs, 1, "the second pass is still inside the backoff")

	now = now.Add(prStateBackoff + time.Minute)
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
	require.NoError(t, st.Agents.Archive(t.Context(), []string{agent.ID}))

	prs := &fakePRStates{state: "MERGED"}
	archiver := &fakeArchiver{}

	require.NoError(t, newPRArchiver(st, prs, archiver).Sync(t.Context()))

	assert.Empty(t, prs.refs)
	assert.Empty(t, archiver.archived)
}

// TestPRArchiverSyncReportsAFailedArchive covers the pass surfacing what it could not do,
// since the loop's own logging is the only place a failed archive shows up.
func TestPRArchiverSyncReportsAFailedArchive(t *testing.T) {
	st := newStore(t)

	workflowID := seedPRWorkflow(t, st, true, true)
	seedPRAgent(t, st, workflowID, agentstore.StateIdle, "%1", prURL)

	prs := &fakePRStates{state: "MERGED"}
	archiver := &fakeArchiver{err: errors.New("nope")}

	err := newPRArchiver(st, prs, archiver).Sync(t.Context())

	require.ErrorContains(t, err, "failed to archive 1 agents whose pull requests closed")
}
