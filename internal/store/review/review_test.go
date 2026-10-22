package review

import (
	"fmt"
	"testing"
	"time"

	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	pullrequeststore "komrad/internal/store/pullrequest"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	*ReviewStore
	stores   *storetest.Stores
	agents   *agentstore.AgentStore
	mail     *mailstore.MailStore
	pool     store.DBTX
	reader   string
	engineID uuid.UUID
	workflow uuid.UUID
}

// tallyRelease is one thing released about the tree named: the person's comment when verdict is
// empty, and otherwise a reviewer's review with that verdict.
type tallyRelease struct {
	tree    string
	verdict Verdict
}

// liveReviewerSeed is the writer and the reviewer on its task in a test of live reviewers.
type liveReviewerSeed struct {
	writerID   uuid.UUID
	reviewerID uuid.UUID
}

// TestASnapshotIsNumberedAfterTheOnesBeforeIt pins that the ordinal is the store's to decide, counted per task.
func TestASnapshotIsNumberedAfterTheOnesBeforeIt(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")

	first := st.takeSnapshot(t, agentID, "tree-one")
	second := st.takeSnapshot(t, agentID, "tree-two")
	elsewhere := st.takeSnapshot(t, st.seedAgent(t, "other"), "tree-one")

	assert.Equal(t, int32(1), first.Ordinal)
	assert.Equal(t, int32(2), second.Ordinal)
	assert.Equal(t, int32(1), elsewhere.Ordinal, "another task counts its own snapshots")

	latest, err := st.GetLatestSnapshot(t.Context(), st.taskOf(t, agentID))
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, second.ID, latest.ID)
}

// TestAnAuthorHoldsOneOpenReviewAtATime pins that the database refuses a second open review by the same reviewer.
func TestAnAuthorHoldsOneOpenReviewAtATime(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	reviewerID := st.seedReviewer(t, agentID)
	snapshot := st.takeSnapshot(t, agentID, "tree-one")

	st.openReviewBy(t, snapshot, reviewerID)

	_, err := st.OpenReview(ctx, OpenReviewParams{
		TaskID:        st.taskOf(t, agentID),
		SnapshotID:    snapshot.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &reviewerID,
	})
	require.Error(t, err, "a reviewer cannot write two reviews of one agent at once")

	other := st.seedAgent(t, "reviewer")
	_, err = st.OpenReview(ctx, OpenReviewParams{
		TaskID:        st.taskOf(t, agentID),
		SnapshotID:    snapshot.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &other,
	})
	require.NoError(t, err, "another reviewer reviewing the same snapshot is a review of its own")
}

// TestReleasingAReviewCarriesEveryCommentInIt pins that one release stamps every comment under it and none of the agent's answers.
func TestReleasingAReviewCarriesEveryCommentInIt(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	reviewerID := st.seedReviewer(t, agentID)
	review := st.openReviewBy(t, st.takeSnapshot(t, agentID, "tree-one"), reviewerID)

	first := st.openThread(t, review, "this needs a nil check")
	second := st.openThread(t, review, "and this one leaks")
	st.writeAnswer(t, first)

	count, err := st.CountOpenComments(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, count, "the agent's answer is in no batch")

	st.releaseReview(t, review, VerdictChangesRequested)

	assert.NotNil(t, st.messageBy(t, first.ID, reviewerID).ReviewReleasedAt)
	assert.NotNil(t, st.messageBy(t, second.ID, reviewerID).ReviewReleasedAt)
	assert.Nil(t, st.messageBy(t, first.ID, agentID).ReviewReleasedAt)
}

// TestAThreadWithNoReviewIsListed pins that a person's thread, which belongs to no review, is
// listed and fetched like any other.
func TestAThreadWithNoReviewIsListed(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	snapshot := st.takeSnapshot(t, agentID, "tree-one")
	opened := st.comment(t, snapshot, "this needs a nil check")

	threads, err := st.ListThreads(ctx, st.taskOf(t, agentID))
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.Nil(t, threads[0].ReviewID)
	assert.Equal(t, AuthorHuman, threads[0].AuthorKind)
	assert.Equal(t, snapshot.ID, threads[0].SnapshotID)

	got, err := st.GetThread(ctx, opened.ID)
	require.NoError(t, err)
	assert.Nil(t, got.ReviewID)
	assert.Equal(t, AuthorHuman, got.AuthorKind)
}

