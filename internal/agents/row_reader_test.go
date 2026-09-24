package agents

import (
	"context"
	"testing"
	"time"

	"weave/internal/forge"
	"weave/internal/storetest"
	"weave/lib/xslice"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	markstore "weave/internal/store/mark"
	pullrequeststore "weave/internal/store/pullrequest"
	reviewstore "weave/internal/store/review"
	rowstore "weave/internal/store/row"
	transcriptstore "weave/internal/store/transcript"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readerRepository = "https://github.com/harrisoncramer/weave"

const missingAgentID = "00000000-0000-0000-0000-000000000000"

func newRowReader(st *storetest.Stores) *RowReader {
	reviews := reviewstore.NewReviewStore(st.Pool.GetDB())

	return NewRowReader(NewRowReaderParams{
		Agents:       st.Agents,
		Rows:         st.Rows,
		Transcripts:  st.Transcripts,
		Marks:        st.Marks,
		Mail:         st.Mail,
		PullRequests: st.PullRequests,
		Repositories: st.PullRequestSyncs,
		Reviews:      reviews,
		Workflows:    st.Workflows,
	})
}

// TestRowReaderRefusesAnAgentNobodyHolds pins that a transcript read names the missing agent rather than answering empty.
func TestRowReaderRefusesAnAgentNobodyHolds(t *testing.T) {
	tests := []struct {
		name string
		read func(t *testing.T, reader *RowReader) error
	}{
		{
			name: "reading its transcript",
			read: func(t *testing.T, reader *RowReader) error {
				_, err := reader.ReadTranscript(t.Context(), missingAgentID)

				return err
			},
		},
		{
			name: "listing its last turns",
			read: func(t *testing.T, reader *RowReader) error {
				_, err := reader.ListLastTurns(t.Context(), ListLastTurnsParams{AgentID: missingAgentID})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.read(t, newRowReader(storetest.Open(t))), ErrAgentNotFound)
		})
	}
}

// TestListLastTurns pins that a listing counts the agent's own turns, one when none is asked for.
func TestListLastTurns(t *testing.T) {
	st := storetest.Open(t)
	reader := newRowReader(st)
	alpha := st.SeedAgent(t, agentstore.Agent{Name: "alpha"})
	st.SeedTurns(t, alpha.ID,
		transcriptstore.Turn{
			Role:    "user",
			Content: "opening question",
		},
		transcriptstore.Turn{
			Role:    "assistant",
			Content: "a first reading",
		},
		transcriptstore.Turn{
			Role:    "user",
			Content: "a nudge",
		},
		transcriptstore.Turn{
			Role:    "assistant",
			Content: "the conclusion",
		},
	)

	tests := []struct {
		name     string
		turns    int32
		wantSeqs []int32
	}{
		{
			name:     "no count reads the last turn the agent wrote",
			wantSeqs: []int32{3},
		},
		{
			name:     "two of its own turns come with the prompt between them",
			turns:    2,
			wantSeqs: []int32{1, 2, 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			turns, err := reader.ListLastTurns(t.Context(), ListLastTurnsParams{
				AgentID: alpha.ID,
				Turns:   tt.turns,
			})
			require.NoError(t, err)
			assert.Equal(t, tt.wantSeqs, xslice.Map(turns, func(turn transcriptstore.StoredTurn) int32 {
				return turn.Seq
			}))
		})
	}
}

