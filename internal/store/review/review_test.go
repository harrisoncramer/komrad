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

// TestARoundIsNumberedAfterTheOnesBeforeIt pins that the ordinal is the store's to decide.
func TestARoundIsNumberedAfterTheOnesBeforeIt(t *testing.T) {
	st := open(t)
	agentID := seedAgent(t, st)

	first := openRound(t, st, agentID, "tree-one")
	second := openRound(t, st, agentID, "tree-two")

	assert.Equal(t, int32(1), first.Ordinal)
	assert.Equal(t, int32(2), second.Ordinal)

	latest, err := st.GetLatestRound(t.Context(), agentID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, second.ID, latest.ID)
}

// TestAnAuthorHoldsOneOpenReviewAtATime is the whole of one round at a time: a second open batch
// by the same author is refused by the database rather than by anything above it.
func TestAnAuthorHoldsOneOpenReviewAtATime(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")

	_, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: round.ID, AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	_, err = st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: round.ID, AuthorKind: AuthorHuman,
	})
	require.Error(t, err, "the person cannot draft two reviews of one agent")

	reviewer := seedAgentNamed(t, st, "reviewer")
	_, err = st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: round.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
	})
	require.NoError(t, err, "another author reviewing the same round is a review of its own")
}

// TestReleasingAReviewCarriesEveryCommentInIt covers the batch being the unit: one release
// stamps every comment written under it, and the agent's own answers belong to no batch and are
// never released at all.
func TestReleasingAReviewCarriesEveryCommentInIt(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	first := openThread(t, st, agentID, review, "this needs a nil check")
	second := openThread(t, st, agentID, review, "and this one leaks")

	_, err := st.CreateMessage(ctx, CreateMessageParams{
		ThreadID:      first.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &agentID,
		Body:          "done",
	})
	require.NoError(t, err)

	count, err := st.CountOpenComments(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "the agent's answer is in no batch")

	release(t, st, agentID, review.ID, VerdictChangesRequested)

	assert.Equal(t, DeliverySent, delivery(t, st, first.ID))
	assert.Equal(t, DeliverySent, delivery(t, st, second.ID))
	assert.Equal(t, DeliveryNone, deliveryOfMessage(t, st, first.ID, AuthorAgent))
}

// TestDeliveryFollowsTheOneMail is why the four delivery states could go: the mail row is the
// record of whether the agent has been told, so claiming it moves every comment in the batch at
// once and nothing else has to be stamped.
func TestDeliveryFollowsTheOneMail(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	thread := openThread(t, st, agentID, review, "this needs a nil check")
	assert.Equal(t, DeliveryUnsent, delivery(t, st, thread.ID))

	doorbell := release(t, st, agentID, review.ID, VerdictChangesRequested)
	assert.Equal(t, DeliverySent, delivery(t, st, thread.ID))

	claimed, err := st.mail.Claim(ctx, agentID, 10)
	require.NoError(t, err)
	require.NotEmpty(t, claimed)
	require.Equal(t, doorbell, claimed[0].ID)

	assert.Equal(t, DeliveryDelivered, delivery(t, st, thread.ID))
}

// TestAThreadCarriesTheRoundItWasWrittenAgainst pins the list handing back which round a thread
// belongs to, which is what says whether the lines it names are still true.
func TestAThreadCarriesTheRoundItWasWrittenAgainst(t *testing.T) {
	st := open(t)
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	opened := openThread(t, st, agentID, review, "this needs a nil check")

	threads, err := st.ListThreads(t.Context(), agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)

	assert.Equal(t, opened.ID, threads[0].ID)
	assert.Equal(t, round.ID, threads[0].RoundID)
	assert.Equal(t, review.ID, threads[0].ReviewID)
	assert.Equal(t, int32(1), threads[0].RoundOrdinal)
}

