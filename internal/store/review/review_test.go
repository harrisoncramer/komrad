package review

import (
	"testing"
	"time"

	"weave/internal/pgtest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	workflowstore "weave/internal/store/workflow"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReleasingStampsOnlyWhatThePersonHasNotSent covers pressing submit twice and the agent
// replying in between: a comment is released once, and the agent's own words are never
// released at all since nobody is waiting to be told about them.
func TestReleasingStampsOnlyWhatThePersonHasNotSent(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	reply, err := st.AddMessage(ctx, AddMessageParams{
		ThreadID:      thread.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &agentID,
		Body:          "done",
	})
	require.NoError(t, err)

	released, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, released, 1, "only the person's comment is released")
	assert.NotNil(t, released[0].SentAt)
	assert.Equal(t, DeliverySent, released[0].Delivery, "released with no doorbell yet raised")

	again, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	assert.Empty(t, again, "a comment already let go of is not released a second time")

	answered, err := st.GetMessage(ctx, reply.ID)
	require.NoError(t, err)
	assert.Nil(t, answered.SentAt)
	assert.Equal(t, DeliveryNone, answered.Delivery, "delivery says nothing about the agent's own words")

	written, err := st.AddMessage(ctx, AddMessageParams{
		ThreadID:   thread.ID,
		AuthorKind: AuthorHuman,
		Body:       "and another thing",
	})
	require.NoError(t, err)
	assert.Equal(t, DeliveryUnsent, written.Delivery)

	third, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, third, 1, "a comment written after the release is released by the next one")
	assert.Equal(t, written.ID, third[0].ID)
}

// TestRewritingAReleasedCommentLeavesItSent pins that an edit does not un-send a comment,
// since the doorbell carries no feedback and the agent reads the comment as it now stands.
func TestRewritingAReleasedCommentLeavesItSent(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	openThread(t, st, agentID, "this needs a nil check")

	released, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, released, 1)

	edited, err := st.EditMessage(ctx, released[0].ID, "this needs a nil check, and a test")
	require.NoError(t, err)
	require.NotNil(t, edited.SentAt)
	assert.Equal(t, DeliverySent, edited.Delivery)

	unsent, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	assert.Empty(t, unsent, "rewriting is not something new to release")
}

// TestDeliveryFollowsTheDoorbell walks a released comment through the states the panel draws
// from: waiting to be claimed, claimed, and run out of time unread.
func TestDeliveryFollowsTheDoorbell(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	released, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, released, 1)
	comment := released[0].ID

	assert.Equal(t, DeliveryExpired, delivery(t, st, thread.ID, comment),
		"a released comment with no doorbell at all is what ringing again reaches")

	doorbell := ring(t, st, agentID, comment, time.Hour)
	assert.Equal(t, DeliverySent, delivery(t, st, thread.ID, comment))

	expired, err := st.ListExpiredMessages(ctx, agentID)
	require.NoError(t, err)
	assert.Empty(t, expired, "a doorbell still waiting to be read is not rung again")

	_, err = st.mail.Claim(ctx, agentID, 10)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDelivered, delivery(t, st, thread.ID, comment))

	require.NoError(t, st.mail.Delete(ctx, []string{doorbell}))

	ring(t, st, agentID, comment, -time.Hour)
	assert.Equal(t, DeliveryExpired, delivery(t, st, thread.ID, comment))

	expired, err = st.ListExpiredMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, expired, 1, "a doorbell that ran out of time unread is rung again")
	assert.Equal(t, comment, expired[0].ID)
}

// TestRingingAgainReusesTheDoorbellOfTheSameComment pins the unique index behind one waiting
// doorbell per comment: an expired one is rung again in place rather than stacking a second.
func TestRingingAgainReusesTheDoorbellOfTheSameComment(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	released, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, released, 1)
	comment := released[0].ID

	first := ring(t, st, agentID, comment, -time.Hour)

	_, err = st.mail.Send(ctx, mailstore.SendParams{
		ToAgentID:       agentID,
		Body:            "weave: there is review on your diff you have not read",
		Kind:            mailstore.KindReviewActivity,
		ReviewMessageID: &comment,
		Hops:            1,
		Deadline:        time.Hour,
	})
	require.NoError(t, err)

	waiting, err := st.mail.CountPending(ctx, agentID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), waiting, "the expired doorbell is rung again rather than joined by a second")
	assert.Equal(t, DeliverySent, delivery(t, st, thread.ID, comment))

	held, err := st.mail.GetByID(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, first, held.ID, "and it is the same row")
}

// TestDeletingACommentLeavesTheDoorbellTheAgentMayHaveRead pins the set null behind the
// doorbell's link: taking a comment back must not delete mail the agent has already been
// handed, since it cannot unread it.
func TestDeletingACommentLeavesTheDoorbellTheAgentMayHaveRead(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	openThread(t, st, agentID, "this needs a nil check")

	released, err := st.ReleaseUnsentMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, released, 1)
	comment := released[0].ID

	doorbell := ring(t, st, agentID, comment, time.Hour)

	require.NoError(t, st.DeleteMessage(ctx, comment))

	held, err := st.mail.GetByID(ctx, doorbell)
	require.NoError(t, err)
	require.NotNil(t, held)
	assert.Nil(t, held.ReviewMessageID, "the doorbell stays, pointing at nothing")
}

