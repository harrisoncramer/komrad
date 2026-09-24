package review

import (
	"testing"
	"time"

	"komrade/internal/dbtest"
	"komrade/internal/store"
	agentstore "komrade/internal/store/agent"
	mailstore "komrade/internal/store/mail"
	workflowstore "komrade/internal/store/workflow"
	"komrade/lib/xptr"
	"komrade/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	*ReviewStore
	agents   *agentstore.AgentStore
	mail     *mailstore.MailStore
	pool     store.DBTX
	workflow string
}

// tallyRelease is one review let go of on the tree named.
type tallyRelease struct {
	tree       string
	byReviewer bool
	verdict    Verdict
}

// liveReviewerSeed is the agent and the reviewer attached to it in a test of live reviewers.
type liveReviewerSeed struct {
	agentID    string
	reviewerID string
}

// TestARoundIsNumberedAfterTheOnesBeforeIt pins that the ordinal is the store's to decide.
func TestARoundIsNumberedAfterTheOnesBeforeIt(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")

	first := st.openRound(t, agentID, "tree-one")
	second := st.openRound(t, agentID, "tree-two")

	assert.Equal(t, int32(1), first.Ordinal)
	assert.Equal(t, int32(2), second.Ordinal)

	latest, err := st.GetLatestRound(t.Context(), agentID)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, second.ID, latest.ID)
}

// TestAnAuthorHoldsOneOpenReviewAtATime pins that the database refuses a second open review by the same author.
func TestAnAuthorHoldsOneOpenReviewAtATime(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	round := st.openRound(t, agentID, "tree-one")

	st.openReview(t, round)

	_, err := st.OpenReview(ctx, OpenReviewParams{
		AgentID:    agentID,
		RoundID:    round.ID,
		AuthorKind: AuthorHuman,
	})
	require.Error(t, err, "the person cannot draft two reviews of one agent")

	reviewer := st.seedAgent(t, "reviewer")
	_, err = st.OpenReview(ctx, OpenReviewParams{
		AgentID:       agentID,
		RoundID:       round.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &reviewer,
	})
	require.NoError(t, err, "another author reviewing the same round is a review of its own")
}

// TestReleasingAReviewCarriesEveryCommentInIt pins that one release stamps every comment under it and none of the agent's answers.
func TestReleasingAReviewCarriesEveryCommentInIt(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	first := st.openThread(t, review, "this needs a nil check")
	second := st.openThread(t, review, "and this one leaks")
	st.writeAnswer(t, first)

	count, err := st.CountOpenComments(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "the agent's answer is in no batch")

	st.releaseReview(t, review, VerdictChangesRequested)

	assert.Equal(t, DeliverySent, st.readDelivery(t, first.ID, AuthorHuman))
	assert.Equal(t, DeliverySent, st.readDelivery(t, second.ID, AuthorHuman))
	assert.Equal(t, DeliveryNone, st.readDelivery(t, first.ID, AuthorAgent))
}

// TestDeliveryFollowsTheOneMail pins that claiming the doorbell moves every comment in the batch at once.
func TestDeliveryFollowsTheOneMail(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	thread := st.openThread(t, review, "this needs a nil check")
	assert.Equal(t, DeliveryUnsent, st.readDelivery(t, thread.ID, AuthorHuman))

	doorbell := st.releaseReview(t, review, VerdictChangesRequested)
	assert.Equal(t, DeliverySent, st.readDelivery(t, thread.ID, AuthorHuman))

	claimed, err := st.mail.Claim(t.Context(), agentID, 10)
	require.NoError(t, err)
	require.NotEmpty(t, claimed)
	require.Equal(t, doorbell, claimed[0].ID)

	assert.Equal(t, DeliveryDelivered, st.readDelivery(t, thread.ID, AuthorHuman))
}

// TestAThreadCarriesTheRoundItWasWrittenAgainst pins the list handing back which round a thread belongs to.
func TestAThreadCarriesTheRoundItWasWrittenAgainst(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	round := st.openRound(t, agentID, "tree-one")
	review := st.openReview(t, round)

	opened := st.openThread(t, review, "this needs a nil check")

	threads, err := st.ListThreads(t.Context(), agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)

	assert.Equal(t, opened.ID, threads[0].ID)
	assert.Equal(t, round.ID, threads[0].RoundID)
	assert.Equal(t, review.ID, threads[0].ReviewID)
	assert.Equal(t, int32(1), threads[0].RoundOrdinal)
}