// TestReadingTheMailStampsTheComment pins that the comments a mail carried are stamped read once,
// against the snapshot taken as they were read.
func TestReadingTheMailStampsTheComment(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	thread := st.comment(t, st.takeSnapshot(t, agentID, "tree-one"), "this needs a nil check")
	sent := st.mailComment(t, thread)
	readOn := st.takeSnapshot(t, agentID, "tree-two")

	read, err := st.MarkMessagesRead(ctx, []uuid.UUID{sent}, &readOn.ID)
	require.NoError(t, err)
	require.Len(t, read, 1)
	assert.NotNil(t, read[0].ReadAt)
	assert.Equal(t, &readOn.ID, read[0].ReadSnapshotID)

	again, err := st.MarkMessagesRead(ctx, []uuid.UUID{sent}, nil)
	require.NoError(t, err)
	assert.Empty(t, again, "a comment is read once")

	messages, err := st.ListMessages(ctx, []uuid.UUID{thread.ID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, &sent, messages[0].MailID)
	assert.NotNil(t, messages[0].ReadAt)
	assert.Equal(t, &readOn.ID, messages[0].ReadSnapshotID)
}

// TestDeletingTheMailLeavesTheCommentRead pins that a comment stays read once the mail that
// carried it is gone.
func TestDeletingTheMailLeavesTheCommentRead(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	thread := st.comment(t, st.takeSnapshot(t, agentID, "tree-one"), "this needs a nil check")
	sent := st.mailComment(t, thread)

	_, err := st.MarkMessagesRead(ctx, []uuid.UUID{sent}, nil)
	require.NoError(t, err)
	require.NoError(t, st.mail.Delete(ctx, []uuid.UUID{sent}))

	messages, err := st.ListMessages(ctx, []uuid.UUID{thread.ID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Nil(t, messages[0].MailID)
	assert.NotNil(t, messages[0].ReadAt)
}

// TestThreadsAreReadByTask pins that a task's threads outlive the agent they were written to and stay off other tasks.
func TestThreadsAreReadByTask(t *testing.T) {
	tests := []struct {
		name   string
		retire func(t *testing.T, st *fixture, agentID uuid.UUID)
	}{
		{
			name: "the first writer archived",
			retire: func(t *testing.T, st *fixture, agentID uuid.UUID) {
				_, err := st.agents.Archive(t.Context(), []uuid.UUID{agentID})
				require.NoError(t, err)
			},
		},
		{
			name: "the first writer deleted",
			retire: func(t *testing.T, st *fixture, agentID uuid.UUID) {
				_, err := st.agents.Delete(t.Context(), []uuid.UUID{agentID})
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			ctx := t.Context()
			firstWriter := st.seedAgent(t, "worker")
			taskID := st.taskOf(t, firstWriter)
			snapshot := st.takeSnapshot(t, firstWriter, "tree-one")
			opened := st.comment(t, snapshot, "this needs a nil check")
			st.seedAgentLike(t, agentstore.Agent{
				Name:   "next-worker",
				TaskID: &taskID,
			})

			tt.retire(t, st, firstWriter)

			threads, err := st.ListThreads(ctx, taskID)
			require.NoError(t, err)
			require.Len(t, threads, 1)
			assert.Equal(t, opened.ID, threads[0].ID)
			snapshots, err := st.ListSnapshots(ctx, taskID)
			require.NoError(t, err)
			require.Len(t, snapshots, 1)
			assert.Equal(t, snapshot.ID, snapshots[0].ID)

			elsewhere, err := st.ListThreads(ctx, st.taskOf(t, st.seedAgent(t, "other")))
			require.NoError(t, err)
			assert.Empty(t, elsewhere)
		})
	}
}

// TestRewritingAReplyIsNewActivity pins that an answer rewritten after the person read it reads unread again.
func TestRewritingAReplyIsNewActivity(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")

	answered := st.writeAnswer(t, st.comment(t, st.takeSnapshot(t, agentID, "tree-one"), "this needs a nil check"))
	st.markAllRead(t, st.taskOf(t, agentID))

	_, err := st.UpdateMessage(ctx, answered.ID, "rewritten", nil)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, st.taskOf(t, agentID))
	require.NoError(t, err)
	require.Len(t, threads, 1)
	assert.True(t, threads[0].UnreadByHuman)
}

// TestMarkThreadReadCatchesUpOneSide keeps reading one conversation from claiming the rest.
func TestMarkThreadReadCatchesUpOneSide(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	snapshot := st.takeSnapshot(t, agentID, "tree-one")

	first := st.comment(t, snapshot, "first note")
	second := st.comment(t, snapshot, "second note")
	st.writeAnswer(t, first)
	st.writeAnswer(t, second)

	_, err := st.MarkThreadRead(ctx, first.ID)
	require.NoError(t, err)

	threads, err := st.ListThreads(ctx, st.taskOf(t, agentID))
	require.NoError(t, err)
	require.Len(t, threads, 2)

	byID := xslice.KeyBy(threads, func(thread Thread) uuid.UUID {
		return thread.ID
	})
	assert.False(t, byID[first.ID].UnreadByHuman, "the thread that was opened is caught up")
	assert.True(t, byID[second.ID].UnreadByHuman, "the one that was not stays unread")
}

// TestMissingRowsAnswerNotFound keeps an id naming nothing a not found rather than an internal failure.
func TestMissingRowsAnswerNotFound(t *testing.T) {
	tests := []struct {
		name string
		id   uuid.UUID
	}{
		{
			name: "a well formed id naming nothing",
			id:   uuid.New(),
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

			_, err = st.GetSnapshot(ctx, tt.id)
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
			TaskID:      st.taskOf(t, agentID),
			FilePath:    "greet.go",
			Fingerprint: fingerprint,
		})
		require.NoError(t, err)

		viewed, err := st.ListViewedFiles(ctx, st.taskOf(t, agentID))
		require.NoError(t, err)
		require.Len(t, viewed, 1)
		assert.Equal(t, fingerprint, viewed[0].Fingerprint)
	}

	require.NoError(t, st.UnmarkFileViewed(ctx, st.taskOf(t, agentID), "greet.go"))

	viewed, err := st.ListViewedFiles(ctx, st.taskOf(t, agentID))
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

// TestWritingInASettledThread pins whose words take a thread's settling back: a new message from
// the person always, a reviewer agent's only where it settled the thread itself, and the agent
// under review's never.
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
			name:        "the person rewriting what they said in what they settled, which says nothing new",
			settler:     "person",
			writer:      "person",
			rewrite:     true,
			wantSettled: true,
		},
		{
			name:    "the person writing in what the agent under review settled",
			settler: "worker",
			writer:  "person",
		},
		{
			name:        "the agent under review writing again in what its answer settled",
			settler:     "worker",
			writer:      "worker",
			wantSettled: true,
		},
		{
			name:        "the agent under review rewriting the answer that settled it",
			settler:     "worker",
			writer:      "worker",
			rewrite:     true,
			wantSettled: true,
		},
		{
			name:        "a reviewer agent writing in what the person settled",
			settler:     "person",
			writer:      "reviewer",
			wantSettled: true,
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
			thread := st.comment(t, st.takeSnapshot(t, workerID, "tree-one"), "this needs a nil check")

			agentIDs := map[string]*uuid.UUID{
				"person":   nil,
				"worker":   &workerID,
				"reviewer": &reviewerID,
			}
			writer := CreateMessageParams{
				ThreadID:      thread.ID,
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

// TestAnAnswerCarryingASnapshotSettlesTheThread pins that a reply naming the snapshot that answers it
// settles the thread in the same write.
func TestAnAnswerCarryingASnapshotSettlesTheThread(t *testing.T) {
	tests := []struct {
		name         string
		byReviewer   bool
		withSnapshot bool
		followUp     bool
		wantSettled  bool
	}{
		{
			name:         "a plain reply",
			withSnapshot: false,
			wantSettled:  false,
		},
		{
			name:         "a reply carrying a snapshot in a reviewer agent's thread",
			byReviewer:   true,
			withSnapshot: true,
			wantSettled:  false,
		},
		{
			name:         "a plain reply after the answer",
			withSnapshot: true,
			followUp:     true,
			wantSettled:  true,
		},
		{
			name:         "a reply carrying the snapshot that answers it",
			withSnapshot: true,
			wantSettled:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			ctx := t.Context()
			workerID := st.seedAgent(t, "worker")
			snapshot := st.takeSnapshot(t, workerID, "tree-one")

			var thread *Thread
			if tt.byReviewer {
				review := st.openReviewBy(t, snapshot, st.seedReviewer(t, workerID))
				thread = st.openThread(t, review, "this needs a nil check")
				st.releaseReview(t, review, VerdictChangesRequested)
			} else {
				thread = st.comment(t, snapshot, "this needs a nil check")
			}

			var snapshotID *uuid.UUID
			if tt.withSnapshot {
				snapshotID = &st.takeSnapshot(t, workerID, "tree-two").ID
			}

			answer, err := st.CreateMessage(ctx, CreateMessageParams{
				ThreadID:      thread.ID,
				SnapshotID:    snapshotID,
				AuthorKind:    AuthorAgent,
				AuthorAgentID: &workerID,
				Body:          "added the check",
			})
			require.NoError(t, err)
			assert.Equal(t, snapshotID, answer.SnapshotID)

			if tt.followUp {
				_, err := st.CreateMessage(ctx, CreateMessageParams{
					ThreadID:      thread.ID,
					AuthorKind:    AuthorAgent,
					AuthorAgentID: &workerID,
					Body:          "also renamed the helper",
				})
				require.NoError(t, err)
			}

			got, err := st.GetThread(ctx, thread.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSettled, got.ResolvedAt != nil)
			if tt.wantSettled {
				assert.Equal(t, AuthorAgent, got.ResolvedByKind)
				assert.Equal(t, &workerID, got.ResolvedByAgentID)
			}
		})
	}
}

// TestASuggestionIsCarriedWithTheComment pins that replacement lines survive the round trip.
func TestASuggestionIsCarriedWithTheComment(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	snapshot := st.takeSnapshot(t, agentID, "tree-one")

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		TaskID:     st.taskOf(t, agentID),
		SnapshotID: snapshot.ID,
		FilePath:   "internal/thing.go",
		NewStart:   41,
		NewEnd:     41,
		Body:       "guard this",
		Suggestion: []string{"if thing == nil {", "\treturn nil", "}"},
		AuthorKind: AuthorHuman,
	})
	require.NoError(t, err)

	messages, err := st.ListMessages(t.Context(), []uuid.UUID{thread.ID})
	require.NoError(t, err)
	require.Len(t, messages, 1)
	assert.Equal(t, []string{"if thing == nil {", "\treturn nil", "}"}, messages[0].Suggestion)
}

