package reviews

import (
	"context"
	"slices"
	"testing"

	"komrad/internal/eventprompts"
	"komrad/internal/events/eventstest"
	"komrad/internal/gittest"
	"komrad/internal/jobs/internaljobs"
	"komrad/internal/reviews/history"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/tasks"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	firstSnapshot  = "1111111111111111111111111111111111111111"
	secondSnapshot = "2222222222222222222222222222222222222222"
	baseSnapshot   = "0000000000000000000000000000000000000000"
)

var personAuthor = ReviewAuthor{Kind: reviewstore.AuthorHuman}

// reviewFixture is a writer with snapshot one taken, its reviewer, and the writer of another task.
type reviewFixture struct {
	service     *ReviewService
	reviews     *reviewstore.ReviewStore
	stores      *storetest.Stores
	snapshots   *gittest.Snapshots
	events      *eventstest.Queue
	interrupter *recordingInterrupter
	writer      *agentstore.Agent
	reviewer    *agentstore.Agent
	otherWriter *agentstore.Agent
}

// TestReviewEntitlement pins who may make each review call on whose work.
func TestReviewEntitlement(t *testing.T) {
	tests := []struct {
		name    string
		call    func(t *testing.T, fx *reviewFixture) error
		wantErr error
	}{
		{
			name: "an agent replying in a review of somebody else's work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
					ThreadID: fx.releaseFirstReview(t).ID,
					Body:     "done",
					Author:   newAgentAuthor(fx.otherWriter),
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "an agent opening a thread on its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					Task:     *fx.writer.TaskID,
					FilePath: "greet.go",
					Body:     "looks fine to me",
					Author:   newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewOwnWork,
		},
		{
			name: "an agent settling a comment on its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.SetReviewThreadResolved(t.Context(), SetReviewThreadResolvedParams{
					ID:       fx.releaseFirstReview(t).ID,
					Resolved: true,
					Author:   newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "an agent releasing a review of its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					Task:    *fx.writer.TaskID,
					Verdict: reviewstore.VerdictApproved,
					Author:  newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewOwnWork,
		},
		{
			name: "an agent marking a thread read for the person",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.MarkReviewThreadRead(t.Context(), MarkReviewThreadReadParams{
					ID:     fx.releaseFirstReview(t).ID,
					Author: newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "an agent rewriting what the person wrote",
			call: func(t *testing.T, fx *reviewFixture) error {
				thread := fx.releaseFirstReview(t)
				messages, err := fx.reviews.ListMessages(t.Context(), []string{thread.ID})
				require.NoError(t, err)

				_, err = fx.service.EditReviewMessage(t.Context(), EditReviewMessageParams{
					ID:     messages[0].ID,
					Body:   "rewritten",
					Author: newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "the person rewriting the agent's answer",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.EditReviewMessage(t.Context(), EditReviewMessageParams{
					ID:     fx.writeAnswer(t, fx.releaseFirstReview(t).ID).ID,
					Body:   "rewritten",
					Author: personAuthor,
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "an agent rewriting another agent's answer",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.EditReviewMessage(t.Context(), EditReviewMessageParams{
					ID:     fx.writeAnswer(t, fx.releaseFirstReview(t).ID).ID,
					Body:   "rewritten",
					Author: newAgentAuthor(fx.otherWriter),
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "rewriting a message nothing holds",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.EditReviewMessage(t.Context(), EditReviewMessageParams{
					ID:     uuid.NewString(),
					Body:   "rewritten",
					Author: personAuthor,
				})

				return err
			},
			wantErr: ErrReviewMessageNotFound,
		},
		{
			name: "a reviewer writing on a task it does not review",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					Task:     *fx.otherWriter.TaskID,
					FilePath: "greet.go",
					Body:     "this needs a nil check",
					Author:   newAgentAuthor(fx.reviewer),
				})

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent taking a snapshot",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.TakeSnapshot(t.Context(), *fx.writer.TaskID, newAgentAuthor(fx.otherWriter))

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent listing the reviews",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListReviews(t.Context(), *fx.writer.TaskID, newAgentAuthor(fx.otherWriter))

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent opening the history",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.GetHistory(t.Context(), GetHistoryParams{
					Task:   *fx.writer.TaskID,
					Author: newAgentAuthor(fx.otherWriter),
				})

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent reading a released review",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.GetReview(t.Context(), fx.releaseFirstReview(t).ReviewID, newAgentAuthor(fx.otherWriter))

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an agent marking a file viewed",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.SetReviewFileViewed(t.Context(), SetReviewFileViewedParams{
					Task:        *fx.writer.TaskID,
					FilePath:    "greet.go",
					Fingerprint: "abc123",
					Viewed:      true,
					Author:      newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "an agent listing the files viewed",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListReviewViewedFiles(t.Context(), ListReviewViewedFilesParams{
					Task:   *fx.writer.TaskID,
					Author: newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "settling a thread nothing holds",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.SetReviewThreadResolved(t.Context(), SetReviewThreadResolvedParams{
					ID:       uuid.NewString(),
					Resolved: true,
					Author:   personAuthor,
				})

				return err
			},
			wantErr: ErrReviewThreadNotFound,
		},
		{
			name: "marking read a thread nothing holds",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.MarkReviewThreadRead(t.Context(), MarkReviewThreadReadParams{
					ID:     uuid.NewString(),
					Author: personAuthor,
				})

				return err
			},
			wantErr: ErrReviewThreadNotFound,
		},
		{
			name: "the person naming no task",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListReviews(t.Context(), "", personAuthor)

				return err
			},
			wantErr: errReviewUnnamedTask,
		},
		{
			name: "taking a snapshot of a task with no directory",
			call: func(t *testing.T, fx *reviewFixture) error {
				homeless := fx.stores.SeedAgent(t, agentstore.Agent{Name: "homeless"})
				_, err := fx.service.TakeSnapshot(t.Context(), *homeless.TaskID, personAuthor)

				return err
			},
			wantErr: ErrReviewNoSnapshot,
		},
		{
			name: "taking a snapshot of a task whose worktree was removed",
			call: func(t *testing.T, fx *reviewFixture) error {
				require.NoError(t, fx.stores.Tasks.SetHead(t.Context(), *fx.writer.TaskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskstore.WorktreeGone}))
				_, err := fx.service.TakeSnapshot(t.Context(), *fx.writer.TaskID, personAuthor)

				return err
			},
			wantErr: errReviewWorktreeRemoved,
		},
		{
			name: "reading a removed worktree's review in a workflow with no directory",
			call: func(t *testing.T, fx *reviewFixture) error {
				fx.removeWorktree(t, "")
				_, err := fx.service.GetHistory(t.Context(), GetHistoryParams{
					Task:   *fx.writer.TaskID,
					Author: personAuthor,
				})

				return err
			},
			wantErr: errReviewNoSnapshotDir,
		},
		{
			name: "a reviewer releasing notes with nothing in them",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					Task:    *fx.writer.TaskID,
					Verdict: reviewstore.VerdictCommented,
					Author:  newAgentAuthor(fx.reviewer),
				})

				return err
			},
			wantErr: ErrReviewEmpty,
		},
		{
			name: "a reviewer releasing with no verdict",
			call: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					Task:   *fx.writer.TaskID,
					Author: newAgentAuthor(fx.reviewer),
				})

				return err
			},
			wantErr: errReviewVerdictMissing,
		},
		{
			name: "the person releasing with a verdict",
			call: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					Task:    *fx.writer.TaskID,
					Verdict: reviewstore.VerdictApproved,
					Author:  personAuthor,
				})

				return err
			},
			wantErr: ErrReviewVerdictFromPerson,
		},
		{
			name: "a comment read from a commit no snapshot was taken of",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					Task:        *fx.writer.TaskID,
					SnapshotSHA: secondSnapshot,
					FilePath:    "greet.go",
					Body:        "this rule lives in two places",
					Author:      personAuthor,
				})

				return err
			},
			wantErr: ErrReviewNotCurrent,
		},
		{
			name: "a comment on no file",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					Task:   *fx.writer.TaskID,
					Body:   "this whole change needs a rethink",
					Author: personAuthor,
				})

				return err
			},
			wantErr: errReviewThreadNoFile,
		},
		{
			name: "reading a review nothing holds",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.GetReview(t.Context(), uuid.NewString(), personAuthor)

				return err
			},
			wantErr: ErrReviewNotFound,
		},
		{
			name: "the agent under review rewriting its own answer",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.EditReviewMessage(t.Context(), EditReviewMessageParams{
					ID:     fx.writeAnswer(t, fx.releaseFirstReview(t).ID).ID,
					Body:   "done, and tested",
					Author: newAgentAuthor(fx.writer),
				})

				return err
			},
		},
		{
			name: "the person rewriting their own comment",
			call: func(t *testing.T, fx *reviewFixture) error {
				thread := fx.releaseFirstReview(t)
				messages, err := fx.reviews.ListMessages(t.Context(), []string{thread.ID})
				require.NoError(t, err)

				_, err = fx.service.EditReviewMessage(t.Context(), EditReviewMessageParams{
					ID:     messages[0].ID,
					Body:   "this needs a nil check, or a guard above it",
					Author: personAuthor,
				})

				return err
			},
		},
		{
			name: "the person sending with nothing written",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					Task:   *fx.writer.TaskID,
					Author: personAuthor,
				})

				return err
			},
		},
		{
			name: "the person marking a thread read",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.MarkReviewThreadRead(t.Context(), MarkReviewThreadReadParams{
					ID:     fx.releaseFirstReview(t).ID,
					Author: personAuthor,
				})

				return err
			},
		},
		{
			name: "the agent under review listing its own snapshots",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListSnapshots(t.Context(), *fx.writer.TaskID, newAgentAuthor(fx.writer))

				return err
			},
		},
		{
			name: "the agent under review taking a snapshot of its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.TakeSnapshot(t.Context(), *fx.writer.TaskID, newAgentAuthor(fx.writer))

				return err
			},
		},
		{
			name: "a reviewer naming no task taking a snapshot of the work it reviews",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.TakeSnapshot(t.Context(), "", newAgentAuthor(fx.reviewer))

				return err
			},
		},
		{
			name: "a reviewer naming no task listing the reviews of the work it reviews",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListReviews(t.Context(), "", newAgentAuthor(fx.reviewer))

				return err
			},
		},
		{
			name: "a reviewer naming no task commenting on the work it reviews",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					FilePath: "greet.go",
					Body:     "say who it greets",
					Author:   newAgentAuthor(fx.reviewer),
				})

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			err := tt.call(t, fx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestListReviewThreads pins that an agent reads only what was released and what it wrote itself.
func TestListReviewThreads(t *testing.T) {
	tests := []struct {
		name        string
		reader      func(fx *reviewFixture) ReviewAuthor
		wantThreads [][]string
	}{
		{
			name: "the agent under review",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			wantThreads: [][]string{{"this needs a nil check", "done"}},
		},
		{
			name: "an agent reading somebody else's review",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.otherWriter)
			},
			wantThreads: [][]string{{"this needs a nil check", "done"}},
		},
		{
			name: "the reviewer, which reads its own draft back",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			wantThreads: [][]string{{"this needs a nil check", "done"}, {"and the caller above it"}},
		},
		{
			name: "the person, who reads every draft",
			reader: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
			wantThreads: [][]string{
				{"this needs a nil check", "done", "and I have not decided about this one"},
				{"and the caller above it"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			released := fx.releaseFirstReview(t)
			fx.writeAnswer(t, released.ID)
			_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
				ThreadID: released.ID,
				Body:     "and I have not decided about this one",
				Author:   personAuthor,
			})
			require.NoError(t, err)

			fx.writeComment(t, newAgentAuthor(fx.reviewer), "and the caller above it")

			reader := tt.reader(fx)
			threads, err := fx.service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
				Task:   *fx.writer.TaskID,
				Author: reader,
			})
			require.NoError(t, err)

			bodies := xslice.Map(threads, func(thread ReviewThreadWithMessages) []string {
				return readBodies(thread.Messages)
			})
			assert.Equal(t, tt.wantThreads, bodies)

			if reader.isHuman() {
				assert.NotNil(t, threads[0].Messages[0].SentAt, "the person keeps their record of when")

				return
			}

			assert.False(t, threads[0].Thread.UnreadByHuman, "whether the person has read the reply is their own progress")
			for _, message := range threads[0].Messages {
				assert.Nil(t, message.SentAt, "when the person let a comment go is theirs to know")
				assert.Equal(t, reviewstore.DeliveryNone, message.Delivery)
			}
		})
	}
}