// TestRewritingAReplyIsNewActivity pins that an answer rewritten after the person read it reads unread again.
func TestRewritingAReplyIsNewActivity(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	answered := st.writeAnswer(t, st.openThread(t, review, "this needs a nil check"))
	st.markAllRead(t, agentID)

	_, err := st.UpdateMessage(ctx, answered.ID, "rewritten", nil)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.True(t, threads[0].UnreadByHuman)
}

// TestMarkThreadReadCatchesUpOneSide keeps reading one conversation from claiming the rest.
func TestMarkThreadReadCatchesUpOneSide(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	first := st.openThread(t, review, "first note")
	second := st.openThread(t, review, "second note")
	st.writeAnswer(t, first)
	st.writeAnswer(t, second)

	_, err := st.MarkThreadRead(ctx, first.ID)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, agentID)
	require.NoError(t, err)
	require.Len(t, threads, 2)

	byID := xslice.KeyBy(threads, func(thread Thread) string {
		return thread.ID
	})
	assert.False(t, byID[first.ID].UnreadByHuman, "the thread that was opened is caught up")
	assert.True(t, byID[second.ID].UnreadByHuman, "the one that was not stays unread")
}

// TestMissingRowsAnswerNotFound keeps an id naming nothing a not found rather than an internal failure.
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
			st := openFixture(t)
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

// TestViewedMarkUsesFingerprint pins that marking a file again moves its fingerprint and unmarking drops it.
func TestViewedMarkUsesFingerprint(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")

	for _, fingerprint := range []string{"abc123", "def456"} {
		_, err := st.MarkFileViewed(ctx, MarkFileViewedParams{
			AgentID:     agentID,
			FilePath:    "greet.go",
			Fingerprint: fingerprint,
		})
		require.NoError(t, err)

		viewed, err := st.ListViewedFiles(ctx, agentID)
		require.NoError(t, err)
		require.Len(t, viewed, 1)
		assert.Equal(t, fingerprint, viewed[0].Fingerprint)
	}

	require.NoError(t, st.UnmarkFileViewed(ctx, agentID, "greet.go"))

	viewed, err := st.ListViewedFiles(ctx, agentID)
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

// TestSettlingAThreadIsCarriedByTheList pins that settling reaches the list the panel draws from.
func TestSettlingAThreadIsCarriedByTheList(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	thread := st.openThread(t, review, "this needs a nil check")
	st.settle(t, thread, nil)

	threads, err := st.ListThreads(t.Context(), agentID)
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.NotNil(t, threads[0].ResolvedAt)
}

// TestWritingInASettledThread pins that only the settler's own words take a settling back.
func TestWritingInASettledThread(t *testing.T) {
	tests := []struct {
		name        string
		settler     string
		writer      string
		rewrite     bool
		wantSettled bool
	}{
		{
			name:        "the agent under review answering what the person settled",
			settler:     "person",
			writer:      "worker",
			wantSettled: true,
		},
		{
			name:        "the agent under review rewriting its answer",
			settler:     "person",
			writer:      "worker",
			rewrite:     true,
			wantSettled: true,
		},
		{
			name:    "the person writing in what they settled",
			settler: "person",
			writer:  "person",
		},
		{
			name:    "the reviewer writing in what it settled",
			settler: "reviewer",
			writer:  "reviewer",
		},
		{
			name:    "the reviewer rewriting what it said in what it settled",
			settler: "reviewer",
			writer:  "reviewer",
			rewrite: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			ctx := t.Context()
			workerID := st.seedAgent(t, "worker")
			reviewerID := st.seedAgent(t, "reviewer")
			review := st.openReview(t, st.openRound(t, workerID, "tree-one"))
			thread := st.openThread(t, review, "this needs a nil check")

			agentIDs := map[string]*string{
				"person":   nil,
				"worker":   &workerID,
				"reviewer": &reviewerID,
			}
			writer := CreateMessageParams{
				ThreadID:      thread.ID,
				ReviewID:      &review.ID,
				Body:          "one more thing",
				AuthorKind:    AuthorHuman,
				AuthorAgentID: agentIDs[tt.writer],
			}
			if writer.AuthorAgentID != nil {
				writer.AuthorKind = AuthorAgent
			}

			said, err := st.CreateMessage(ctx, writer)
			require.NoError(t, err)

			st.settle(t, thread, agentIDs[tt.settler])
			if tt.rewrite {
				_, err = st.UpdateMessage(ctx, said.ID, "one more thing, rewritten", nil)
			} else {
				_, err = st.CreateMessage(ctx, writer)
			}
			require.NoError(t, err)

			got, err := st.GetThread(ctx, thread.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantSettled, got.ResolvedAt != nil)
			if !tt.wantSettled {
				assert.Empty(t, got.ResolvedByKind, "an unsettled thread names nobody as having settled it")
				assert.Nil(t, got.ResolvedByAgentID)
			}
		})
	}
}