// TestListTallies pins which snapshots count against which agent and which conclusion stands with them.
func TestListTallies(t *testing.T) {
	tests := []struct {
		name      string
		releases  []tallyRelease
		openOn    string
		wantTally *tally
	}{
		{
			name: "work nobody has said anything about",
		},
		{
			name: "the person's comment, with no conclusion",
			releases: []tallyRelease{
				{
					tree: "tree-one",
				},
			},
			wantTally: &tally{
				Snapshots: 1,
			},
		},
		{
			name: "two snapshots the reviewer sent back, and a third still being written",
			releases: []tallyRelease{
				{
					tree:    "tree-one",
					verdict: VerdictChangesRequested,
				},
				{
					tree:    "tree-two",
					verdict: VerdictChangesRequested,
				},
			},
			openOn: "tree-three",
			wantTally: &tally{
				Snapshots:   2,
				Verdict:     xptr.Ptr(VerdictChangesRequested),
				SnapshotSHA: "snapshot-tree-two",
			},
		},
		{
			name: "the person and the reviewer on the one snapshot",
			releases: []tallyRelease{
				{
					tree: "tree-one",
				},
				{
					tree:    "tree-one",
					verdict: VerdictChangesRequested,
				},
			},
			wantTally: &tally{
				Snapshots:   1,
				Verdict:     xptr.Ptr(VerdictChangesRequested),
				SnapshotSHA: "snapshot-tree-one",
			},
		},
		{
			name: "an approval after changes requested",
			releases: []tallyRelease{
				{
					tree:    "tree-one",
					verdict: VerdictChangesRequested,
				},
				{
					tree:    "tree-two",
					verdict: VerdictApproved,
				},
			},
			wantTally: &tally{
				Snapshots:   2,
				Verdict:     xptr.Ptr(VerdictApproved),
				SnapshotSHA: "snapshot-tree-two",
			},
		},
		{
			name: "an approval with a later snapshot the person only commented on",
			releases: []tallyRelease{
				{
					tree:    "tree-one",
					verdict: VerdictApproved,
				},
				{
					tree: "tree-two",
				},
			},
			wantTally: &tally{
				Snapshots:   2,
				Verdict:     xptr.Ptr(VerdictApproved),
				SnapshotSHA: "snapshot-tree-one",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			agentID := st.seedAgent(t, "worker")
			reviewerID := st.seedReviewer(t, agentID)

			snapshots := map[string]*Snapshot{}
			for _, released := range tt.releases {
				if snapshots[released.tree] == nil {
					snapshots[released.tree] = st.takeSnapshot(t, agentID, released.tree)
				}
				if released.verdict == "" {
					st.comment(t, snapshots[released.tree], "a note")

					continue
				}

				st.releaseReview(t, st.openReviewBy(t, snapshots[released.tree], reviewerID), released.verdict)
			}
			if tt.openOn != "" {
				st.openReviewBy(t, st.takeSnapshot(t, agentID, tt.openOn), reviewerID)
			}

			tallies, err := st.listTallies(t.Context())
			require.NoError(t, err)

			if tt.wantTally == nil {
				assert.Empty(t, tallies)

				return
			}

			tt.wantTally.TaskID = st.taskOf(t, agentID)
			assert.Equal(t, []tally{*tt.wantTally}, tallies, "the reviewer holds no diff of its own to have gone snapshot")
		})
	}
}