// TestCreateReviewThreadPicksTheSnapshot pins which snapshot a comment is filed under.
func TestCreateReviewThreadPicksTheSnapshot(t *testing.T) {
	tests := []struct {
		name        string
		write       func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages
		wantOrdinal int32
	}{
		{
			name: "naming the commit of a snapshot",
			write: func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages {
				fx.advanceTree(t)

				return fx.writeCommentOn(t, personAuthor, firstSnapshot)
			},
			wantOrdinal: 1,
		},
		{
			name: "naming the snapshot it holds comments on after somebody took a newer snapshot",
			write: func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages {
				fx.writeCommentOn(t, personAuthor, firstSnapshot)
				fx.advanceTree(t)

				return fx.writeCommentOn(t, personAuthor, firstSnapshot)
			},
			wantOrdinal: 1,
		},
		{
			name: "naming no snapshot while holding comments on an older snapshot",
			write: func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
				fx.advanceTree(t)

				return fx.writeComment(t, newAgentAuthor(fx.reviewer), "and this one")
			},
			wantOrdinal: 1,
		},
		{
			name: "naming no snapshot and holding nothing",
			write: func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages {
				fx.advanceTree(t)

				return fx.writeComment(t, personAuthor, "this needs a nil check")
			},
			wantOrdinal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			written := tt.write(t, fx)

			snapshot, err := fx.reviews.GetSnapshot(t.Context(), written.Thread.SnapshotID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrdinal, snapshot.Ordinal)
		})
	}
}