// TestListSurroundings pins which watched rows a page of agents is drawn beside.
func TestListSurroundings(t *testing.T) {
	st := storetest.Open(t)
	reader := newRowReader(st)
	ctx := t.Context()

	opened := seedAgentOnBranch(t, st, "opened", readerRepository, "agent/x")
	elsewhere := seedAgentOnBranch(t, st, "elsewhere", "https://github.com/harrisoncramer/unread", "agent/x")
	bystander := st.SeedWorkflow(t, workflowstore.Config{Name: "bystander"})

	_, err := st.Marks.Set(ctx, "a", markstore.OnAgent(elsewhere.ID))
	require.NoError(t, err)
	_, err = st.Mail.Send(ctx, mailstore.SendParams{
		FromName:  "beta",
		ToAgentID: elsewhere.ID,
		Body:      "refunded_cents",
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	require.NoError(t, st.PullRequestSyncs.EnsureSync(ctx, readerRepository))
	openedAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	written, err := st.PullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
		Host:              "github.com",
		Forge:             "github",
		ProviderID:        "PR_740",
		URL:               readerRepository + "/pull/740",
		RepositoryURL:     readerRepository,
		HeadRepositoryURL: readerRepository,
		Number:            740,
		HeadBranch:        "agent/x",
		BaseBranch:        "main",
		State:             "open",
		OpenedAt:          openedAt,
		ChangedAt:         openedAt,
	})
	require.NoError(t, err)
	require.NoError(t, st.PullRequests.SetStanding(ctx, written.ID, forge.Standing{Decision: forge.ReviewDecisionApproved}, openedAt))
	_, err = st.PullRequests.Observe(ctx)
	require.NoError(t, err)

	rows, err := st.Rows.ListWatched(ctx, []string{opened.ID})
	require.NoError(t, err)

	surroundings, err := reader.ListSurroundings(ctx, rows)
	require.NoError(t, err)

	assert.Len(t, surroundings.Marks, 1, "every mark, whichever row it is on")
	assert.Len(t, surroundings.Mail, 1, "every unclaimed piece of mail, whoever it is to")
	assert.Equal(t, []string{written.ID}, xslice.Map(surroundings.PullRequests, func(p pullrequeststore.PullRequest) string {
		return p.ID
	}), "only the pull requests the agents on the page were seen with")
	assert.Equal(t, []string{readerRepository}, xslice.Map(surroundings.Repositories, func(s pullrequeststore.Sync) string {
		return s.RepositoryURL
	}), "only the repositories the agents on the page work in")
	assert.Equal(t, []string{opened.WorkflowID}, xslice.Map(surroundings.Workflows, func(w workflowstore.Workflow) string {
		return w.ID
	}), "only the workflows the agents on the page are in, never %s", bystander.ID)
	assert.Empty(t, surroundings.Reviews)

	nothing, err := reader.ListSurroundings(ctx, []rowstore.Row{})
	require.NoError(t, err)
	assert.Empty(t, nothing.PullRequests, "a page of nobody reads no pull request")
	assert.Empty(t, nothing.Workflows)
}

// seedAgentOnBranch is a live agent whose checkout is on a branch of a repository.
func seedAgentOnBranch(t *testing.T, st *storetest.Stores, name, repositoryURL, branch string) *agentstore.Agent {
	t.Helper()

	agent := st.SeedAgent(t, agentstore.Agent{Name: name})
	require.NoError(t, st.Agents.SetCheckout(t.Context(), agent.ID, agentstore.SetCheckoutParams{
		RepositoryURL: &repositoryURL,
		Branch:        &branch,
	}))

	return agent
}

// TestListWatchedByKeyReadsNothingWithoutKeys pins that an empty key list never falls through to the snapshot.
func TestListWatchedByKeyReadsNothingWithoutKeys(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantCalled bool
		want       []string
	}{
		{
			name:       "nil keys never read the snapshot",
			keys:       nil,
			wantCalled: false,
			want:       nil,
		},
		{
			name:       "an empty key list never reads the snapshot",
			keys:       []string{},
			wantCalled: false,
			want:       nil,
		},
		{
			name:       "keys are read by key",
			keys:       []string{"a", "b"},
			wantCalled: true,
			want:       []string{"a", "b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := &keyEchoingLister{}

			got, err := listWatchedByKey(t.Context(), lister, tt.keys)
			require.NoError(t, err)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantCalled, lister.called)
		})
	}
}

// keyEchoingLister answers a read with the keys it was given.
type keyEchoingLister struct {
	called bool
}

func (l *keyEchoingLister) ListWatched(_ context.Context, keys []string) ([]string, error) {
	l.called = true

	return keys, nil
}