// TestNewestSnapshotsAreEachTasksLastSnapshot pins the snapshot listed for each task, released or not.
func TestNewestSnapshotsAreEachTasksLastSnapshot(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	other := st.seedAgent(t, "other")
	st.seedAgent(t, "unreviewed")

	st.takeSnapshot(t, agentID, "tree-one")
	st.takeSnapshot(t, agentID, "tree-two")
	st.takeSnapshot(t, other, "tree-three")

	newest, err := st.ListNewestSnapshots(t.Context())
	require.NoError(t, err)

	assert.ElementsMatch(t, []newestSnapshot{
		{
			TaskID: st.taskOf(t, agentID),
			SHA:    "snapshot-tree-two",
		},
		{
			TaskID: st.taskOf(t, other),
			SHA:    "snapshot-tree-three",
		},
	}, newest)
}

// TestUnlandedSnapshotsAreTheReleasedOnesNotYetLanded pins which snapshots the landing walk is given and
// that landing a snapshot lands every earlier one with it.
func TestUnlandedSnapshotsAreTheReleasedOnesNotYetLanded(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	archived := st.seedAgent(t, "archived")

	for _, tree := range []string{"tree-one", "tree-two", "tree-three"} {
		st.comment(t, st.takeSnapshot(t, agentID, tree), "a note")
	}
	st.openReviewBy(t, st.takeSnapshot(t, agentID, "tree-draft"), st.seedReviewer(t, agentID))
	st.comment(t, st.takeSnapshot(t, archived, "tree-gone"), "a note")
	_, err := st.agents.Archive(ctx, []uuid.UUID{archived})
	require.NoError(t, err)

	unlanded, err := st.ListUnlandedSnapshots(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string][]UnlandedSnapshot{
		st.taskOf(t, agentID).String(): {
			{
				Ordinal: 3,
				BaseSHA: "base-tree-three",
				SHA:     "snapshot-tree-three",
			},
			{
				Ordinal: 2,
				BaseSHA: "base-tree-two",
				SHA:     "snapshot-tree-two",
			},
			{
				Ordinal: 1,
				BaseSHA: "base-tree-one",
				SHA:     "snapshot-tree-one",
			},
		},
	}, unlanded, "a snapshot only an open review names and an archived agent's snapshots are not walked")

	landed, err := st.MarkSnapshotsLanded(ctx, st.taskOf(t, agentID), 2)
	require.NoError(t, err)
	assert.Equal(t, int64(2), landed)

	again, err := st.MarkSnapshotsLanded(ctx, st.taskOf(t, agentID), 3)
	require.NoError(t, err)
	assert.Equal(t, int64(1), again, "a snapshot already landed keeps when it landed")

	unlanded, err = st.ListUnlandedSnapshots(ctx)
	require.NoError(t, err)
	assert.Empty(t, unlanded)

	reviews, err := st.ListWatched(ctx, nil)
	require.NoError(t, err)
	for _, review := range reviews {
		if review.TaskID == st.taskOf(t, agentID) && review.SnapshotOrdinal <= 3 {
			assert.NotNil(t, review.SnapshotLandedAt, "snapshot %d", review.SnapshotOrdinal)
		}
	}
}