// TestASecondReview pins when an author may start another review of the same agent.
func TestASecondReview(t *testing.T) {
	tests := []struct {
		name        string
		write       func(t *testing.T, fx *reviewFixture) error
		wantErr     error
		wantErrText string
	}{
		{
			name: "the reviewer on a snapshot it has reviewed",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
				fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)

				return fx.tryComment(t, newAgentAuthor(fx.reviewer), "")
			},
			wantErr: ErrReviewAlreadyGiven,
		},
		{
			name: "the reviewer going back to a snapshot it reviewed",
			write: func(t *testing.T, fx *reviewFixture) error {
				reviewer := newAgentAuthor(fx.reviewer)
				fx.writeComment(t, reviewer, "this needs a nil check")
				fx.releaseReview(t, reviewer, reviewstore.VerdictChangesRequested)
				fx.advanceTree(t)
				fx.writeComment(t, reviewer, "and this one")
				fx.releaseReview(t, reviewer, reviewstore.VerdictChangesRequested)

				return fx.tryComment(t, reviewer, firstSnapshot)
			},
			wantErr: ErrReviewAlreadyGiven,
		},
		{
			name: "a comment on a newer snapshot while holding comments on an older one",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
				fx.advanceTree(t)

				return fx.tryComment(t, newAgentAuthor(fx.reviewer), secondSnapshot)
			},
			wantErr:     errReviewInFlight,
			wantErrText: "snapshot 1",
		},
		{
			name: "the person commenting on a newer snapshot while holding comments on an older one",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				fx.advanceTree(t)

				return fx.tryComment(t, personAuthor, secondSnapshot)
			},
		},
		{
			name: "the person commenting on an older snapshot while holding comments on a newer one",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.advanceTree(t)
				fx.writeCommentOn(t, personAuthor, secondSnapshot)

				return fx.tryComment(t, personAuthor, firstSnapshot)
			},
		},
		{
			name: "the person answering back in the thread",
			write: func(t *testing.T, fx *reviewFixture) error {
				released := fx.releaseFirstReview(t)
				fx.writeAnswer(t, released.ID)

				_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
					ThreadID: released.ID,
					Body:     "the MCP path can, see review.go",
					Author:   personAuthor,
				})

				return err
			},
		},
		{
			name: "the person while the agent has not answered the first",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.releaseFirstReview(t)

				return fx.tryComment(t, personAuthor, "")
			},
		},
		{
			name: "the person once they settled the first",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.settle(t, fx.releaseFirstReview(t).ID, personAuthor)

				return fx.tryComment(t, personAuthor, "")
			},
		},
		{
			name: "the person once they settled the first, whatever the agent wrote there after",
			write: func(t *testing.T, fx *reviewFixture) error {
				released := fx.releaseFirstReview(t)
				fx.settle(t, released.ID, personAuthor)
				fx.writeAnswer(t, released.ID)

				return fx.tryComment(t, personAuthor, "")
			},
		},
		{
			name: "the reviewer once the agent has written more",
			write: func(t *testing.T, fx *reviewFixture) error {
				reviewer := newAgentAuthor(fx.reviewer)
				fx.writeComment(t, reviewer, "this needs a nil check")
				fx.releaseReview(t, reviewer, reviewstore.VerdictChangesRequested)
				fx.advanceTree(t)

				return fx.tryComment(t, reviewer, "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			err := tt.write(t, fx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.ErrorContains(t, err, tt.wantErrText)

				return
			}

			require.NoError(t, err)
		})
	}
}