// TestASuggestionIsCarriedWithTheComment pins that replacement lines survive the round trip.
func TestASuggestionIsCarriedWithTheComment(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	round := st.openRound(t, agentID, "tree-one")
	review := st.openReview(t, round)

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

// TestListTallies pins which rounds count against which agent and which conclusion stands with them.
func TestListTallies(t *testing.T) {
	tests := []struct {
		name      string
		releases  []tallyRelease
		openOn    string
		wantTally *Tally
	}{
		{
			name: "work nobody has released anything about",
		},
		{
			name: "notes with no conclusion",
			releases: []tallyRelease{
				{
					tree:    "tree-one",
					verdict: VerdictCommented,
				},
			},
			wantTally: &Tally{
				Rounds: 1,
			},
		},
		{
			name: "two rounds the reviewer sent back, and a third still being written",
			releases: []tallyRelease{
				{
					tree:       "tree-one",
					byReviewer: true,
					verdict:    VerdictChangesRequested,
				},
				{
					tree:       "tree-two",
					byReviewer: true,
					verdict:    VerdictChangesRequested,
				},
			},
			openOn: "tree-three",
			wantTally: &Tally{
				Rounds:      2,
				Verdict:     xptr.Ptr(VerdictChangesRequested),
				SnapshotSHA: "snapshot-tree-two",
			},
		},
		{
			name: "two authors on the one round",
			releases: []tallyRelease{
				{
					tree:    "tree-one",
					verdict: VerdictCommented,
				},
				{
					tree:       "tree-one",
					byReviewer: true,
					verdict:    VerdictChangesRequested,
				},
			},
			wantTally: &Tally{
				Rounds:      1,
				Verdict:     xptr.Ptr(VerdictChangesRequested),
				SnapshotSHA: "snapshot-tree-one",
			},
		},
		{
			name: "an approval after changes requested",
			releases: []tallyRelease{
				{
					tree:       "tree-one",
					byReviewer: true,
					verdict:    VerdictChangesRequested,
				},
				{
					tree:       "tree-two",
					byReviewer: true,
					verdict:    VerdictApproved,
				},
			},
			wantTally: &Tally{
				Rounds:      2,
				Verdict:     xptr.Ptr(VerdictApproved),
				SnapshotSHA: "snapshot-tree-two",
			},
		},
		{
			name: "an approval with a later round only commented on",
			releases: []tallyRelease{
				{
					tree:       "tree-one",
					byReviewer: true,
					verdict:    VerdictApproved,
				},
				{
					tree:    "tree-two",
					verdict: VerdictCommented,
				},
			},
			wantTally: &Tally{
				Rounds:      2,
				Verdict:     xptr.Ptr(VerdictApproved),
				SnapshotSHA: "snapshot-tree-one",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			agentID := st.seedAgent(t, "worker")
			reviewerID := st.seedReviewerChild(t, agentID)

			rounds := map[string]*Round{}
			for _, released := range append(tt.releases, tallyRelease{tree: tt.openOn}) {
				if released.tree == "" {
					continue
				}
				if rounds[released.tree] == nil {
					rounds[released.tree] = st.openRound(t, agentID, released.tree)
				}

				var review *Review
				if released.byReviewer {
					review = st.openReviewBy(t, rounds[released.tree], reviewerID)
				} else {
					review = st.openReview(t, rounds[released.tree])
				}
				if released.verdict != "" {
					st.releaseReview(t, review, released.verdict)
				}
			}

			tallies, err := st.ListTallies(t.Context())
			require.NoError(t, err)

			if tt.wantTally == nil {
				assert.Empty(t, tallies)

				return
			}

			tt.wantTally.AgentID = agentID
			assert.Equal(t, []Tally{*tt.wantTally}, tallies, "the reviewer holds no diff of its own to have gone round")
		})
	}
}

// TestNewestRoundsAreEachAgentsLastRound pins the round listed for each agent, released or not.
func TestNewestRoundsAreEachAgentsLastRound(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	other := st.seedAgent(t, "other")
	st.seedAgent(t, "unreviewed")

	st.openRound(t, agentID, "tree-one")
	st.openRound(t, agentID, "tree-two")
	st.openRound(t, other, "tree-three")

	newest, err := st.ListNewestRounds(t.Context())
	require.NoError(t, err)

	assert.ElementsMatch(t, []NewestRound{
		{
			AgentID:     agentID,
			SnapshotSHA: "snapshot-tree-two",
		},
		{
			AgentID:     other,
			SnapshotSHA: "snapshot-tree-three",
		},
	}, newest)
}

// TestTheLastReleasedReviewIsTheNewestOne pins which batch answers for where an author last looked.
func TestTheLastReleasedReviewIsTheNewestOne(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	author := AuthorParams{
		AgentID:    agentID,
		AuthorKind: AuthorHuman,
	}

	_, err := st.GetLastReleasedReview(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound, "an author who has released none has looked nowhere")

	first := st.openReview(t, st.openRound(t, agentID, "tree-one"))
	st.releaseReview(t, first, VerdictChangesRequested)
	second := st.openReview(t, st.openRound(t, agentID, "tree-two"))

	held, err := st.GetLastReleasedReview(ctx, author)
	require.NoError(t, err)
	assert.Equal(t, first.ID, held.ID, "a batch still being written is not one they have looked at")

	st.releaseReview(t, second, VerdictApproved)

	latest, err := st.GetLastReleasedReview(ctx, author)
	require.NoError(t, err)
	assert.Equal(t, second.ID, latest.ID)
}

// TestTheOpenReviewIsTheOneStillBeingWritten pins that releasing a batch closes it.
func TestTheOpenReviewIsTheOneStillBeingWritten(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	author := AuthorParams{
		AgentID:    agentID,
		AuthorKind: AuthorHuman,
	}

	_, err := st.OpenReviewOf(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound)

	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	held, err := st.OpenReviewOf(ctx, author)
	require.NoError(t, err)
	assert.Equal(t, review.ID, held.ID)

	st.releaseReview(t, review, VerdictCommented)

	_, err = st.OpenReviewOf(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound, "a batch that has gone is not still being written")
}

// TestDeletingAnAuthorsOpenReviewsLeavesWhatTheyReleased pins that a departing reviewer's draft goes and its release stays.
func TestDeletingAnAuthorsOpenReviewsLeavesWhatTheyReleased(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	reviewer := st.seedAgent(t, "reviewer")

	released := st.openReviewBy(t, st.openRound(t, agentID, "tree-one"), reviewer)
	st.releaseReview(t, released, VerdictChangesRequested)
	draft := st.openReviewBy(t, st.openRound(t, agentID, "tree-two"), reviewer)

	require.NoError(t, st.DeleteOpenReviewsBy(ctx, []string{reviewer}))

	_, err := st.GetReview(ctx, draft.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "the draft nobody will finish goes")

	kept, err := st.GetReview(ctx, released.ID)
	require.NoError(t, err)
	assert.Equal(t, released.ID, kept.ID, "the review the agent was already given stands")
}

// TestLiveReviewers pins which reviewers the work is still owed an answer by.
func TestLiveReviewers(t *testing.T) {
	tests := []struct {
		name     string
		leave    func(t *testing.T, st *fixture, seeded liveReviewerSeed)
		wantErr  error
		wantLive bool
	}{
		{
			name: "a reviewer that approved",
			leave: func(t *testing.T, st *fixture, seeded liveReviewerSeed) {
				st.releaseReview(t, st.openReviewBy(t, st.openRound(t, seeded.agentID, "tree-one"), seeded.reviewerID), VerdictChangesRequested)
				st.releaseReview(t, st.openReviewBy(t, st.openRound(t, seeded.agentID, "tree-two"), seeded.reviewerID), VerdictApproved)
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a reviewer whose setup failed",
			leave: func(t *testing.T, st *fixture, seeded liveReviewerSeed) {
				require.NoError(t, st.agents.SetStateByID(t.Context(), seeded.reviewerID, agentstore.StateFailedToCreate))
			},
			wantErr: store.ErrNotFound,
		},
		{
			name:     "a reviewer that has released nothing yet",
			leave:    func(*testing.T, *fixture, liveReviewerSeed) {},
			wantLive: true,
		},
		{
			name: "a reviewer that asked for changes",
			leave: func(t *testing.T, st *fixture, seeded liveReviewerSeed) {
				st.releaseReview(t, st.openReviewBy(t, st.openRound(t, seeded.agentID, "tree-one"), seeded.reviewerID), VerdictChangesRequested)
			},
			wantLive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			ctx := t.Context()
			agentID := st.seedAgent(t, "worker")
			seeded := liveReviewerSeed{
				agentID:    agentID,
				reviewerID: st.seedReviewerChild(t, agentID),
			}
			unread := st.seedAgent(t, "unread")

			tt.leave(t, st, seeded)

			_, err := st.GetLiveReviewerOf(ctx, unread)
			require.ErrorIs(t, err, store.ErrNotFound, "an agent nobody reviews has no reviewer")

			reading, err := st.GetLiveReviewerOf(ctx, agentID)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}

			live, err := st.ListLiveReviewers(ctx)
			require.NoError(t, err)

			if !tt.wantLive {
				assert.Empty(t, live)

				return
			}

			want := LiveReviewer{
				AgentID:    seeded.agentID,
				ReviewerID: seeded.reviewerID,
			}
			assert.Equal(t, []LiveReviewer{want}, live)
			assert.Equal(t, &want, reading)
		})
	}
}