// TestUnlandedSnapshotsCarryTheNewestMerge pins that an agent's unlanded snapshots say when its newest
// merged pull request merged.
func TestUnlandedSnapshotsCarryTheNewestMerge(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	st.comment(t, st.takeSnapshot(t, agentID, "tree-one"), "a note")

	repository, branch := "https://github.com/harrisoncramer/komrad", "agent/worker"
	st.stores.SeedTaskHead(t, agentID, taskstore.SetHeadParams{
		RepositoryURL: &repository,
		Branch:        &branch,
	})
	require.NoError(t, pullrequeststore.NewPullRequestSyncStore(st.pool).EnsureSync(ctx, repository))
	pullRequests := pullrequeststore.NewPullRequestStore(st.pool)
	earlier := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(24 * time.Hour)
	for number, mergedAt := range map[int]*time.Time{1: &earlier, 2: &later, 3: nil} {
		_, err := pullRequests.Upsert(ctx, pullrequeststore.UpsertParams{
			Host:              "github.com",
			Forge:             "github",
			ProviderID:        fmt.Sprintf("PR_%d", number),
			URL:               fmt.Sprintf("%s/pull/%d", repository, number),
			RepositoryURL:     repository,
			HeadRepositoryURL: repository,
			Number:            number,
			HeadBranch:        branch,
			BaseBranch:        "main",
			State:             "merged",
			OpenedAt:          earlier.Add(-time.Hour),
			ChangedAt:         earlier,
			MergedAt:          mergedAt,
		})
		require.NoError(t, err)
	}
	_, err := pullRequests.Observe(ctx)
	require.NoError(t, err)

	unlanded, err := st.ListUnlandedSnapshots(ctx)
	require.NoError(t, err)
	taskID := st.taskOf(t, agentID)
	require.Len(t, unlanded[taskID], 1)
	require.NotNil(t, unlanded[taskID][0].MergedAt)
	assert.True(t, later.Equal(*unlanded[taskID][0].MergedAt))
}