// TestSetReviewThreadResolved pins who settles a reviewer's comment and what the thread records of it.
func TestSetReviewThreadResolved(t *testing.T) {
	tests := []struct {
		name         string
		settler      func(fx *reviewFixture) ReviewAuthor
		reopen       bool
		wantKind     reviewstore.AuthorKind
		wantSettler  func(fx *reviewFixture) *string
		wantResolved bool
	}{
		{
			name: "the reviewer takes its settling back",
			settler: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			reopen: true,
			wantSettler: func(*reviewFixture) *string {
				return nil
			},
		},
		{
			name: "the person overrules the reviewer",
			settler: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
			wantKind: reviewstore.AuthorHuman,
			wantSettler: func(*reviewFixture) *string {
				return nil
			},
			wantResolved: true,
		},
		{
			name: "the reviewer settles its own comment",
			settler: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.reviewer)
			},
			wantKind: reviewstore.AuthorAgent,
			wantSettler: func(fx *reviewFixture) *string {
				return &fx.reviewer.ID
			},
			wantResolved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			written := fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
			fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)

			settled := fx.settle(t, written.Thread.ID, tt.settler(fx))
			if tt.reopen {
				reopened, err := fx.service.SetReviewThreadResolved(t.Context(), SetReviewThreadResolvedParams{
					ID:       written.Thread.ID,
					Resolved: false,
					Author:   tt.settler(fx),
				})
				require.NoError(t, err)

				settled = reopened
			}

			assert.Equal(t, tt.wantResolved, settled.ResolvedAt != nil)
			assert.Equal(t, tt.wantKind, settled.ResolvedByKind)
			assert.Equal(t, tt.wantSettler(fx), settled.ResolvedByAgentID)
		})
	}
}

