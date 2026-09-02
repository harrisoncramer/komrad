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

	released := release(t, st, agentID)
	require.Len(t, released, 1, "only the person's comment is released")
	assert.NotNil(t, released[0].SentAt)
	assert.Equal(t, DeliverySent, released[0].Delivery, "let go of, with its doorbell waiting to be read")

	assert.Empty(t, release(t, st, agentID), "a comment already let go of is not released a second time")

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

	third := release(t, st, agentID)
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

	released := release(t, st, agentID)
	require.Len(t, released, 1)

	edited, err := st.EditMessage(ctx, released[0].ID, "this needs a nil check, and a test")
	require.NoError(t, err)
	require.NotNil(t, edited.SentAt)
	assert.Equal(t, DeliverySent, edited.Delivery)

	assert.Empty(t, release(t, st, agentID), "rewriting is not something new to release")
}

// TestDeliveryFollowsTheDoorbell walks a released comment through the states the panel draws
// from: waiting to be claimed, claimed, and left with no doorbell it can still be read through.
func TestDeliveryFollowsTheDoorbell(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	released := release(t, st, agentID)
	require.Len(t, released, 1)
	comment := released[0].ID

	assert.Equal(t, DeliverySent, delivery(t, st, thread.ID, comment))

	expired, err := st.ListExpiredMessages(ctx, agentID)
	require.NoError(t, err)
	assert.Empty(t, expired, "a doorbell still waiting to be read is not rung again")

	_, err = st.mail.Claim(ctx, agentID, 10)
	require.NoError(t, err)
	assert.Equal(t, DeliveryDelivered, delivery(t, st, thread.ID, comment))

	require.NoError(t, st.mail.Delete(ctx, []string{*doorbellOf(t, st, comment)}))
	assert.Equal(t, DeliveryExpired, delivery(t, st, thread.ID, comment),
		"a comment whose doorbell is gone is what ringing again reaches")

	ring(t, st, agentID, -time.Hour, comment)
	assert.Equal(t, DeliveryExpired, delivery(t, st, thread.ID, comment))

	expired, err = st.ListExpiredMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, expired, 1, "a doorbell that ran out of time unread is rung again")
	assert.Equal(t, comment, expired[0].ID)

	ring(t, st, agentID, time.Hour, comment)
	assert.Equal(t, DeliverySent, delivery(t, st, thread.ID, comment))
}

// TestOneDoorbellCarriesAWholeRelease pins that letting go of a review costs one piece of mail
// however many comments it holds, and that the comments sharing that doorbell each read its
// state, since it is the truth about all of them at once.
func TestOneDoorbellCarriesAWholeRelease(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	first := openThread(t, st, agentID, "this needs a nil check")
	second := openThread(t, st, agentID, "and this leaks a file handle")

	released := release(t, st, agentID)
	require.Len(t, released, 2)

	waiting, err := st.mail.CountPending(ctx, agentID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), waiting, "one doorbell says what two identical ones would")

	assert.Equal(t, doorbellOf(t, st, released[0].ID), doorbellOf(t, st, released[1].ID),
		"both comments were let go of through the same doorbell")

	assert.Equal(t, DeliverySent, delivery(t, st, first.ID, released[0].ID))
	assert.Equal(t, DeliverySent, delivery(t, st, second.ID, released[1].ID))

	_, err = st.mail.Claim(ctx, agentID, 10)
	require.NoError(t, err)

	assert.Equal(t, DeliveryDelivered, delivery(t, st, first.ID, released[0].ID))
	assert.Equal(t, DeliveryDelivered, delivery(t, st, second.ID, released[1].ID),
		"reading the one doorbell is being told about every comment behind it")
}

// TestDeletingADoorbellLeavesTheCommentNeedingAnother pins the set null behind the comment's
// link: an agent throwing away mail must not take the person's comment with it, and the
// comment it pointed at is left reading as one to ring again.
func TestDeletingADoorbellLeavesTheCommentNeedingAnother(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	thread := openThread(t, st, agentID, "this needs a nil check")

	released := release(t, st, agentID)
	require.Len(t, released, 1)
	comment := released[0].ID

	require.NoError(t, st.mail.Delete(ctx, []string{*doorbellOf(t, st, comment)}))

	held, err := st.GetMessage(ctx, comment)
	require.NoError(t, err)
	require.NotNil(t, held)
	assert.NotNil(t, held.SentAt, "the record that it was let go of outlives the mail that said so")

	assert.Equal(t, DeliveryExpired, delivery(t, st, thread.ID, comment))

	expired, err := st.ListExpiredMessages(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, expired, 1)
	assert.Equal(t, comment, expired[0].ID)
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
	pool     store.DBTX
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
		pool:     p.GetDB(),
		workflow: created.ID,
	}
}

// ring raises one doorbell and points the given comments at it, the way ringing again does,
// taking a negative deadline as one that has already run out so a test reaches expired without
// waiting.
func ring(t *testing.T, st *fixture, agentID string, deadline time.Duration, messageIDs ...string) {
	t.Helper()
	ctx := t.Context()

	sent, err := st.mail.Send(ctx, mailstore.SendParams{
		ToAgentID: agentID,
		Body:      "weave: there is review on your diff you have not read",
		Kind:      mailstore.KindReviewActivity,
		Hops:      1,
		Deadline:  deadline,
	})
	require.NoError(t, err)
	require.NotNil(t, sent)

	pointed, err := st.PointMessagesAtDoorbell(ctx, messageIDs, sent.ID)
	require.NoError(t, err)
	require.Len(t, pointed, len(messageIDs))
}

// release raises a doorbell and lets go of everything unsent under it, which is the order
// ReviewService releases in so that nothing is stamped for a review that never rang.
func release(t *testing.T, st *fixture, agentID string) []Message {
	t.Helper()
	ctx := t.Context()

	sent, err := st.mail.Send(ctx, mailstore.SendParams{
		ToAgentID: agentID,
		Body:      "weave: there is review on your diff you have not read",
		Kind:      mailstore.KindReviewActivity,
		Hops:      1,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)

	released, err := st.ReleaseUnsentMessages(ctx, agentID, sent.ID)
	require.NoError(t, err)

	return released
}

// doorbellOf is the mail a comment was released under, read straight out of the table since
// nothing hands it back.
func doorbellOf(t *testing.T, st *fixture, messageID string) *string {
	t.Helper()

	var held *string
	require.NoError(t, st.pool.QueryRow(t.Context(),
		"SELECT doorbell_mail_id FROM review_messages WHERE id = $1", messageID).Scan(&held))
	require.NotNil(t, held, "the comment was released under a doorbell")

	return held
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