// TestTheOpenReviewIsTheOneStillBeingWritten pins that releasing a batch closes it.
func TestTheOpenReviewIsTheOneStillBeingWritten(t *testing.T) {
	st := openFixture(t)
	ctx := t.Context()
	agentID := st.seedAgent(t, "worker")
	reviewerID := st.seedReviewer(t, agentID)
	author := OpenReviewOfParams{
		TaskID:        st.taskOf(t, agentID),
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &reviewerID,
	}

	_, err := st.OpenReviewOf(ctx, author)
	require.ErrorIs(t, err, store.ErrNotFound)

	review := st.openReviewBy(t, st.takeSnapshot(t, agentID, "tree-one"), reviewerID)

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

	released := st.openReviewBy(t, st.takeSnapshot(t, agentID, "tree-one"), reviewer)
	st.releaseReview(t, released, VerdictChangesRequested)
	draft := st.openReviewBy(t, st.takeSnapshot(t, agentID, "tree-two"), reviewer)

	require.NoError(t, st.DeleteOpenReviewsBy(ctx, []uuid.UUID{reviewer}))

	_, err := st.GetReview(ctx, draft.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "the draft nobody will finish goes")

	kept, err := st.GetReview(ctx, released.ID)
	require.NoError(t, err)
	assert.Equal(t, released.ID, kept.ID, "the review the agent was already given stands")
}

// TestLiveReviewers pins which reviewers a task's work is still owed an answer by.
func TestLiveReviewers(t *testing.T) {
	tests := []struct {
		name     string
		leave    func(t *testing.T, st *fixture, seeded liveReviewerSeed)
		wantLive bool
	}{
		{
			name: "a reviewer that approved",
			leave: func(t *testing.T, st *fixture, seeded liveReviewerSeed) {
				st.releaseReview(t, st.openReviewBy(t, st.takeSnapshot(t, seeded.writerID, "tree-one"), seeded.reviewerID), VerdictChangesRequested)
				st.releaseReview(t, st.openReviewBy(t, st.takeSnapshot(t, seeded.writerID, "tree-two"), seeded.reviewerID), VerdictApproved)
			},
		},
		{
			name: "a reviewer whose setup failed",
			leave: func(t *testing.T, st *fixture, seeded liveReviewerSeed) {
				require.NoError(t, st.agents.SetStateByID(t.Context(), seeded.reviewerID, agentstore.StateFailedToCreate))
			},
		},
		{
			name:     "a reviewer that has released nothing yet",
			leave:    func(*testing.T, *fixture, liveReviewerSeed) {},
			wantLive: true,
		},
		{
			name: "a reviewer that asked for changes",
			leave: func(t *testing.T, st *fixture, seeded liveReviewerSeed) {
				st.releaseReview(t, st.openReviewBy(t, st.takeSnapshot(t, seeded.writerID, "tree-one"), seeded.reviewerID), VerdictChangesRequested)
			},
			wantLive: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openFixture(t)
			ctx := t.Context()
			writerID := st.seedAgent(t, "writer")
			seeded := liveReviewerSeed{
				writerID:   writerID,
				reviewerID: st.seedReviewer(t, writerID),
			}
			unread := st.seedAgent(t, "unread")

			tt.leave(t, st, seeded)

			other, err := st.ListLiveReviewers(ctx, st.taskOf(t, unread))
			require.NoError(t, err)
			assert.Empty(t, other, "a task nobody reviews has no reviewer")

			live, err := st.ListLiveReviewers(ctx, st.taskOf(t, writerID))
			require.NoError(t, err)

			if !tt.wantLive {
				assert.Empty(t, live)

				return
			}

			assert.Equal(t, []uuid.UUID{seeded.reviewerID}, live)
		})
	}
}