// TestReviewFileViewed pins that the person marks a file read and takes the mark back.
func TestReviewFileViewed(t *testing.T) {
	fx := newReviewFixture(t)

	_, err := fx.service.SetReviewFileViewed(t.Context(), SetReviewFileViewedParams{
		Task:        *fx.writer.TaskID,
		FilePath:    "greet.go",
		Fingerprint: "abc123",
		Viewed:      true,
		Author:      personAuthor,
	})
	require.NoError(t, err)

	viewed, err := fx.service.ListReviewViewedFiles(t.Context(), ListReviewViewedFilesParams{
		Task:   *fx.writer.TaskID,
		Author: personAuthor,
	})
	require.NoError(t, err)
	require.Len(t, viewed, 1)
	assert.Equal(t, "greet.go", viewed[0].FilePath)

	_, err = fx.service.SetReviewFileViewed(t.Context(), SetReviewFileViewedParams{
		Task:     *fx.writer.TaskID,
		FilePath: "greet.go",
		Author:   personAuthor,
	})
	require.NoError(t, err)

	viewed, err = fx.service.ListReviewViewedFiles(t.Context(), ListReviewViewedFilesParams{
		Task:   *fx.writer.TaskID,
		Author: personAuthor,
	})
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

// TestAnsweringAReviewRingsTheReviewer pins that the writer's answer, and only its answer, tells and wakes the reviewer whose thread it answered.
func TestAnsweringAReviewRingsTheReviewer(t *testing.T) {
	tests := []struct {
		name     string
		opener   func(t *testing.T, fx *reviewFixture) *agentstore.Agent
		author   func(fx *reviewFixture) ReviewAuthor
		muted    bool
		wantRung bool
	}{
		{
			name: "the person writing in the thread",
			opener: func(_ *testing.T, fx *reviewFixture) *agentstore.Agent {
				return fx.reviewer
			},
			author: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
		},
		{
			name: "the writer answering",
			opener: func(_ *testing.T, fx *reviewFixture) *agentstore.Agent {
				return fx.reviewer
			},
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			wantRung: true,
		},
		{
			name: "the writer answering when the workflow mutes answers",
			opener: func(_ *testing.T, fx *reviewFixture) *agentstore.Agent {
				return fx.reviewer
			},
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			muted: true,
		},
		{
			name: "the writer answering a thread the person opened",
			opener: func(*testing.T, *reviewFixture) *agentstore.Agent {
				return nil
			},
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
		},
		{
			name: "the writer answering a thread a second reviewer opened",
			opener: func(t *testing.T, fx *reviewFixture) *agentstore.Agent {
				return fx.stores.SeedAgent(t, agentstore.Agent{
					Name:       "second-reviewer",
					Cwd:        "/tree",
					WorkflowID: fx.writer.WorkflowID,
					Role:       agentstore.RoleReviewer,
					TaskID:     fx.writer.TaskID,
				})
			},
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			wantRung: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			if tt.muted {
				workflow, err := fx.stores.Workflows.Get(t.Context(), fx.writer.WorkflowID)
				require.NoError(t, err)
				cfg := workflow.Config()
				cfg.EventPrompts = []workflowstore.EventPrompt{{Event: eventprompts.ReviewAnswered, Muted: true}}
				require.NoError(t, fx.stores.Workflows.Update(t.Context(), workflow.ID, cfg))
			}
			opener := tt.opener(t, fx)
			opening := personAuthor
			verdict := reviewstore.Verdict("")
			if opener != nil {
				opening = newAgentAuthor(opener)
				verdict = reviewstore.VerdictChangesRequested
			}
			written := fx.writeComment(t, opening, "this needs a nil check")
			fx.releaseReview(t, opening, verdict)

			_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
				ThreadID: written.Thread.ID,
				Body:     "fixed, the lookahead now stops at the newline",
				Author:   tt.author(fx),
			})
			require.NoError(t, err)

			if opener != fx.reviewer {
				assert.Empty(t, fx.listMail(t, fx.reviewer.ID), "a reviewer whose thread was not answered is not told")
			}

			if !tt.wantRung {
				if opener != nil {
					assert.Empty(t, fx.listMail(t, opener.ID))
				}
				assert.Empty(t, fx.events.ListFollowUps())

				return
			}

			assert.Equal(t, []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: opener.ID}}, fx.events.ListFollowUps())

			rung := fx.listMail(t, opener.ID)
			require.Len(t, rung, 1)
			assert.Equal(t, mailstore.KindReviewActivity, rung[0].Kind)
			assert.Equal(t, &fx.writer.ID, rung[0].SubjectID)
			assert.Contains(t, rung[0].Body, "list_review_threads", "the doorbell says where to look rather than what was said")
			assert.Equal(t, []string{eventprompts.ReviewAnswered}, rung[0].Events)
		})
	}
}

