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
	agentID       string
	pullRequestID string
	mirrored      string
	local         string
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

	snapshot := st.openSnapshot(t, agentID, "tree-one")
	mirrored, err := st.CreateThread(ctx, CreateThreadParams{
		TaskID:        st.taskOf(t, agentID),
		ReviewID:      st.openForgeReview(t, snapshot, "alice").ID,
		SnapshotID:    snapshot.ID,
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
		TaskID:      st.taskOf(t, agentID),
		ReviewID:    st.openReview(t, snapshot).ID,
		SnapshotID:  snapshot.ID,
		FilePath:    "internal/thing.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        "and this?",
		AuthorKind:  AuthorHuman,
	})
	require.NoError(t, err)

	return replyThreads{
		agentID:       agentID,
		pullRequestID: pullRequestID,
		mirrored:      mirrored.ID,
		local:         local.ID,
	}
}

// writeIn writes the person's reply and moves it along the happy path until it reaches state.
func (st *fixture) writeIn(t *testing.T, threads replyThreads, state ForgeReplyState) *ForgeReply {
	t.Helper()
	ctx := t.Context()

	reply, err := st.CreateForgeReply(ctx, threads.mirrored, "mine")
	require.NoError(t, err)
	steps := map[ForgeReplyState]func() (*ForgeReply, error){
		ForgeReplySending: func() (*ForgeReply, error) { return st.BeginForgeReplySend(ctx, reply.ID) },
		ForgeReplySent:    func() (*ForgeReply, error) { return st.MarkForgeReplySent(ctx, reply.ID, "PRRC_1") },
		ForgeReplyFailed:  func() (*ForgeReply, error) { return st.MarkForgeReplyFailed(ctx, reply.ID, "forbidden") },
	}
	path := map[ForgeReplyState][]ForgeReplyState{
		ForgeReplyApproved: nil,
		ForgeReplySending:  {ForgeReplySending},
		ForgeReplySent:     {ForgeReplySending, ForgeReplySent},
		ForgeReplyFailed:   {ForgeReplySending, ForgeReplyFailed},
	}
	for _, step := range path[state] {
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
		from  []ForgeReplyState
		to    ForgeReplyState
		apply func(ctx context.Context, st *fixture, id string) (ForgeReplyState, error)
	}{
		{
			name: "begin sending",
			from: []ForgeReplyState{ForgeReplyApproved, ForgeReplyFailed},
			to:   ForgeReplySending,
			apply: func(ctx context.Context, st *fixture, id string) (ForgeReplyState, error) {
				reply, err := st.BeginForgeReplySend(ctx, id)
				return stateOf(reply), err
			},
		},
		{
			name: "mark sent",
			from: []ForgeReplyState{ForgeReplySending},
			to:   ForgeReplySent,
			apply: func(ctx context.Context, st *fixture, id string) (ForgeReplyState, error) {
				reply, err := st.MarkForgeReplySent(ctx, id, "PRRC_2")
				return stateOf(reply), err
			},
		},
		{
			name: "mark failed",
			from: []ForgeReplyState{ForgeReplySending},
			to:   ForgeReplyFailed,
			apply: func(ctx context.Context, st *fixture, id string) (ForgeReplyState, error) {
				reply, err := st.MarkForgeReplyFailed(ctx, id, "rate limited")
				return stateOf(reply), err
			},
		},
		{
			name: "discard",
			from: []ForgeReplyState{ForgeReplyFailed},
			apply: func(ctx context.Context, st *fixture, id string) (ForgeReplyState, error) {
				return "", st.DeleteForgeReply(ctx, id)
			},
		},
	}
	states := []ForgeReplyState{ForgeReplyApproved, ForgeReplySending, ForgeReplySent, ForgeReplyFailed}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, from := range states {
				t.Run(string(from), func(t *testing.T) {
					st := openFixture(t)
					ctx := t.Context()
					reply := st.writeIn(t, st.seedReplyThreads(t), from)

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
		reach func(t *testing.T, st *fixture, threads replyThreads) *ForgeReply
		check func(t *testing.T, reply *ForgeReply)
	}{
		{
			name: "the person's own reply is approved as it is written",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *ForgeReply {
				reply, err := st.CreateForgeReply(t.Context(), threads.mirrored, "mine")
				require.NoError(t, err)

				return reply
			},
			check: func(t *testing.T, reply *ForgeReply) {
				assert.Equal(t, ForgeReplyApproved, reply.State)
				assert.Equal(t, "mine", reply.Body)
				assert.NotNil(t, reply.ApprovedAt)
			},
		},
		{
			name: "a sent reply holds the forge's comment id",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *ForgeReply {
				return st.writeIn(t, threads, ForgeReplySent)
			},
			check: func(t *testing.T, reply *ForgeReply) {
				require.NotNil(t, reply.ForgeCommentProviderID)
				assert.Equal(t, "PRRC_1", *reply.ForgeCommentProviderID)
				assert.NotNil(t, reply.SendingStartedAt)
				assert.NotNil(t, reply.SentAt)
				assert.Nil(t, reply.Error)
			},
		},
		{
			name: "a failed reply holds the forge's error",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *ForgeReply {
				return st.writeIn(t, threads, ForgeReplyFailed)
			},
			check: func(t *testing.T, reply *ForgeReply) {
				require.NotNil(t, reply.Error)
				assert.Equal(t, "forbidden", *reply.Error)
				assert.NotNil(t, reply.SendingStartedAt)
				assert.Nil(t, reply.SentAt)
				assert.Nil(t, reply.ForgeCommentProviderID)
			},
		},
		{
			name: "retrying a failed reply clears its error",
			reach: func(t *testing.T, st *fixture, threads replyThreads) *ForgeReply {
				failed := st.writeIn(t, threads, ForgeReplyFailed)
				reply, err := st.BeginForgeReplySend(t.Context(), failed.ID)
				require.NoError(t, err)

				return reply
			},
			check: func(t *testing.T, reply *ForgeReply) {
				assert.Equal(t, ForgeReplySending, reply.State)
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

// TestForgeReplyThreads covers which threads take a reply.
func TestForgeReplyThreads(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, st *fixture, threads replyThreads) error
		want  error
	}{
		{
			name: "the person cannot reply on a thread opened in komrad",
			write: func(t *testing.T, st *fixture, threads replyThreads) error {
				_, err := st.CreateForgeReply(t.Context(), threads.local, "hi")

				return err
			},
			want: errNotMirrored,
		},
		{
			name: "a thread that does not exist takes no reply",
			write: func(t *testing.T, st *fixture, _ replyThreads) error {
				_, err := st.CreateForgeReply(t.Context(), "01a10000-0000-7000-8000-000000000000", "hi")

				return err
			},
			want: errNotMirrored,
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

	sending := st.writeIn(t, threads, ForgeReplySending)
	written, err := st.CreateForgeReply(ctx, threads.mirrored, "mine")
	require.NoError(t, err)
	draft, err := st.CreateForgeReply(ctx, threads.mirrored, "proposed")
	require.NoError(t, err)
	_, err = st.pool.Exec(ctx, `UPDATE review_forge_replies
		SET state = 'draft', author_kind = 'agent', author_agent_id = $2, proposed_body = body, approver_kind = NULL, approved_at = NULL
		WHERE id = $1`, draft.ID, threads.agentID)
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

	t.Run("a sent reply carries the comment it came back as", func(t *testing.T) {
		tests := []struct {
			name       string
			providerID string
			synced     bool
		}{
			{
				name:       "not yet synced",
				providerID: "PRRC_unsynced",
				synced:     false,
			},
			{
				name:       "github",
				providerID: "PRRC_kwDOUgkrbM75GfCu",
				synced:     true,
			},
			{
				name:       "gitlab",
				providerID: "gid://gitlab/DiffNote/7",
				synced:     true,
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				st := openFixture(t)
				ctx := t.Context()
				threads := st.seedReplyThreads(t)

				reply := st.writeIn(t, threads, ForgeReplySending)
				_, err := st.MarkForgeReplySent(ctx, reply.ID, tt.providerID)
				require.NoError(t, err)

				forgeThreads := pullrequeststore.NewForgeThreadStore(st.pool)
				thread := forge.ReviewThread{
					ProviderID: "PRRT_1",
					Path:       "internal/thing.go",
					Line:       41,
					CommitSHA:  "abc123",
				}
				if tt.synced {
					thread.Comments = []forge.ReviewComment{{
						ProviderID: tt.providerID,
						Login:      "harrisoncramer",
						Body:       "proposed",
					}}
				}
				require.NoError(t, forgeThreads.SetThreads(ctx, pullrequeststore.SetThreadsParams{
					PullRequestID: threads.pullRequestID,
					Viewer:        "harrisoncramer",
					Threads:       []forge.ReviewThread{thread},
				}))

				listed, err := st.ListForgeReplies(ctx, []string{threads.mirrored})
				require.NoError(t, err)
				require.Len(t, listed, 1)
				if !tt.synced {
					assert.Nil(t, listed[0].MirroredCommentID)

					return
				}

				stored, err := forgeThreads.ListOfPullRequest(ctx, threads.pullRequestID)
				require.NoError(t, err)
				require.Len(t, stored, 1)
				require.Len(t, stored[0].Comments, 1)
				require.NotNil(t, listed[0].MirroredCommentID)
				assert.Equal(t, stored[0].Comments[0].ID, *listed[0].MirroredCommentID)
			})
		}
	})
}

// TestForgeReplyChecks covers the rows the schema refuses however they are written.
func TestForgeReplyChecks(t *testing.T) {
	tests := []struct {
		name   string
		state  ForgeReplyState
		update string
	}{
		{
			name:   "a reply being sent says when sending began",
			state:  ForgeReplyApproved,
			update: "state = 'sending'",
		},
		{
			name:   "a sent reply holds the forge's comment id",
			state:  ForgeReplySending,
			update: "state = 'sent', sent_at = NOW()",
		},
		{
			name:   "a failed reply holds the forge's error",
			state:  ForgeReplySending,
			update: "state = 'failed'",
		},
		{
			name:   "a reply has text to post",
			state:  ForgeReplyApproved,
			update: "body = ''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			reply := st.writeIn(t, st.seedReplyThreads(t), tt.state)

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

	st.writeIn(t, threads, ForgeReplySent)
	written, err := st.CreateForgeReply(ctx, threads.mirrored, "mine")
	require.NoError(t, err)
	_, err = st.BeginForgeReplySend(ctx, written.ID)
	require.NoError(t, err)

	_, err = st.MarkForgeReplySent(ctx, written.ID, "PRRC_1")
	require.Error(t, err)
	_, err = st.MarkForgeReplySent(ctx, written.ID, "PRRC_2")
	require.NoError(t, err)
}

func stateOf(reply *ForgeReply) ForgeReplyState {
	if reply == nil {
		return ""
	}

	return reply.State
}

// TestGetForgeReplyTarget covers finding the forge thread and pull request a mirrored thread replies to.
func TestGetForgeReplyTarget(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	threads := st.seedReplyThreads(t)

	target, err := st.GetForgeReplyTarget(ctx, threads.mirrored)
	require.NoError(t, err)
	assert.Equal(t, ForgeReplyTarget{
		ProviderID:    "PRRT_1",
		Path:          "internal/thing.go",
		RepositoryURL: "https://github.com/harrisoncramer/komrad",
		Number:        1,
	}, *target)

	for _, id := range []string{threads.local, "01a10000-0000-7000-8000-000000000000", "not-an-id"} {
		_, err := st.GetForgeReplyTarget(ctx, id)
		assert.ErrorIs(t, err, errNotMirrored, id)
	}
}