// TestRewritingAReplyIsNewActivity covers the agent rewriting what it said after the person has
// read it: the edit is news, so the thread reads unread again.
func TestRewritingAReplyIsNewActivity(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	thread := openThread(t, st, agentID, review, "this needs a nil check")

	answered, err := st.CreateMessage(ctx, CreateMessageParams{
		ThreadID:      thread.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &agentID,
		Body:          "done",
	})
	require.NoError(t, err)

	catchUp(t, st, agentID)

	_, err = st.UpdateMessage(ctx, answered.ID, "rewritten", nil)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.True(t, threads[0].UnreadByHuman)
}

// TestMarkThreadReadCatchesUpOneSide keeps reading one conversation from claiming the rest.
func TestMarkThreadReadCatchesUpOneSide(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	first := openThread(t, st, agentID, review, "first note")
	second := openThread(t, st, agentID, review, "second note")

	for _, thread := range []*Thread{first, second} {
		_, err := st.CreateMessage(ctx, CreateMessageParams{
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

// TestMissingRowsAnswerNotFound keeps the store's promise that an id naming nothing is a not
// found rather than an internal failure.
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

			_, err = st.UpdateMessage(ctx, tt.id, "rewritten", nil)
			require.ErrorIs(t, err, store.ErrNotFound)

			_, err = st.GetRound(ctx, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)

			_, err = st.GetReview(ctx, tt.id)
			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

// TestAViewedMarkBelongsToItsRound is why the fingerprint could go: a mark is held against the
// round it was made in, so the next round starts with nothing read and no hash is compared.
func TestAViewedMarkBelongsToItsRound(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)

	first := openRound(t, st, agentID, "tree-one")
	_, err := st.MarkFileViewed(ctx, MarkFileViewedParams{RoundID: first.ID, FilePath: "greet.go"})
	require.NoError(t, err)

	viewed, err := st.ListViewedFiles(ctx, first.ID)
	require.NoError(t, err)
	require.Len(t, viewed, 1)

	second := openRound(t, st, agentID, "tree-two")
	viewed, err = st.ListViewedFiles(ctx, second.ID)
	require.NoError(t, err)
	assert.Empty(t, viewed, "a later round has read nothing yet")

	require.NoError(t, st.UnmarkFileViewed(ctx, first.ID, "greet.go"))

	viewed, err = st.ListViewedFiles(ctx, first.ID)
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

// TestSettlingAThreadIsCarriedByTheList pins that settling reaches the panel, which draws from
// the list and nothing else.
func TestSettlingAThreadIsCarriedByTheList(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	thread := openThread(t, st, agentID, review, "this needs a nil check")

	_, err := st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.NotNil(t, threads[0].ResolvedAt)
}

// TestSayingSomethingElseReopensAThread covers a settled conversation that turns out not to be
// finished, whether the new words are written or an old message is rewritten.
func TestSayingSomethingElseReopensAThread(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	thread := openThread(t, st, agentID, review, "this needs a nil check")

	_, err := st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)

	said, err := st.CreateMessage(ctx, CreateMessageParams{
		ThreadID:   thread.ID,
		ReviewID:   &review.ID,
		Body:       "one more thing",
		AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	again, err := st.GetThread(ctx, thread.ID)
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Nil(t, again.ResolvedAt)

	_, err = st.SetThreadResolved(ctx, thread.ID, true)
	require.NoError(t, err)

	_, err = st.UpdateMessage(ctx, said.ID, "one more thing, rewritten", nil)
	require.NoError(t, err)

	edited, err := st.GetThread(ctx, thread.ID)
	require.NoError(t, err)
	require.NotNil(t, edited)
	assert.Nil(t, edited.ResolvedAt, "an edit reopens it too")
}

// TestASuggestionIsCarriedWithTheComment pins that replacement lines survive the round trip.
func TestASuggestionIsCarriedWithTheComment(t *testing.T) {
	st := open(t)
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		AgentID:    agentID,
		ReviewID:   review.ID,
		RoundID:    round.ID,
		FilePath:   "internal/thing.go",
		NewStart:   41,
		NewEnd:     41,
		Body:       "guard this",
		Suggestion: []string{"if thing == nil {", "\treturn nil", "}"},
		AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	messages, err := st.ListMessages(t.Context(), []string{thread.ID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, []string{"if thing == nil {", "\treturn nil", "}"}, messages[0].Suggestion)
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

// release sends the one piece of mail a batch is heard through and lets the batch go under it,
// which is the order ReviewService releases in.
func release(t *testing.T, st *fixture, agentID, reviewID string, verdict Verdict) string {
	t.Helper()
	ctx := t.Context()

	sent, err := st.mail.Send(ctx, mailstore.SendParams{
		ToAgentID: agentID,
		Body:      "weave: review of round 1, changes requested, 1 comment(s)",
		Kind:      mailstore.KindReviewActivity,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, sent)

	_, err = st.ReleaseReview(ctx, ReleaseReviewParams{
		ID:             reviewID,
		Verdict:        verdict,
		Path:           "/tmp/round-1.md",
		DoorbellMailID: &sent.ID,
	})
	require.NoError(t, err)

	return sent.ID
}

// delivery is the state the list reports for a thread's first comment, which is the only place
// the panel reads it from.
func delivery(t *testing.T, st *fixture, threadID string) DeliveryState {
	t.Helper()

	return deliveryOfMessage(t, st, threadID, AuthorHuman)
}

func deliveryOfMessage(t *testing.T, st *fixture, threadID string, kind AuthorKind) DeliveryState {
	t.Helper()

	messages, err := st.ListMessages(t.Context(), []string{threadID})
	require.NoError(t, err)

	for _, message := range messages {
		if message.AuthorKind == kind {
			return message.Delivery
		}
	}

	t.Fatalf("no %s message in thread %q", kind, threadID)

	return DeliveryNone
}

func seedAgent(t *testing.T, st *fixture) string {
	t.Helper()

	return seedAgentNamed(t, st, "worker")
}

func seedAgentNamed(t *testing.T, st *fixture, name string) string {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name: name, State: agentstore.StateIdle, WorkflowID: st.workflow,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func openRound(t *testing.T, st *fixture, agentID, tree string) *Round {
	t.Helper()

	round, err := st.CreateRound(t.Context(), CreateRoundParams{
		AgentID:     agentID,
		BaseSHA:     "base-" + tree,
		SnapshotSHA: "snapshot-" + tree,
	})
	require.NoError(t, err)
	require.NotNil(t, round)

	return round
}

func openReview(t *testing.T, st *fixture, agentID, roundID string) *Review {
	t.Helper()

	review, err := st.OpenReview(t.Context(), OpenReviewParams{
		AgentID:    agentID,
		RoundID:    roundID,
		AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)
	require.NotNil(t, review)

	return review
}

func openThread(t *testing.T, st *fixture, agentID string, review *Review, body string) *Thread {
	t.Helper()

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		AgentID:     agentID,
		ReviewID:    review.ID,
		RoundID:     review.RoundID,
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

// TestTalliesCountTheAgentHoldingTheDiff is what the panel draws a loop from: the rounds are
// counted against the agent whose work went round, the reviewer that read it is not counted at
// all, and a batch nobody has released is counted against neither.
func TestTalliesCountTheAgentHoldingTheDiff(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	reviewer := seedAgentNamed(t, st, "reviewer")

	for _, tree := range []string{"tree-one", "tree-two"} {
		round := openRound(t, st, agentID, tree)
		review, err := st.OpenReview(ctx, OpenReviewParams{
			AgentID: agentID, RoundID: round.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
		})
		require.NoError(t, err)
		release(t, st, agentID, review.ID, VerdictChangesRequested)
	}

	third := openRound(t, st, agentID, "tree-three")
	openReview(t, st, agentID, third.ID)

	tallies, err := st.ListTallies(ctx)
	require.NoError(t, err)

	require.Len(t, tallies, 1, "the reviewer holds no diff of its own to have gone round")
	changesRequested := VerdictChangesRequested
	assert.Equal(t, Tally{AgentID: agentID, Rounds: 2, Verdict: &changesRequested}, tallies[0])
}

// TestTalliesCountOneRoundOnce is why the count is of rounds rather than of reviews: two authors
// reading the same snapshot is one round the work went through, not two.
func TestTalliesCountOneRoundOnce(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	reviewer := seedAgentNamed(t, st, "reviewer")
	round := openRound(t, st, agentID, "tree-one")

	human := openReview(t, st, agentID, round.ID)
	release(t, st, agentID, human.ID, VerdictCommented)

	agent, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: round.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
	})
	require.NoError(t, err)
	release(t, st, agentID, agent.ID, VerdictChangesRequested)

	tallies, err := st.ListTallies(ctx)
	require.NoError(t, err)

	require.Len(t, tallies, 1)
	changesRequested := VerdictChangesRequested
	assert.Equal(t, Tally{AgentID: agentID, Rounds: 1, Verdict: &changesRequested}, tallies[0])
}

// TestHarvestedThreadsAreOnlyTheOnesStillBeingWritten pins what the reconcile pass reads, which
// leaves out the harvested threads of a batch that has gone.
func TestHarvestedThreadsAreOnlyTheOnesStillBeingWritten(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	harvested, err := st.CreateThread(ctx, CreateThreadParams{
		AgentID: agentID, ReviewID: review.ID, RoundID: round.ID, Harvested: true,
		FilePath: "internal/thing.go", NewStart: 12, NewEnd: 14,
		QuotedLines: []string{"+\tdoThing()"}, Body: "an edit", AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)
	typed := openThread(t, st, agentID, review, "something a person typed")

	other := seedAgentNamed(t, st, "other")
	otherRound := openRound(t, st, other, "tree-two")
	otherReview := openReview(t, st, other, otherRound.ID)
	_, err = st.CreateThread(ctx, CreateThreadParams{
		AgentID: other, ReviewID: otherReview.ID, RoundID: otherRound.ID, Harvested: true,
		FilePath: "internal/thing.go", NewStart: 12, NewEnd: 14,
		QuotedLines: []string{"+\tdoThing()"}, Body: "another agent's edit", AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	open, err := st.ListHarvested(ctx, agentID, round.ID)
	require.NoError(t, err)
	require.Len(t, open, 1, "the typed thread and the other agent's are not this pass's to touch")
	assert.Equal(t, harvested.ID, open[0].ID)
	assert.NotEqual(t, typed.ID, open[0].ID)

	release(t, st, agentID, review.ID, VerdictChangesRequested)

	released, err := st.ListHarvested(ctx, agentID, round.ID)
	require.NoError(t, err)
	assert.Empty(t, released, "a released batch is out of the pass's hands")
}

// TestMovingAThreadKeepsWhatWasSaidInIt pins the repin a widened edit asks for, which a rewrite
// would lose the agent's reply to.
func TestMovingAThreadKeepsWhatWasSaidInIt(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)
	thread := openThread(t, st, agentID, review, "the first word")

	_, err := st.CreateMessage(ctx, CreateMessageParams{
		ThreadID: thread.ID, ReviewID: &review.ID, Body: "the second", AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	moved, err := st.MoveThread(ctx, MoveThreadParams{
		ID: thread.ID, NewStart: 40, NewEnd: 44, QuotedLines: []string{"+\tdoThing()", "+\tandAnother()"},
	})
	require.NoError(t, err)

	assert.Equal(t, int32(40), moved.NewStart)
	assert.Equal(t, int32(44), moved.NewEnd)
	assert.Equal(t, []string{"+\tdoThing()", "+\tandAnother()"}, moved.QuotedLines)
	assert.Equal(t, round.ID, moved.RoundID, "a move is within the round the thread was written against")

	messages, err := st.ListMessages(ctx, []string{thread.ID})
	require.NoError(t, err)
	assert.Len(t, messages, 2, "both messages are still under it")
}

// TestMovingAThreadNobodyHoldsIsNotAQuietSuccess pins the refusal a thread deleted under the
// reconcile pass gets.
func TestMovingAThreadNobodyHoldsIsNotAQuietSuccess(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{name: "a string that is not an id at all", id: "not-an-id"},
		{name: "a well formed id naming nothing", id: uuid.NewString()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := open(t)

			_, err := st.MoveThread(t.Context(), MoveThreadParams{
				ID:       tt.id,
				NewStart: 1,
				NewEnd:   1,
			})
			require.ErrorIs(t, err, store.ErrNotFound)

			require.ErrorIs(t, st.DeleteThread(t.Context(), tt.id), store.ErrNotFound)
		})
	}
}

// TestDeletingAThreadTakesWhatWasSaidInIt pins that an undone edit leaves nothing behind.
func TestDeletingAThreadTakesWhatWasSaidInIt(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	going := openThread(t, st, agentID, review, "an edit that was undone")
	staying := openThread(t, st, agentID, review, "an edit that stands")

	require.NoError(t, st.DeleteThread(ctx, going.ID))

	_, err := st.GetThread(ctx, going.ID)
	require.ErrorIs(t, err, store.ErrNotFound)

	messages, err := st.ListMessages(ctx, []string{going.ID, staying.ID})
	require.NoError(t, err)
	require.Len(t, messages, 1, "the deleted thread's message goes with it")
	assert.Equal(t, staying.ID, messages[0].ThreadID)
}

// TestTheLastReleasedReviewIsTheNewestOne pins which batch answers for where an author last
// looked.
func TestTheLastReleasedReviewIsTheNewestOne(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	author := AuthorParams{AgentID: agentID, AuthorKind: AuthorHuman}

	_, err := st.GetLastReleasedReview(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound, "an author who has released none has looked nowhere")

	first := openRound(t, st, agentID, "tree-one")
	firstReview := openReview(t, st, agentID, first.ID)
	release(t, st, agentID, firstReview.ID, VerdictChangesRequested)

	second := openRound(t, st, agentID, "tree-two")
	secondReview := openReview(t, st, agentID, second.ID)

	held, err := st.GetLastReleasedReview(ctx, author)
	require.NoError(t, err)
	assert.Equal(t, firstReview.ID, held.ID, "a batch still being written is not one they have looked at")

	release(t, st, agentID, secondReview.ID, VerdictApproved)

	latest, err := st.GetLastReleasedReview(ctx, author)
	require.NoError(t, err)
	assert.Equal(t, secondReview.ID, latest.ID)
}

// TestTheOpenReviewIsTheOneStillBeingWritten pins that releasing a batch closes it, which is what
// lets the same author open another against the next round.
func TestTheOpenReviewIsTheOneStillBeingWritten(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	author := AuthorParams{AgentID: agentID, AuthorKind: AuthorHuman}

	_, err := st.OpenReviewOf(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound)

	round := openRound(t, st, agentID, "tree-one")
	review := openReview(t, st, agentID, round.ID)

	held, err := st.OpenReviewOf(ctx, author)
	require.NoError(t, err)
	assert.Equal(t, review.ID, held.ID)

	release(t, st, agentID, review.ID, VerdictCommented)

	_, err = st.OpenReviewOf(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound, "a batch that has gone is not still being written")
}

// TestDeletingAnAuthorsOpenReviewsLeavesWhatTheyReleased pins the cleanup a departing reviewer
// gets, which leaves a review the agent has already read.
func TestDeletingAnAuthorsOpenReviewsLeavesWhatTheyReleased(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	reviewer := seedAgentNamed(t, st, "reviewer")
	author := AuthorParams{
		AgentID:       agentID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &reviewer,
	}

	first := openRound(t, st, agentID, "tree-one")
	released, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: first.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
	})
	require.NoError(t, err)
	release(t, st, agentID, released.ID, VerdictChangesRequested)

	second := openRound(t, st, agentID, "tree-two")
	draft, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: second.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
	})
	require.NoError(t, err)

	require.NoError(t, st.DeleteOpenReviewsBy(ctx, []string{reviewer}))

	_, err = st.GetReview(ctx, draft.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "the draft nobody will finish goes")

	kept, err := st.GetReview(ctx, released.ID)
	require.NoError(t, err)
	assert.Equal(t, released.ID, kept.ID, "the review the agent was already given stands")

	_, err = st.GetLastReleasedReview(ctx, author)
	require.NoError(t, err)
}

// seedReviewerChild is a reviewer attached to the agent whose work it reads, which is the shape
// a tally reads to decide that work is being read right now.
func seedReviewerChild(t *testing.T, st *fixture, name, parentID string) string {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, st.agents.Create(ctx, agentstore.Agent{
		Name:          name,
		State:         agentstore.StateIdle,
		WorkflowID:    st.workflow,
		Role:          agentstore.RoleReviewer,
		ParentAgentID: &parentID,
	}))
	got, err := st.agents.GetInWorkflow(ctx, st.workflow, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

// TestTalliesReportWorkBeingRead is the state a round count cannot say: a reviewer that has been
// spawned and has released nothing yet leaves no rounds behind it.
func TestTalliesReportWorkBeingRead(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	seedReviewerChild(t, st, "worker-reviewer", agentID)

	tallies, err := st.ListTallies(ctx)
	require.NoError(t, err)

	require.Len(t, tallies, 1)
	assert.Equal(t, Tally{AgentID: agentID, InReview: true}, tallies[0])
}

// TestTalliesStopReportingReadingOnceApproved pins the end of the loop: an approval is the last
// thing a reviewer says, so the work is no longer owed an answer.
func TestTalliesStopReportingReadingOnceApproved(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	reviewer := seedReviewerChild(t, st, "worker-reviewer", agentID)
	round := openRound(t, st, agentID, "tree-one")

	review, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: round.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
	})
	require.NoError(t, err)
	release(t, st, agentID, review.ID, VerdictChangesRequested)

	tallies, err := st.ListTallies(ctx)
	require.NoError(t, err)
	require.Len(t, tallies, 1)
	assert.True(t, tallies[0].InReview, "changes requested is the middle of the loop")

	second := openRound(t, st, agentID, "tree-two")
	signed, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID: agentID, RoundID: second.ID, AuthorKind: AuthorAgent, AuthorAgentID: &reviewer,
	})
	require.NoError(t, err)
	release(t, st, agentID, signed.ID, VerdictApproved)

	tallies, err = st.ListTallies(ctx)
	require.NoError(t, err)
	require.Len(t, tallies, 1)

	approved := VerdictApproved
	assert.Equal(t, Tally{AgentID: agentID, Rounds: 2, Verdict: &approved}, tallies[0])
}

// TestTalliesLeaveOutADeadReviewer pins that a reviewer whose setup failed is not read as still
// reading, which would leave the work looking busy forever.
func TestTalliesLeaveOutADeadReviewer(t *testing.T) {
	st := open(t)
	ctx := t.Context()
	agentID := seedAgent(t, st)
	reviewer := seedReviewerChild(t, st, "worker-reviewer", agentID)

	require.NoError(t, st.agents.SetStateByID(ctx, reviewer, agentstore.StateFailedToCreate))

	tallies, err := st.ListTallies(ctx)
	require.NoError(t, err)
	assert.Empty(t, tallies, "nothing is reading the work and no review has been released of it")
}