// TestAnsweringAReviewSeveralTimesLeavesOneMail pins that answers the reviewer has not read yet are one mail.
func TestAnsweringAReviewSeveralTimesLeavesOneMail(t *testing.T) {
	fx := newReviewFixture(t)
	written := fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
	fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)

	for range 3 {
		fx.writeAnswer(t, written.Thread.ID)
	}

	claimed, err := fx.stores.Mail.Claim(t.Context(), fx.reviewer.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	fx.writeAnswer(t, written.Thread.ID)

	assert.Len(t, fx.listMail(t, fx.reviewer.ID), 2, "an answer after the mail was read rings again")
}

type recordingInterrupter struct {
	interrupted []string
}

func (r *recordingInterrupter) Interrupt(_ context.Context, agentID string) error {
	r.interrupted = append(r.interrupted, agentID)

	return nil
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()

	return newReviewFixtureReleasing(t, NewReleasedHandlerParams{ArchiveOnApproval: true})
}

func newReviewFixtureReleasing(t *testing.T, params NewReleasedHandlerParams) *reviewFixture {
	t.Helper()

	stores := storetest.Open(t)
	reviews := reviewstore.NewReviewStore(stores.Pool.GetDB())
	snapshots := &gittest.Snapshots{
		SHA:     firstSnapshot,
		BaseSHA: baseSnapshot,
		Files:   map[string][]string{},
	}
	events := eventstest.NewQueue(t, stores, NewReleasedHandler(params), NewAnsweredHandler())
	interrupter := &recordingInterrupter{}

	writer := stores.SeedAgent(t, agentstore.Agent{
		Name: "writer",
		Cwd:  "/tree",
	})
	reviewer := stores.SeedAgent(t, agentstore.Agent{
		Name:       "reviewer",
		Cwd:        "/tree",
		WorkflowID: writer.WorkflowID,
		Role:       agentstore.RoleReviewer,
		TaskID:     writer.TaskID,
	})
	otherWriter := stores.SeedAgent(t, agentstore.Agent{
		Name: "other-writer",
		Cwd:  "/elsewhere",
	})
	require.NoError(t, stores.Tasks.SetCheckoutDir(t.Context(), *writer.TaskID, "/tree"))

	service := NewReviewService(NewReviewServiceParams{
		Reviews:     reviews,
		Snapshots:   snapshots,
		Workflows:   stores.Workflows,
		Agents:      stores.Agents,
		Tasks:       stores.Tasks,
		TaskFinder:  tasks.NewTaskService(tasks.NewTaskServiceParams{Tasks: stores.Tasks}),
		Events:      events,
		Interrupter: interrupter,
		StateDir:    t.TempDir(),
		Git:         gittest.NoRepo{},
		Tracer:      history.NewTracer(gittest.NoRepo{}),
	})

	_, err := service.TakeSnapshot(t.Context(), *writer.TaskID, personAuthor)
	require.NoError(t, err)

	return &reviewFixture{
		service:     service,
		reviews:     reviews,
		stores:      stores,
		snapshots:   snapshots,
		events:      events,
		interrupter: interrupter,
		writer:      writer,
		reviewer:    reviewer,
		otherWriter: otherWriter,
	}
}

