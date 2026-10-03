package review

import (
	"context"
	"slices"
	"testing"

	"komrad/internal/forge"
	"komrad/internal/store"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replyThreads is an agent with one thread mirrored from the forge and one opened in komrad.
type replyThreads struct {
	agentID  string
	mirrored string
	local    string
}

func (st *fixture) seedReplyThreads(t *testing.T) replyThreads {
	t.Helper()
	ctx := t.Context()

	agentID := st.seedAgent(t, "worker")
	pullRequestID := st.seedPullRequest(t, agentID)
	forgeThreads := pullrequeststore.NewForgeThreadStore(st.pool)
	require.NoError(t, forgeThreads.SetThreads(ctx, pullrequeststore.SetThreadsParams{
		PullRequestID: pullRequestID,
		Viewer:        "harrisoncramer",
		Threads: []forge.ReviewThread{{
			ProviderID: "PRRT_1",
			Path:       "internal/thing.go",
			Line:       41,
			CommitSHA:  "abc123",
			URL:        "https://github.com/harrisoncramer/komrad/pull/1#discussion_r1",
		}},
	}))
	stored, err := forgeThreads.ListOfPullRequest(ctx, pullRequestID)
	require.NoError(t, err)
	require.Len(t, stored, 1)

	round := st.openRound(t, agentID, "tree-one")
	mirrored, err := st.CreateThread(ctx, CreateThreadParams{
		AgentID:       agentID,
		ReviewID:      st.openForgeReview(t, round, "alice").ID,
		RoundID:       round.ID,
		FilePath:      "internal/thing.go",
		NewStart:      41,
		NewEnd:        41,
		QuotedLines:   []string{"+\tdoThing()"},
		Body:          "why this?",
		AuthorKind:    AuthorForge,
		ForgeThreadID: &stored[0].ID,
	})
	require.NoError(t, err)

	local, err := st.CreateThread(ctx, CreateThreadParams{
		AgentID:     agentID,
		ReviewID:    st.openReview(t, round).ID,
		RoundID:     round.ID,
		FilePath:    "internal/thing.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        "and this?",
		AuthorKind:  AuthorHuman,
	})
	require.NoError(t, err)

	return replyThreads{
		agentID:  agentID,
		mirrored: mirrored.ID,
		local:    local.ID,
	}
}

func (st *fixture) propose(t *testing.T, threads replyThreads, body string) *forgeReply {
	t.Helper()

	reply, err := st.CreateAgentForgeReply(t.Context(), CreateAgentForgeReplyParams{
		ThreadID: threads.mirrored,
		AgentID:  threads.agentID,
		Body:     body,
	})
	require.NoError(t, err)

	return reply
}

// proposeIn drafts an agent reply and moves it along the happy path until it reaches state.
func (st *fixture) proposeIn(t *testing.T, threads replyThreads, state forgeReplyState) *forgeReply {
	t.Helper()
	ctx := t.Context()

	reply := st.propose(t, threads, "proposed")
	steps := map[forgeReplyState]func() (*forgeReply, error){
		forgeReplyApproved: func() (*forgeReply, error) {
			return st.MarkForgeReplyApproved(ctx, MarkForgeReplyApprovedParams{
				ID:           reply.ID,
				Body:         "approved",
				ApproverKind: forgeReplyApproverHuman,
			})
		},
		forgeReplySending: func() (*forgeReply, error) { return st.BeginForgeReplySend(ctx, reply.ID) },
		forgeReplySent:    func() (*forgeReply, error) { return st.MarkForgeReplySent(ctx, reply.ID, "PRRC_1") },
		forgeReplyFailed:  func() (*forgeReply, error) { return st.MarkForgeReplyFailed(ctx, reply.ID, "forbidden") },
	}
	path := map[forgeReplyState][]forgeReplyState{
		forgeReplyDraft:    nil,
		forgeReplyApproved: {forgeReplyApproved},
		forgeReplySending:  {forgeReplyApproved, forgeReplySending},
		forgeReplySent:     {forgeReplyApproved, forgeReplySending, forgeReplySent},
		forgeReplyFailed:   {forgeReplyApproved, forgeReplySending, forgeReplyFailed},
	}
	for _, step := range path[state] {
		var err error
		reply, err = steps[step]()
		require.NoError(t, err)
	}
	require.Equal(t, state, reply.State)

	return reply
}

// TestForgeReplyTransitions covers every move against every state, allowing only the ones the reply's life permits.
func TestForgeReplyTransitions(t *testing.T) {
	tests := []struct {
		name  string
		from  []forgeReplyState
		to    forgeReplyState
		apply func(ctx context.Context, st *fixture, id string) (forgeReplyState, error)
	}{
		{
			name: "approve",
			from: []forgeReplyState{forgeReplyDraft},
			to:   forgeReplyApproved,
			apply: func(ctx context.Context, st *fixture, id string) (forgeReplyState, error) {
				reply, err := st.MarkForgeReplyApproved(ctx, MarkForgeReplyApprovedParams{
					ID:           id,
					Body:         "edited",
					ApproverKind: forgeReplyApproverHuman,
				})

				return stateOf(reply), err
			},
		},
		{
			name: "begin sending",
			from: []forgeReplyState{forgeReplyApproved, forgeReplyFailed},
			to:   forgeReplySending,
			apply: func(ctx context.Context, st *fixture, id string) (forgeReplyState, error) {
				reply, err := st.BeginForgeReplySend(ctx, id)
				return stateOf(reply), err
			},
		},
		{
			name: "mark sent",
			from: []forgeReplyState{forgeReplySending},
			to:   forgeReplySent,
			apply: func(ctx context.Context, st *fixture, id string) (forgeReplyState, error) {
				reply, err := st.MarkForgeReplySent(ctx, id, "PRRC_2")
				return stateOf(reply), err
			},
		},
		{
			name: "mark failed",
			from: []forgeReplyState{forgeReplySending},
			to:   forgeReplyFailed,
			apply: func(ctx context.Context, st *fixture, id string) (forgeReplyState, error) {
				reply, err := st.MarkForgeReplyFailed(ctx, id, "rate limited")
				return stateOf(reply), err
			},
		},
		{
			name: "discard",
			from: []forgeReplyState{forgeReplyDraft, forgeReplyFailed},
			apply: func(ctx context.Context, st *fixture, id string) (forgeReplyState, error) {
				return "", st.DeleteForgeReply(ctx, id)
			},
		},
	}
	states := []forgeReplyState{forgeReplyDraft, forgeReplyApproved, forgeReplySending, forgeReplySent, forgeReplyFailed}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, from := range states {
				t.Run(string(from), func(t *testing.T) {
					st := openFixture(t)
					ctx := t.Context()
					reply := st.proposeIn(t, st.seedReplyThreads(t), from)

					got, err := tt.apply(ctx, st, reply.ID)
					if !slices.Contains(tt.from, from) {
						require.ErrorIs(t, err, errReplyState)
						after, err := st.GetForgeReply(ctx, reply.ID)
						require.NoError(t, err)
						assert.Equal(t, from, after.State)

						return
					}

					require.NoError(t, err)
					assert.Equal(t, tt.to, got)
					if tt.to == "" {
						_, err := st.GetForgeReply(ctx, reply.ID)
						assert.ErrorIs(t, err, store.ErrNotFound)
					}
				})
			}
		})
	}
}