// TestLiveReviewersAreOldestFirst pins the order a task's live reviewers are listed in.
func TestLiveReviewersAreOldestFirst(t *testing.T) {
	st := openFixture(t)
	writerID := st.seedAgent(t, "writer")
	first := st.seedReviewer(t, writerID)
	taskID := st.taskOf(t, writerID)
	second := st.seedAgentLike(t, agentstore.Agent{
		Name:   "writer-second-reviewer",
		Role:   agentstore.RoleReviewer,
		TaskID: &taskID,
	})

	live, err := st.ListLiveReviewers(t.Context(), taskID)
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{first, second}, live)
}

// TestAReviewCountsWhatIsStillOwedAnAnswer pins the count a released review carries.
func TestAReviewCountsWhatIsStillOwedAnAnswer(t *testing.T) {
	st := openFixture(t)
	agentID := st.seedAgent(t, "worker")
	review := st.openReviewBy(t, st.takeSnapshot(t, agentID, "tree-one"), st.seedReviewer(t, agentID))

	listed := st.findListedReview(t, review)
	assert.Zero(t, listed.OpenThreads, "a batch with no comments in it owes nothing")
	assert.False(t, listed.isSettled(), "but an unsent review is not a finished one")

	first := st.openThread(t, review, "this needs a nil check")
	second := st.openThread(t, review, "and this reads the flag twice")
	st.releaseReview(t, review, VerdictChangesRequested)

	listed = st.findListedReview(t, review)
	assert.Equal(t, int32(2), listed.OpenThreads, "both comments are owed an answer")
	assert.False(t, listed.isSettled())

	st.settle(t, first, nil)

	listed = st.findListedReview(t, review)
	assert.Equal(t, int32(1), listed.OpenThreads)
	assert.False(t, listed.isSettled(), "a review is not finished while one comment stands")

	st.settle(t, second, nil)

	listed = st.findListedReview(t, review)
	assert.Zero(t, listed.OpenThreads)
	assert.True(t, listed.isSettled(), "and is finished once none do")
}

func openFixture(t *testing.T) *fixture {
	t.Helper()

	stores := storetest.Open(t)
	created := stores.SeedWorkflow(t, workflowstore.Config{
		Name:     "default",
		Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"},
	})

	return &fixture{
		ReviewStore: NewReviewStore(stores.Pool.GetDB()),
		stores:      stores,
		agents:      stores.Agents,
		mail:        stores.Mail,
		pool:        stores.Pool.GetDB(),
		reader:      stores.ReadOnlyURL,
		engineID:    stores.Pool.GetEngine().ID,
		workflow:    created.ID,
	}
}

func (st *fixture) seedAgent(t *testing.T, name string) uuid.UUID {
	t.Helper()

	return st.seedAgentLike(t, agentstore.Agent{Name: name})
}

// seedReviewer is a reviewer on the task of the writer whose work it reads.
func (st *fixture) seedReviewer(t *testing.T, writerID uuid.UUID) uuid.UUID {
	t.Helper()

	writer, err := st.agents.GetByID(t.Context(), writerID)
	require.NoError(t, err)
	taskID := st.taskOf(t, writerID)

	return st.seedAgentLike(t, agentstore.Agent{
		Name:   writer.Name + "-reviewer",
		Role:   agentstore.RoleReviewer,
		TaskID: &taskID,
	})
}

func (st *fixture) seedAgentLike(t *testing.T, agent agentstore.Agent) uuid.UUID {
	t.Helper()

	agent.State = agentstore.StateIdle
	agent.WorkflowID = st.workflow

	return st.stores.SeedAgent(t, agent).ID
}

// taskOf is the task an agent works on.
func (st *fixture) taskOf(t *testing.T, agentID uuid.UUID) uuid.UUID {
	t.Helper()

	agent, err := st.agents.GetByID(t.Context(), agentID)
	require.NoError(t, err)
	require.NotNil(t, agent.TaskID)

	return *agent.TaskID
}

// writerOf is the live writer of a task.
func (st *fixture) writerOf(t *testing.T, taskID uuid.UUID) uuid.UUID {
	t.Helper()

	writer, err := st.agents.GetTaskWriter(t.Context(), taskID)
	require.NoError(t, err)

	return writer.ID
}

func (st *fixture) takeSnapshot(t *testing.T, agentID uuid.UUID, tree string) *Snapshot {
	t.Helper()

	snapshot, err := st.CreateSnapshot(t.Context(), CreateSnapshotParams{
		TaskID:  st.taskOf(t, agentID),
		BaseSHA: "base-" + tree,
		SHA:     "snapshot-" + tree,
	})
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	return snapshot
}