// TestAReviewCountsWhatIsStillOwedAnAnswer pins the count a released review carries.
func TestAReviewCountsWhatIsStillOwedAnAnswer(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	review := st.openReview(t, st.openRound(t, agentID, "tree-one"))

	listed := st.findListedReview(t, review)
	assert.Zero(t, listed.OpenThreads, "a batch with no comments in it owes nothing")
	assert.False(t, listed.IsSettled(), "but an unsent review is not a finished one")

	first := st.openThread(t, review, "this needs a nil check")
	second := st.openThread(t, review, "and this reads the flag twice")
	st.releaseReview(t, review, VerdictChangesRequested)

	listed = st.findListedReview(t, review)
	assert.Equal(t, int32(2), listed.OpenThreads, "both comments are owed an answer")
	assert.False(t, listed.IsSettled())

	st.settle(t, first, nil)

	listed = st.findListedReview(t, review)
	assert.Equal(t, int32(1), listed.OpenThreads)
	assert.False(t, listed.IsSettled(), "a review is not finished while one comment stands")

	st.settle(t, second, nil)

	listed = st.findListedReview(t, review)
	assert.Zero(t, listed.OpenThreads)
	assert.True(t, listed.IsSettled(), "and is finished once none do")
}

func openFixture(t *testing.T) *fixture {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	workflows := workflowstore.NewWorkflowStore(pool.GetDB())
	created, err := workflows.Create(t.Context(), workflowstore.Config{
		Name:     "default",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
	})
	require.NoError(t, err)

	return &fixture{
		ReviewStore: NewReviewStore(pool.GetDB()),
		agents:      agentstore.NewAgentStore(pool.GetDB()),
		mail:        mailstore.NewMailStore(pool.GetDB()),
		pool:        pool.GetDB(),
		workflow:    created.ID,
	}
}