// TestForgeReplyRecords covers what each step leaves on the reply.
func TestForgeReplyRecords(t *testing.T) {
	tests := []struct {
		name  string
		reach func(t *testing.T, st *fixture, threads replyThreads) *forgeReply
		check func(t *testing.T, reply *forgeReply)
	}{
		{
			name: "the agent's proposal is a draft with its text kept apart from the text to post",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				return st.propose(t, threads, "proposed")
			},
			check: func(t *testing.T, reply *forgeReply) {
				assert.Equal(t, forgeReplyDraft, reply.State)
				assert.Equal(t, AuthorAgent, reply.AuthorKind)
				require.NotNil(t, reply.AuthorAgentID)
				require.NotNil(t, reply.ProposedBody)
				assert.Equal(t, "proposed", *reply.ProposedBody)
				assert.Equal(t, "proposed", reply.Body)
				assert.Empty(t, reply.ApproverKind)
				assert.Nil(t, reply.ApprovedAt)
			},
		},
		{
			name: "the person's own reply is approved as it is written",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				reply, err := st.CreateHumanForgeReply(t.Context(), threads.mirrored, "mine")
				require.NoError(t, err)

				return reply
			},
			check: func(t *testing.T, reply *forgeReply) {
				assert.Equal(t, forgeReplyApproved, reply.State)
				assert.Equal(t, AuthorHuman, reply.AuthorKind)
				assert.Nil(t, reply.AuthorAgentID)
				assert.Nil(t, reply.ProposedBody)
				assert.Equal(t, "mine", reply.Body)
				assert.Equal(t, forgeReplyApproverHuman, reply.ApproverKind)
				assert.NotNil(t, reply.ApprovedAt)
			},
		},
		{
			name: "approving with an edit keeps the agent's proposal",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				return st.proposeIn(t, threads, forgeReplyApproved)
			},
			check: func(t *testing.T, reply *forgeReply) {
				require.NotNil(t, reply.ProposedBody)
				assert.Equal(t, "proposed", *reply.ProposedBody)
				assert.Equal(t, "approved", reply.Body)
				assert.Equal(t, forgeReplyApproverHuman, reply.ApproverKind)
				assert.NotNil(t, reply.ApprovedAt)
			},
		},
		{
			name: "a sent reply holds the forge's comment id",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				return st.proposeIn(t, threads, forgeReplySent)
			},
			check: func(t *testing.T, reply *forgeReply) {
				require.NotNil(t, reply.ForgeCommentProviderID)
				assert.Equal(t, "PRRC_1", *reply.ForgeCommentProviderID)
				assert.NotNil(t, reply.SendingStartedAt)
				assert.NotNil(t, reply.SentAt)
				assert.Nil(t, reply.Error)
			},
		},
		{
			name: "a failed reply holds the forge's error",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				return st.proposeIn(t, threads, forgeReplyFailed)
			},
			check: func(t *testing.T, reply *forgeReply) {
				require.NotNil(t, reply.Error)
				assert.Equal(t, "forbidden", *reply.Error)
				assert.NotNil(t, reply.SendingStartedAt)
				assert.Nil(t, reply.SentAt)
				assert.Nil(t, reply.ForgeCommentProviderID)
			},
		},
		{
			name: "a proposal outlives the agent that wrote it",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				ctx := t.Context()
				author := st.seedAgent(t, "helper")
				proposed, err := st.CreateAgentForgeReply(ctx, CreateAgentForgeReplyParams{
					ThreadID: threads.mirrored,
					AgentID:  author,
					Body:     "proposed",
				})
				require.NoError(t, err)
				_, err = st.pool.Exec(ctx, "DELETE FROM agents WHERE id = $1", author)
				require.NoError(t, err)

				reply, err := st.GetForgeReply(ctx, proposed.ID)
				require.NoError(t, err)

				return reply
			},
			check: func(t *testing.T, reply *forgeReply) {
				assert.Equal(t, AuthorAgent, reply.AuthorKind)
				assert.Nil(t, reply.AuthorAgentID)
				require.NotNil(t, reply.ProposedBody)
				assert.Equal(t, "proposed", *reply.ProposedBody)
			},
		},
		{
			name: "retrying a failed reply clears its error",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *forgeReply {
				failed := st.proposeIn(t, threads, forgeReplyFailed)
				reply, err := st.BeginForgeReplySend(t.Context(), failed.ID)
				require.NoError(t, err)

				return reply
			},
			check: func(t *testing.T, reply *forgeReply) {
				assert.Equal(t, forgeReplySending, reply.State)
				assert.Nil(t, reply.Error)
				assert.NotNil(t, reply.SendingStartedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			reply := tt.reach(t, st, st.seedReplyThreads(t))
			tt.check(t, reply)

			read, err := st.GetForgeReply(t.Context(), reply.ID)
			require.NoError(t, err)
			assert.Equal(t, reply, read)
		})
	}
}