func newAgentAuthor(agent *agentstore.Agent) ReviewAuthor {
	return ReviewAuthor{
		Kind:    reviewstore.AuthorAgent,
		AgentID: &agent.ID,
	}
}

// releaseFirstReview has the person release one comment on snapshot one, returning its thread.
func (fx *reviewFixture) releaseFirstReview(t *testing.T) reviewstore.Thread {
	t.Helper()

	written := fx.writeComment(t, personAuthor, "this needs a nil check")
	fx.releaseReview(t, personAuthor, "")

	return written.Thread
}

func (fx *reviewFixture) writeComment(t *testing.T, author ReviewAuthor, body string) *ReviewThreadWithMessages {
	t.Helper()

	written, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:        *fx.writer.TaskID,
		FilePath:    "greet.go",
		NewStart:    41,
		NewEnd:      41,
		QuotedLines: []string{"+\tgreet(nil)"},
		Body:        body,
		Author:      author,
	})
	require.NoError(t, err)

	return written
}

func (fx *reviewFixture) writeCommentOn(t *testing.T, author ReviewAuthor, snapshot string) *ReviewThreadWithMessages {
	t.Helper()

	written, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:        *fx.writer.TaskID,
		SnapshotSHA: snapshot,
		FilePath:    "greet.go",
		Body:        "this needs a nil check",
		Author:      author,
	})
	require.NoError(t, err)

	return written
}