// openReviewBy opens a reviewing agent's review of a snapshot.
func (st *fixture) openReviewBy(t *testing.T, snapshot *Snapshot, reviewerID uuid.UUID) *Review {
	t.Helper()

	review, err := st.OpenReview(t.Context(), OpenReviewParams{
		TaskID:        snapshot.TaskID,
		SnapshotID:    snapshot.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: &reviewerID,
	})
	require.NoError(t, err)
	require.NotNil(t, review)

	return review
}

// comment opens the person's thread on a snapshot, which belongs to no review.
func (st *fixture) comment(t *testing.T, snapshot *Snapshot, body string) *Thread {
	t.Helper()

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		TaskID:      snapshot.TaskID,
		SnapshotID:  snapshot.ID,
		FilePath:    "internal/thing.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tdoThing()"},
		Body:        body,
		AuthorKind:  AuthorHuman,
	})
	require.NoError(t, err)
	require.NotNil(t, thread)

	return &thread.Thread
}

// mailComment sends the mail that carries the person's comment opening a thread to the writer,
// returning the mail's id.
func (st *fixture) mailComment(t *testing.T, thread *Thread) uuid.UUID {
	t.Helper()

	sent, err := st.mail.Send(t.Context(), mailstore.SendParams{
		ToAgentID: st.writerOf(t, thread.TaskID),
		Body:      "this needs a nil check",
		Kind:      mailstore.KindReviewComment,
	})
	require.NoError(t, err)
	require.NotNil(t, sent)

	messages, err := st.ListMessages(t.Context(), []uuid.UUID{thread.ID})
	require.NoError(t, err)
	require.NotEmpty(t, messages)
	require.NoError(t, st.SetMessageMail(t.Context(), messages[0].ID, sent.ID))

	return sent.ID
}

// messageBy is the first message an agent wrote in a thread.
func (st *fixture) messageBy(t *testing.T, threadID, agentID uuid.UUID) Message {
	t.Helper()

	messages, err := st.ListMessages(t.Context(), []uuid.UUID{threadID})
	require.NoError(t, err)

	for _, message := range messages {
		if message.AuthorAgentID != nil && *message.AuthorAgentID == agentID {
			return message
		}
	}

	t.Fatalf("no message by %q in thread %q", agentID, threadID)

	return Message{}
}

// openThread opens a thread in a reviewer agent's review.
func (st *fixture) openThread(t *testing.T, review *Review, body string) *Thread {
	t.Helper()

	thread, err := st.CreateThread(t.Context(), CreateThreadParams{
		TaskID:        review.TaskID,
		ReviewID:      &review.ID,
		SnapshotID:    review.SnapshotID,
		FilePath:      "internal/thing.go",
		NewStart:      41,
		NewEnd:        41,
		QuotedLines:   []string{"+\tdoThing()"},
		Body:          body,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: review.AuthorAgentID,
	})
	require.NoError(t, err)
	require.NotNil(t, thread)

	return &thread.Thread
}

// writeAnswer has the agent under review answer in a thread, under no batch.
func (st *fixture) writeAnswer(t *testing.T, thread *Thread) *Message {
	t.Helper()

	answer, err := st.CreateMessage(t.Context(), CreateMessageParams{
		ThreadID:      thread.ID,
		AuthorKind:    AuthorAgent,
		AuthorAgentID: xptr.Ptr(st.writerOf(t, thread.TaskID)),
		Body:          "done",
	})
	require.NoError(t, err)

	return answer
}

// settle settles a thread as the agent named, or as the person when none is.
func (st *fixture) settle(t *testing.T, thread *Thread, agentID *uuid.UUID) {
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

// releaseReview lets a reviewer agent's review go.
func (st *fixture) releaseReview(t *testing.T, review *Review, verdict Verdict) {
	t.Helper()

	_, err := st.ReleaseReview(t.Context(), ReleaseReviewParams{
		ID:      review.ID,
		Verdict: verdict,
		Path:    "/tmp/snapshot-1.md",
	})
	require.NoError(t, err)
}

// markAllRead reads the person up to date on every thread of a task.
func (st *fixture) markAllRead(t *testing.T, taskID uuid.UUID) {
	t.Helper()

	threads, err := st.ListThreads(t.Context(), taskID)
	require.NoError(t, err)

	for _, thread := range threads {
		_, err := st.MarkThreadRead(t.Context(), thread.ID)
		require.NoError(t, err)
	}
}

// findListedReview is one review read back off its task's listing.
func (st *fixture) findListedReview(t *testing.T, review *Review) Review {
	t.Helper()

	listed, err := st.ListReviews(t.Context(), review.TaskID)
	require.NoError(t, err)

	for _, candidate := range listed {
		if candidate.ID == review.ID {
			return candidate
		}
	}

	t.Fatalf("the listing holds no review %q", review.ID)

	return Review{}
}
