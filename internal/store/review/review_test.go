package review

import (
	"testing"

	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnreadClearsWhenTheAgentReads pins that a thread stops reading as unread once the agent
// looks, which a second statement stamping activity after the message used to make impossible.
func TestUnreadClearsWhenTheAgentReads(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	openThread(t, st, agentID, "this needs a nil check")

	looked, err := st.ListThreads(ctx, ListThreadsParams{AgentID: agentID, Reader: AuthorAgent})
	require.NoError(t, err)
	require.Len(t, looked, 1)
	assert.True(t, looked[0].UnreadByAgent, "a thread the agent has never read is unread")

	claimed, err := st.ListThreads(ctx, ListThreadsParams{
		AgentID:  agentID,
		Reader:   AuthorAgent,
		MarkRead: true,
	})
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	assert.True(t, claimed[0].UnreadByAgent, "the claiming read is still shown what it claimed")

	after, err := st.ListThreads(ctx, ListThreadsParams{AgentID: agentID, Reader: AuthorAgent})
	require.NoError(t, err)
	require.Len(t, after, 1)
	assert.False(t, after[0].UnreadByAgent, "reading it clears it")
}

// TestRewritingAMessageIsNewActivity pins that an edit reaches the other side, which created_at
// could never show since rewriting a body does not move when it was written.
func TestRewritingAMessageIsNewActivity(t *testing.T) {
	tests := []struct {
		name              string
		editAs            AuthorKind
		wantUnreadByAgent bool
		wantUnreadByHuman bool
	}{
		{
			name:              "the person rewriting their note is unread by the agent",
			editAs:            AuthorHuman,
			wantUnreadByAgent: true,
			wantUnreadByHuman: false,
		},
		{
			name:              "the agent rewriting its reply is unread by the person",
			editAs:            AuthorAgent,
			wantUnreadByAgent: false,
			wantUnreadByHuman: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()
			agentID := seedAgent(t, st)

			thread := openThread(t, st, agentID, "this needs a nil check")

			asked, err := st.ListMessages(ctx, []string{thread.ID})
			require.NoError(t, err)
			require.Len(t, asked, 1)

			answered, err := st.AddMessage(ctx, AddMessageParams{
				ThreadID:      thread.ID,
				AuthorKind:    AuthorAgent,
				AuthorAgentID: &agentID,
				Body:          "added one",
			})
			require.NoError(t, err)

			catchUp(t, st, agentID)

			edited := asked[0].ID
			if tt.editAs == AuthorAgent {
				edited = answered.ID
			}

			_, err = st.EditMessage(ctx, edited, "rewritten")
			require.NoError(t, err)

			threads, err := st.ListThreads(ctx, ListThreadsParams{AgentID: agentID, Reader: AuthorAgent})
			require.NoError(t, err)
			require.Len(t, threads, 1)

			assert.Equal(t, tt.wantUnreadByAgent, threads[0].UnreadByAgent)
			assert.Equal(t, tt.wantUnreadByHuman, threads[0].UnreadByHuman)
		})
	}
}

// TestMarkThreadReadCatchesUpOneSide proves opening one conversation does not claim the rest,
// which is the difference between the person having read a thread and having seen the list it
// sits in.
func TestMarkThreadReadCatchesUpOneSide(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	first := openThread(t, st, agentID, "first note")
	second := openThread(t, st, agentID, "second note")

	for _, thread := range []*Thread{first, second} {
		_, err := st.AddMessage(ctx, AddMessageParams{
			ThreadID:      thread.ID,
			AuthorKind:    AuthorAgent,
			AuthorAgentID: &agentID,
			Body:          "done",
		})
		require.NoError(t, err)
	}

	_, err := st.MarkThreadRead(ctx, first.ID, AuthorHuman)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, ListThreadsParams{AgentID: agentID, Reader: AuthorHuman})
	require.NoError(t, err)
	require.Len(t, threads, 2)

	byID := map[string]Thread{}
	for _, thread := range threads {
		byID[thread.ID] = thread
	}

	assert.False(t, byID[first.ID].UnreadByHuman, "the thread that was opened is caught up")
	assert.True(t, byID[second.ID].UnreadByHuman, "the one that was not stays unread")
	assert.True(t, byID[first.ID].UnreadByAgent, "the agent's side is untouched by the person reading")
}