func (st *fixture) seedAgent(t *testing.T, name string) string {
	t.Helper()

	return st.seedAgentLike(t, agentstore.Agent{Name: name})
}

// seedReviewerChild is a reviewer attached to the agent whose work it reads.
func (st *fixture) seedReviewerChild(t *testing.T, parentID string) string {
	t.Helper()

	return st.seedAgentLike(t, agentstore.Agent{
		Name:          "worker-reviewer",
		Role:          agentstore.RoleReviewer,
		ParentAgentID: &parentID,
	})
}

func (st *fixture) seedAgentLike(t *testing.T, agent agentstore.Agent) string {
	t.Helper()

	agent.State = agentstore.StateIdle
	agent.WorkflowID = st.workflow
	require.NoError(t, st.agents.Create(t.Context(), agent))

	got, err := st.agents.GetInWorkflow(t.Context(), st.workflow, agent.Name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.ID
}

func (st *fixture) openRound(t *testing.T, agentID, tree string) *Round {
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

// openReview opens the person's review of a round.
func (st *fixture) openReview(t *testing.T, round *Round) *Review {
	t.Helper()

	review, err := st.OpenReview(t.Context(), OpenReviewParams{
		AgentID:    round.AgentID,
		RoundID:    round.ID,
		AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)
	require.NotNil(t, review)

	return review
}

// openReviewBy opens a reviewing agent's review of a round.
func (st *fixture) openReviewBy(t *testing.T, round *Round, reviewerID string) *Review {
	t.Helper()

	review, err := st.OpenReview(t.Context(), OpenReviewParams{
		AgentID:       round.AgentID,
		RoundID:       round.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &reviewerID,
	})
	require.NoError(t, err)
	require.NotNil(t, review)

	return review
}

func (st *fixture) openThread(t *testing.T, review *Review, body string) *Thread {
	t.Helper()

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		AgentID:     review.AgentID,
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

// writeAnswer has the agent under review answer in a thread, under no batch.
func (st *fixture) writeAnswer(t *testing.T, thread *Thread) *Message {
	t.Helper()

	answer, err := st.CreateMessage(t.Context(), CreateMessageParams{
		ThreadID:      thread.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &thread.AgentID,
		Body:          "done",
	})
	require.NoError(t, err)

	return answer
}

// settle settles a thread as the agent named, or as the person when none is.
func (st *fixture) settle(t *testing.T, thread *Thread, agentID *string) {
	t.Helper()

	byKind := AuthorHuman
	if agentID != nil {
		byKind = AuthorAgent
	}

	_, err := st.SetThreadResolved(t.Context(), SetThreadResolvedParams{
		ID:        thread.ID,
		Resolved:  true,
		ByKind:    byKind,
		ByAgentID: agentID,
	})
	require.NoError(t, err)
}

// releaseReview sends the doorbell a review is heard through and lets the review go under it, returning the mail's id.
func (st *fixture) releaseReview(t *testing.T, review *Review, verdict Verdict) string {
	t.Helper()

	sent, err := st.mail.Send(t.Context(), mailstore.SendParams{
		ToAgentID: review.AgentID,
		Body:      "komrade: review of round 1, changes requested, 1 comment(s)",
		Kind:      mailstore.KindReviewActivity,
		Deadline:  time.Hour,
	})
	require.NoError(t, err)
	require.NotNil(t, sent)

	_, err = st.ReleaseReview(t.Context(), ReleaseReviewParams{
		ID:             review.ID,
		Verdict:        verdict,
		Path:           "/tmp/round-1.md",
		DoorbellMailID: &sent.ID,
	})
	require.NoError(t, err)

	return sent.ID
}

// readDelivery is the state the list reports for a thread's first message by an author of the kind named.
func (st *fixture) readDelivery(t *testing.T, threadID string, kind AuthorKind) DeliveryState {
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

// markAllRead reads the person up to date on every thread of an agent.
func (st *fixture) markAllRead(t *testing.T, agentID string) {
	t.Helper()

	threads, err := st.ListThreads(t.Context(), agentID)
	require.NoError(t, err)

	for _, thread := range threads {
		_, err := st.MarkThreadRead(t.Context(), thread.ID)
		require.NoError(t, err)
	}
}

// findListedReview is one review read back off its agent's listing.
func (st *fixture) findListedReview(t *testing.T, review *Review) Review {
	t.Helper()

	listed, err := st.ListReviews(t.Context(), review.AgentID)
	require.NoError(t, err)

	for _, candidate := range listed {
		if candidate.ID == review.ID {
			return candidate
		}
	}

	t.Fatalf("the listing holds no review %q", review.ID)

	return Review{}
}