// TestRewritingAReplyIsNewActivity pins that an edit reaches the person reviewing, which
// created_at could never show since rewriting a body does not move when it was written.
func TestRewritingAReplyIsNewActivity(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	answered, err := st.AddMessage(ctx, AddMessageParams{
		ThreadID:      thread.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &agentID,
		Body:          "added one",
	})
	require.NoError(t, err)

	catchUp(t, st, agentID)

	_, err = st.EditMessage(ctx, answered.ID, "rewritten")
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.True(t, threads[0].UnreadByHuman, "the agent rewriting its reply is unread by the person")
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

	_, err := st.MarkThreadRead(ctx, first.ID)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, threads, 2)

	byID := map[string]Thread{}
	for _, thread := range threads {
		byID[thread.ID] = thread
	}

	assert.False(t, byID[first.ID].UnreadByHuman, "the thread that was opened is caught up")
	assert.True(t, byID[second.ID].UnreadByHuman, "the one that was not stays unread")
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

			_, err = st.MarkThreadRead(ctx, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)

			_, err = st.EditMessage(ctx, tt.id, "rewritten")
			assert.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

// TestMarkingAFileViewedAgainReplacesTheShapeHeldForIt covers reading a file a second time
// after it has changed: the mark is one row per file, and the shape it holds is the one from
// the last read rather than the first.
func TestMarkingAFileViewedAgainReplacesTheShapeHeldForIt(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	_, err := st.MarkFileViewed(ctx, MarkFileViewedParams{
		AgentID:     agentID,
		FilePath:    "greet.go",
		Fingerprint: "before",
	})
	require.NoError(t, err)

	again, err := st.MarkFileViewed(ctx, MarkFileViewedParams{
		AgentID:     agentID,
		FilePath:    "greet.go",
		Fingerprint: "after",
	})
	require.NoError(t, err)
	assert.Equal(t, "after", again.Fingerprint)

	viewed, err := st.ListViewedFiles(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, viewed, 1)
	assert.Equal(t, "greet.go", viewed[0].FilePath)
	assert.Equal(t, "after", viewed[0].Fingerprint)

	require.NoError(t, st.UnmarkFileViewed(ctx, agentID, "greet.go"))

	viewed, err = st.ListViewedFiles(ctx, agentID)
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

type fixture struct {
	*Store
	agents   *agentstore.Store
	mail     *mailstore.Store
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
		mail:     mailstore.New(p.GetDB()),
		workflow: created.ID,
	}
}

// ring raises the doorbell for one comment and answers the id of the mail holding it, taking a
// negative deadline as one that has already run out so a test reaches expired without waiting.
func ring(t *testing.T, st *fixture, agentID, messageID string, deadline time.Duration) string {
	t.Helper()

	sent, err := st.mail.Send(t.Context(), mailstore.SendParams{
		ToAgentID:       agentID,
		Body:            "weave: there is review on your diff you have not read",
		Kind:            mailstore.KindReviewActivity,
		ReviewMessageID: &messageID,
		Hops:            1,
		Deadline:        deadline,
	})
	require.NoError(t, err)
	require.NotNil(t, sent)

	return sent.ID
}

// delivery is the state the list reports for one comment, which is the only place the panel
// reads it from.
func delivery(t *testing.T, st *fixture, threadID, messageID string) DeliveryState {
	t.Helper()

	messages, err := st.ListMessages(t.Context(), []string{threadID})
	require.NoError(t, err)

	for _, message := range messages {
		if message.ID == messageID {
			return message.Delivery
		}
	}

	t.Fatalf("no message %q in thread %q", messageID, threadID)

	return DeliveryNone
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

// catchUp reads the person up to date on every thread, so what a test asserts afterwards is
// only what the step it is testing changed.
func catchUp(t *testing.T, st *fixture, agentID string) {
	t.Helper()

	threads, err := st.ListThreads(t.Context(), agentID)
	require.NoError(t, err)

	for _, thread := range threads {
		_, err := st.MarkThreadRead(t.Context(), thread.ID)
		require.NoError(t, err)
	}
}

// TestSettlingAThreadIsCarriedByTheList pins that settling reaches the panel, since the panel
// draws from the list and nothing else: a thread settled in the table and not in the list is
// settled nowhere anybody can see.
func TestSettlingAThreadIsCarriedByTheList(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	settled, err := st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)
	require.NotNil(t, settled)
	assert.NotNil(t, settled.ResolvedAt)

	listed, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotNil(t, listed[0].ResolvedAt, "the list carries the settling")

	reopened, err := st.SetThreadResolved(ctx, thread.ID, false)
	require.NoError(t, err)
	require.NotNil(t, reopened)
	assert.Nil(t, reopened.ResolvedAt)

	listed, err = st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].ResolvedAt, "and it carries the reopening")
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