// TestCountUnreadForAgent pins how much review an agent still has to read, which is what the
// notice is raised from.
func TestCountUnreadForAgent(t *testing.T) {
	tests := []struct {
		name      string
		threads   int
		agentRead bool
		want      int64
	}{
		{
			name: "an agent with no review has nothing to read",
			want: 0,
		},
		{
			name:      "a thread the agent has already read is not counted again",
			threads:   1,
			agentRead: true,
			want:      0,
		},
		{
			name:    "open threads with activity are counted",
			threads: 2,
			want:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()
			agentID := seedAgent(t, st)

			for range tt.threads {
				openThread(t, st, agentID, "note")
			}

			if tt.agentRead {
				_, err := st.ListThreads(ctx, ListThreadsParams{
					AgentID:  agentID,
					Reader:   AuthorAgent,
					MarkRead: true,
				})
				require.NoError(t, err)
			}

			got, err := st.CountUnreadForAgent(ctx, agentID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMissingRowsAnswerNotFound keeps the store's promise that an id naming nothing is an empty
// answer rather than an error, which is what lets the service above it say not found instead
// of leaking an internal failure.
func TestMissingRowsAnswerNotFound(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{
			name: "a string that is not an id at all",
			id:   "not-an-id",
		},
		{
			name: "a well formed id naming nothing",
			id:   uuid.NewString(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)
			ctx := t.Context()

			_, err := st.GetThread(ctx, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)

			_, err = st.GetMessage(ctx, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)

			_, err = st.MarkThreadRead(ctx, tt.id, AuthorHuman)
			require.ErrorIs(t, err, store.ErrNotFound)

			_, err = st.EditMessage(ctx, tt.id, "rewritten")
			assert.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

type fixture struct {
	*Store

	agents   *agentstore.Store
	workflow string
}

func open(t *testing.T) *fixture {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	workflows := workflowstore.New(p.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{Name: "default"})
	require.NoError(t, err)

	return &fixture{
		Store:    New(p.GetDB()),
		agents:   agentstore.New(p.GetDB()),
		workflow: created.ID,
	}
}

func seedAgent(t *testing.T, st *fixture) string {
	t.Helper()
	ctx := t.Context()
	const name = "worker"
	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name: name, State: agentstore.StateIdle, WorkflowID: st.workflow,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func openThread(t *testing.T, st *fixture, agentID, body string) *Thread {
	t.Helper()

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		AgentID:     agentID,
		FilePath:    "internal/thing.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        body,
		AuthorKind:  AuthorHuman,
	})
	require.NoError(t, err)
	require.NotNil(t, thread)

	return thread
}

// catchUp reads both sides up to date, so what a test asserts afterwards is only what the
// step it is testing changed.
func catchUp(t *testing.T, st *fixture, agentID string) {
	t.Helper()

	for _, reader := range []AuthorKind{AuthorAgent, AuthorHuman} {
		_, err := st.ListThreads(t.Context(), ListThreadsParams{
			AgentID:  agentID,
			Reader:   reader,
			MarkRead: true,
		})
		require.NoError(t, err)
	}
}

// TestSettlingAThreadTakesItOutOfWhatIsOwed pins that settling is the reviewer's judgement
// alone, so the agent is not told about a settled thread however it has been read or rewritten.
func TestSettlingAThreadTakesItOutOfWhatIsOwed(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	owed, err := st.CountUnreadForAgent(ctx, agentID)
	require.NoError(t, err)
	require.Equal(t, int64(1), owed)

	settled, err := st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)
	require.NotNil(t, settled)
	assert.NotNil(t, settled.ResolvedAt)

	owed, err = st.CountUnreadForAgent(ctx, agentID)
	require.NoError(t, err)
	assert.Zero(t, owed, "a settled thread is not something the agent still owes an answer to")

	// The panel draws from the list and nothing else, so a thread that is settled in the table
	// and not settled in the list is settled nowhere that anybody can see.
	listed, err := st.ListThreads(ctx, ListThreadsParams{AgentID: agentID, Reader: AuthorHuman})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].ResolvedAt, "the list carries the settling")

	// The doorbell and the agent's own listing have to agree, since a thread the count has
	// stopped mentioning but the list still flags as owed is one the agent answers and reopens.
	asAgent, err := st.ListThreads(ctx, ListThreadsParams{AgentID: agentID, Reader: AuthorAgent})
	require.NoError(t, err)
	require.Len(t, asAgent, 1)
	assert.False(t, asAgent[0].UnreadByAgent, "a settled thread is not owed an answer either way")

	reopened, err := st.SetThreadResolved(ctx, thread.ID, false)
	require.NoError(t, err)
	require.NotNil(t, reopened)
	assert.Nil(t, reopened.ResolvedAt)

	owed, err = st.CountUnreadForAgent(ctx, agentID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), owed, "reopening puts it back")
}

// TestSayingSomethingElseReopensAThread pins that a settled conversation somebody has since
// written in is a conversation again, whichever side wrote.
func TestSayingSomethingElseReopensAThread(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	_, err := st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)

	said, err := st.AddMessage(ctx, AddMessageParams{
		ThreadID:   thread.ID,
		Body:       "one more thing",
		AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	again, err := st.GetThread(ctx, thread.ID)
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Nil(t, again.ResolvedAt)

	// Rewriting a message is the same news to the other side as writing one, so leaving it
	// settled would hide an edit nobody has read.
	_, err = st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)

	_, err = st.EditMessage(ctx, said.ID, "one more thing, rewritten")
	require.NoError(t, err)

	edited, err := st.GetThread(ctx, thread.ID)
	require.NoError(t, err)
	require.NotNil(t, edited)
	assert.Nil(t, edited.ResolvedAt, "an edit reopens it too")
}