// tryComment writes a comment on the snapshot named, or on whatever snapshot the author is reading when it names none.
func (fx *reviewFixture) tryComment(t *testing.T, author ReviewAuthor, snapshot string) error {
	t.Helper()

	_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:        *fx.writer.TaskID,
		SnapshotSHA: snapshot,
		FilePath:    "greet.go",
		Body:        "and another thing",
		Author:      author,
	})

	return err
}

func (fx *reviewFixture) releaseReview(t *testing.T, author ReviewAuthor, verdict reviewstore.Verdict) *ReleaseReviewResult {
	t.Helper()

	released, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
		Task:    *fx.writer.TaskID,
		Verdict: verdict,
		Author:  author,
	})
	require.NoError(t, err)

	return released
}

// writeAnswer has the agent under review answer in a thread.
func (fx *reviewFixture) writeAnswer(t *testing.T, threadID string) *reviewstore.Message {
	t.Helper()

	answer, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID: threadID,
		Body:     "done",
		Author:   newAgentAuthor(fx.writer),
	})
	require.NoError(t, err)

	return &answer.Message
}

func (fx *reviewFixture) settle(t *testing.T, threadID string, author ReviewAuthor) *reviewstore.Thread {
	t.Helper()

	settled, err := fx.service.SetReviewThreadResolved(t.Context(), SetReviewThreadResolvedParams{
		ID:       threadID,
		Resolved: true,
		Author:   author,
	})
	require.NoError(t, err)

	return settled
}

// advanceTree has the writer write something and the person take snapshot two of it.
func (fx *reviewFixture) advanceTree(t *testing.T) *reviewstore.Snapshot {
	t.Helper()

	fx.snapshots.SHA = secondSnapshot
	snapshot, err := fx.service.TakeSnapshot(t.Context(), *fx.writer.TaskID, personAuthor)
	require.NoError(t, err)

	return snapshot
}

// removeWorktree records the writer's worktree as removed, with its workflow in dir.
func (fx *reviewFixture) removeWorktree(t *testing.T, dir string) {
	t.Helper()

	workflow, err := fx.stores.Workflows.Get(t.Context(), fx.writer.WorkflowID)
	require.NoError(t, err)
	cfg := workflow.Config()
	cfg.Cwd = dir
	require.NoError(t, fx.stores.Workflows.Update(t.Context(), workflow.ID, cfg))
	require.NoError(t, fx.stores.Tasks.SetHead(t.Context(), *fx.writer.TaskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskstore.WorktreeGone}))
}

// listMail is every piece of mail addressed to an agent, oldest first.
func (fx *reviewFixture) listMail(t *testing.T, agentID string) []mailstore.Mail {
	t.Helper()

	listed, err := fx.stores.Mail.ListPage(t.Context(), agentID, 200, nil)
	require.NoError(t, err)

	slices.Reverse(listed)

	return listed
}