// TestForgeReplyThreads covers which threads take a reply and how many open proposals each holds.
func TestForgeReplyThreads(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, st *fixture, threads replyThreads) error
		want  error
	}{
		{
			name: "the agent cannot propose on a thread opened in komrad",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				_, err := st.CreateAgentForgeReply(t.Context(), CreateAgentForgeReplyParams{
					ThreadID: threads.local,
					AgentID:  threads.agentID,
					Body:     "hi",
				})

				return err
			},
			want: errNotMirrored,
		},
		{
			name: "the person cannot reply on a thread opened in komrad",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				_, err := st.CreateHumanForgeReply(t.Context(), threads.local, "hi")

				return err
			},
			want: errNotMirrored,
		},
		{
			name: "a thread that does not exist takes no reply",
			write: func(t *testing.T, st *fixture, _ replyThreads) error {
				_, err := st.CreateHumanForgeReply(t.Context(), "01a10000-0000-7000-8000-000000000000", "hi")

				return err
			},
			want: errNotMirrored,
		},
		{
			name: "a second proposal waits on a draft",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				st.propose(t, threads, "first")
				_, err := st.CreateAgentForgeReply(t.Context(), CreateAgentForgeReplyParams{
					ThreadID: threads.mirrored,
					AgentID:  threads.agentID,
					Body:     "second",
				})

				return err
			},
			want: errProposalOpen,
		},
		{
			name: "a second proposal waits on a failed reply until it is discarded",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				st.proposeIn(t, threads, forgeReplyFailed)
				_, err := st.CreateAgentForgeReply(t.Context(), CreateAgentForgeReplyParams{
					ThreadID: threads.mirrored,
					AgentID:  threads.agentID,
					Body:     "second",
				})

				return err
			},
			want: errProposalOpen,
		},
		{
			name: "a proposal follows one that was sent",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				st.proposeIn(t, threads, forgeReplySent)
				_, err := st.CreateAgentForgeReply(t.Context(), CreateAgentForgeReplyParams{
					ThreadID: threads.mirrored,
					AgentID:  threads.agentID,
					Body:     "second",
				})

				return err
			},
		},
		{
			name: "a proposal follows one that was discarded",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				require.NoError(t, st.DeleteForgeReply(t.Context(), st.propose(t, threads, "first").ID))
				_, err := st.CreateAgentForgeReply(t.Context(), CreateAgentForgeReplyParams{
					ThreadID: threads.mirrored,
					AgentID:  threads.agentID,
					Body:     "second",
				})

				return err
			},
		},
		{
			name: "the person replies beside the agent's open proposal",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				st.propose(t, threads, "first")
				_, err := st.CreateHumanForgeReply(t.Context(), threads.mirrored, "mine")

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			err := tt.write(t, st, st.seedReplyThreads(t))
			if tt.want == nil {
				require.NoError(t, err)

				return
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}

// TestForgeReplyNotFound covers every call on a reply that does not exist.
func TestForgeReplyNotFound(t *testing.T) {
	calls := map[string]func(ctx context.Context, st *fixture, id string) error{
		"get": func(ctx context.Context, st *fixture, id string) error {
			_, err := st.GetForgeReply(ctx, id)

			return err
		},
		"approve": func(ctx context.Context, st *fixture, id string) error {
			_, err := st.MarkForgeReplyApproved(ctx, MarkForgeReplyApprovedParams{
				ID:           id,
				Body:         "x",
				ApproverKind: forgeReplyApproverHuman,
			})

			return err
		},
		"begin sending": func(ctx context.Context, st *fixture, id string) error {
			_, err := st.BeginForgeReplySend(ctx, id)

			return err
		},
		"mark sent": func(ctx context.Context, st *fixture, id string) error {
			_, err := st.MarkForgeReplySent(ctx, id, "PRRC_1")

			return err
		},
		"mark failed": func(ctx context.Context, st *fixture, id string) error {
			_, err := st.MarkForgeReplyFailed(ctx, id, "x")

			return err
		},
		"discard": func(ctx context.Context, st *fixture, id string) error {
			return st.DeleteForgeReply(ctx, id)
		},
	}

	st := openFixture(t)
	for name, call := range calls {
		for _, id := range []string{"01a10000-0000-7000-8000-000000000000", "not-an-id"} {
			t.Run(name+" "+id, func(t *testing.T) {
				assert.ErrorIs(t, call(t.Context(), st, id), store.ErrNotFound)
			})
		}
	}
}

// TestListForgeReplies covers listing the replies of threads and the replies left mid-send.
func TestListForgeReplies(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	threads := st.seedReplyThreads(t)

	sending := st.proposeIn(t, threads, forgeReplySending)
	written, err := st.CreateHumanForgeReply(ctx, threads.mirrored, "mine")
	require.NoError(t, err)

	listed, err := st.ListForgeReplies(ctx, []string{threads.mirrored, threads.local})
	require.NoError(t, err)
	require.Len(t, listed, 2)
	assert.Equal(t, sending.ID, listed[0].ID)
	assert.Equal(t, written.ID, listed[1].ID)

	empty, err := st.ListForgeReplies(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, empty)

	stuck, err := st.ListSendingForgeReplies(ctx)
	require.NoError(t, err)
	require.Len(t, stuck, 1)
	assert.Equal(t, sending.ID, stuck[0].ID)
}

// TestForgeReplyChecks covers the rows the schema refuses however they are written.
func TestForgeReplyChecks(t *testing.T) {
	tests := []struct {
		name   string
		state  forgeReplyState
		update string
	}{
		{
			name:   "the agent is never an approver",
			state:  forgeReplyDraft,
			update: "state = 'approved', approver_kind = 'agent', approved_at = NOW()",
		},
		{
			name:   "an approved reply names its approver",
			state:  forgeReplyDraft,
			update: "state = 'approved'",
		},
		{
			name:   "a draft has no approver",
			state:  forgeReplyApproved,
			update: "state = 'draft'",
		},
		{
			name:   "a reply being sent says when sending began",
			state:  forgeReplyApproved,
			update: "state = 'sending'",
		},
		{
			name:   "a sent reply holds the forge's comment id",
			state:  forgeReplySending,
			update: "state = 'sent', sent_at = NOW()",
		},
		{
			name:   "a failed reply holds the forge's error",
			state:  forgeReplySending,
			update: "state = 'failed'",
		},
		{
			name:   "a reply has text to post",
			state:  forgeReplyDraft,
			update: "body = ''",
		},
		{
			name:   "the person's reply has no proposal",
			state:  forgeReplyDraft,
			update: "author_kind = 'human', author_agent_id = NULL",
		},
		{
			name:   "the forge never writes a reply",
			state:  forgeReplyDraft,
			update: "author_kind = 'forge'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			reply := st.proposeIn(t, st.seedReplyThreads(t), tt.state)

			_, err := st.pool.Exec(t.Context(), "UPDATE review_forge_replies SET "+tt.update+" WHERE id = $1", reply.ID)
			require.Error(t, err)
		})
	}
}

// TestForgeReplyEchoGuard covers two replies on a thread claiming the same forge comment.
func TestForgeReplyEchoGuard(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	threads := st.seedReplyThreads(t)

	st.proposeIn(t, threads, forgeReplySent)
	written, err := st.CreateHumanForgeReply(ctx, threads.mirrored, "mine")
	require.NoError(t, err)
	_, err = st.BeginForgeReplySend(ctx, written.ID)
	require.NoError(t, err)

	_, err = st.MarkForgeReplySent(ctx, written.ID, "PRRC_1")
	require.Error(t, err)
	_, err = st.MarkForgeReplySent(ctx, written.ID, "PRRC_2")
	require.NoError(t, err)
}

func stateOf(reply *forgeReply) forgeReplyState {
	if reply == nil {
		return ""
	}

	return reply.State
}
