package reviews

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/agents"
	"komrad/internal/gittest"
	"komrad/internal/mail"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	"komrad/internal/storetest"
	"komrad/lib/xslice"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	firstSnapshot  = "1111111111111111111111111111111111111111"
	secondSnapshot = "2222222222222222222222222222222222222222"
	baseSnapshot   = "0000000000000000000000000000000000000000"
)

var personAuthor = ReviewAuthor{Kind: reviewstore.AuthorHuman}

// reviewFixture is a worker with round one taken, its reviewer, and an agent with no business in either.
type reviewFixture struct {
	service   *ReviewService
	reviews   *reviewstore.ReviewStore
	stores    *storetest.Stores
	snapshots *gittest.Snapshots
	resumer   *recordingResumer
	worker    *agentstore.Agent
	reviewer  *agentstore.Agent
	stranger  *agentstore.Agent
}

type recordingResumer struct {
	mu      sync.Mutex
	resumed []string
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
					Author:   newAgentAuthor(fx.stranger),
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "an agent opening a thread on its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					AgentID:  fx.worker.ID,
					FilePath: "greet.go",
					Body:     "looks fine to me",
					Author:   newAgentAuthor(fx.worker),
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
					Author:   newAgentAuthor(fx.worker),
				})

				return err
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "an agent releasing a review of its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					AgentID: fx.worker.ID,
					Verdict: reviewstore.VerdictApproved,
					Author:  newAgentAuthor(fx.worker),
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
					Author: newAgentAuthor(fx.worker),
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
					Author: newAgentAuthor(fx.worker),
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
					Author: newAgentAuthor(fx.stranger),
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
			name: "a child reviewing an agent other than its parent",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					AgentID:  fx.stranger.ID,
					FilePath: "greet.go",
					Body:     "this needs a nil check",
					Author:   newAgentAuthor(fx.reviewer),
				})

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent taking a round",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.OpenRound(t.Context(), fx.worker.ID, newAgentAuthor(fx.stranger))

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent listing the reviews",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListReviews(t.Context(), fx.worker.ID, newAgentAuthor(fx.stranger))

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an unrelated agent reading a released review",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.GetReview(t.Context(), fx.releaseFirstReview(t).ReviewID, newAgentAuthor(fx.stranger))

				return err
			},
			wantErr: ErrReviewNotEntitled,
		},
		{
			name: "an agent marking a file viewed",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.SetReviewFileViewed(t.Context(), SetReviewFileViewedParams{
					AgentID:     fx.worker.ID,
					FilePath:    "greet.go",
					Fingerprint: "abc123",
					Viewed:      true,
					Author:      newAgentAuthor(fx.worker),
				})

				return err
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "an agent listing the files viewed",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListReviewViewedFiles(t.Context(), ListReviewViewedFilesParams{
					AgentID: fx.worker.ID,
					Author:  newAgentAuthor(fx.worker),
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
			name: "the person taking a round of a child",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.OpenRound(t.Context(), fx.reviewer.ID, personAuthor)

				return err
			},
			wantErr: agents.ErrReviewNeedsOwnWork,
		},
		{
			name: "a child taking a round of itself",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.OpenRound(t.Context(), fx.reviewer.ID, newAgentAuthor(fx.reviewer))

				return err
			},
			wantErr: agents.ErrReviewNeedsOwnWork,
		},
		{
			name: "taking a round of an agent with no directory",
			call: func(t *testing.T, fx *reviewFixture) error {
				homeless := fx.stores.SeedAgent(t, agentstore.Agent{Name: "homeless"})
				_, err := fx.service.OpenRound(t.Context(), homeless.ID, personAuthor)

				return err
			},
			wantErr: ErrReviewNoRound,
		},
		{
			name: "releasing to an archived agent",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.stores.Agents.Archive(t.Context(), []string{fx.worker.ID})
				require.NoError(t, err)

				_, err = fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					AgentID: fx.worker.ID,
					Author:  personAuthor,
				})

				return err
			},
			wantErr: agents.ErrAgentArchived,
		},
		{
			name: "a reviewer releasing notes with nothing in them",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					AgentID: fx.worker.ID,
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
					AgentID: fx.worker.ID,
					Author:  newAgentAuthor(fx.reviewer),
				})

				return err
			},
			wantErr: ErrReviewVerdictMissing,
		},
		{
			name: "the person releasing with a verdict",
			call: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				_, err := fx.service.ReleaseReview(t.Context(), ReleaseReviewParams{
					AgentID: fx.worker.ID,
					Verdict: reviewstore.VerdictApproved,
					Author:  personAuthor,
				})

				return err
			},
			wantErr: ErrReviewVerdictFromPerson,
		},
		{
			name: "a comment read from a snapshot no round was taken of",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
					AgentID:     fx.worker.ID,
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
					Author: newAgentAuthor(fx.worker),
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
					AgentID: fx.worker.ID,
					Author:  personAuthor,
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
			name: "the agent under review listing its own rounds",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.ListRounds(t.Context(), fx.worker.ID, newAgentAuthor(fx.worker))

				return err
			},
		},
		{
			name: "the agent under review taking a round of its own work",
			call: func(t *testing.T, fx *reviewFixture) error {
				_, err := fx.service.OpenRound(t.Context(), fx.worker.ID, newAgentAuthor(fx.worker))

				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t, time.Hour)

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
				return newAgentAuthor(fx.worker)
			},
			wantThreads: [][]string{{"this needs a nil check", "done"}},
		},
		{
			name: "an agent reading somebody else's review",
			reader: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.stranger)
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
			fx := newReviewFixture(t, time.Hour)
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
				AgentID: fx.worker.ID,
				Author:  reader,
			})
			require.NoError(t, err)

			bodies := xslice.Map(threads, func(thread ReviewThreadWithMessages) []string {
				return readBodies(thread.Messages)
			})
			assert.Equal(t, tt.wantThreads, bodies)

			if reader.IsHuman() {
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

// TestCreateReviewThreadPicksTheRound pins which round a comment is filed under.
func TestCreateReviewThreadPicksTheRound(t *testing.T) {
	tests := []struct {
		name        string
		write       func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages
		wantOrdinal int32
	}{
		{
			name: "naming the snapshot of a round",
			write: func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages {
				fx.advanceTree(t)

				return fx.writeCommentOn(t, personAuthor, firstSnapshot)
			},
			wantOrdinal: 1,
		},
		{
			name: "naming the snapshot it holds comments on after somebody took a newer round",
			write: func(t *testing.T, fx *reviewFixture) *ReviewThreadWithMessages {
				fx.writeCommentOn(t, personAuthor, firstSnapshot)
				fx.advanceTree(t)

				return fx.writeCommentOn(t, personAuthor, firstSnapshot)
			},
			wantOrdinal: 1,
		},
		{
			name: "naming no snapshot while holding comments on an older round",
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
			fx := newReviewFixture(t, time.Hour)

			written := tt.write(t, fx)

			round, err := fx.reviews.GetRound(t.Context(), written.Thread.RoundID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrdinal, round.Ordinal)
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
			name: "the reviewer on a round it has reviewed",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
				fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)

				return fx.tryComment(t, newAgentAuthor(fx.reviewer), "")
			},
			wantErr: ErrReviewAlreadyGiven,
		},
		{
			name: "the reviewer going back to a round it reviewed",
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
			name: "a comment on a newer round while holding comments on an older one",
			write: func(t *testing.T, fx *reviewFixture) error {
				fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
				fx.advanceTree(t)

				return fx.tryComment(t, newAgentAuthor(fx.reviewer), secondSnapshot)
			},
			wantErr:     ErrReviewInFlight,
			wantErrText: "round 1",
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
			fx := newReviewFixture(t, time.Hour)

			err := tt.write(t, fx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), tt.wantErrText)

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
			fx := newReviewFixture(t, time.Hour)
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
	fx := newReviewFixture(t, time.Hour)

	_, err := fx.service.SetReviewFileViewed(t.Context(), SetReviewFileViewedParams{
		AgentID:     fx.worker.ID,
		FilePath:    "greet.go",
		Fingerprint: "abc123",
		Viewed:      true,
		Author:      personAuthor,
	})
	require.NoError(t, err)

	viewed, err := fx.service.ListReviewViewedFiles(t.Context(), ListReviewViewedFilesParams{
		AgentID: fx.worker.ID,
		Author:  personAuthor,
	})
	require.NoError(t, err)
	require.Len(t, viewed, 1)
	assert.Equal(t, "greet.go", viewed[0].FilePath)

	_, err = fx.service.SetReviewFileViewed(t.Context(), SetReviewFileViewedParams{
		AgentID:  fx.worker.ID,
		FilePath: "greet.go",
		Author:   personAuthor,
	})
	require.NoError(t, err)

	viewed, err = fx.service.ListReviewViewedFiles(t.Context(), ListReviewViewedFilesParams{
		AgentID: fx.worker.ID,
		Author:  personAuthor,
	})
	require.NoError(t, err)
	assert.Empty(t, viewed)
}

// TestAnsweringAReviewRingsTheReviewer pins that the agent's answer, and only its answer, tells and wakes its reviewer.
func TestAnsweringAReviewRingsTheReviewer(t *testing.T) {
	tests := []struct {
		name     string
		author   func(fx *reviewFixture) ReviewAuthor
		wantRung bool
	}{
		{
			name: "the person writing in the thread",
			author: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
		},
		{
			name: "the agent under review answering",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.worker)
			},
			wantRung: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t, time.Millisecond)
			written := fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
			fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)

			_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
				ThreadID: written.Thread.ID,
				Body:     "fixed, the lookahead now stops at the newline",
				Author:   tt.author(fx),
			})
			require.NoError(t, err)

			if !tt.wantRung {
				fx.service.Close()
				assert.Empty(t, fx.listMail(t, fx.reviewer.ID), "nothing was waiting to be sent")
				assert.Empty(t, fx.resumer.listResumed())

				return
			}

			require.Eventually(t, func() bool {
				return len(fx.resumer.listResumed()) == 1
			}, time.Second, 5*time.Millisecond, "mail waits for a pane rather than making one")
			assert.Equal(t, []string{fx.reviewer.ID}, fx.resumer.listResumed())

			rung := fx.listMail(t, fx.reviewer.ID)
			require.Len(t, rung, 1)
			assert.Equal(t, mailstore.KindReviewActivity, rung[0].Kind)
			assert.Equal(t, &fx.worker.ID, rung[0].AboutAgentID)
			assert.Contains(t, rung[0].Body, "list_review_threads", "the doorbell says where to look rather than what was said")
		})
	}
}

// TestAnsweringAReviewSeveralTimesRingsOnce pins that answers inside the debounce wake the reviewer once.
func TestAnsweringAReviewSeveralTimesRingsOnce(t *testing.T) {
	fx := newReviewFixture(t, 200*time.Millisecond)
	written := fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
	fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)

	for range 3 {
		fx.writeAnswer(t, written.Thread.ID)
	}

	require.Eventually(t, func() bool {
		return len(fx.resumer.listResumed()) == 1
	}, time.Second, 5*time.Millisecond)
	assert.Never(t, func() bool {
		return len(fx.resumer.listResumed()) > 1
	}, 300*time.Millisecond, 10*time.Millisecond, "three answers inside the window are one wake")

	claimed, err := fx.stores.Mail.Claim(t.Context(), fx.reviewer.ID, 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	fx.writeAnswer(t, written.Thread.ID)

	require.Eventually(t, func() bool {
		return len(fx.listMail(t, fx.reviewer.ID)) == 2
	}, time.Second, 5*time.Millisecond, "an answer once the window has passed rings again")
}

// TestCloseSendsAPendingReviewerNotice pins that a shutdown mid-debounce tells the reviewer without waking it.
func TestCloseSendsAPendingReviewerNotice(t *testing.T) {
	fx := newReviewFixture(t, time.Hour)
	written := fx.writeComment(t, newAgentAuthor(fx.reviewer), "this needs a nil check")
	fx.releaseReview(t, newAgentAuthor(fx.reviewer), reviewstore.VerdictChangesRequested)
	fx.writeAnswer(t, written.Thread.ID)

	fx.service.Close()

	assert.Len(t, fx.listMail(t, fx.reviewer.ID), 1)
	assert.Empty(t, fx.resumer.listResumed(), "a resumed pane picks the mail up on its own")
}

func newReviewFixture(t *testing.T, debounce time.Duration) *reviewFixture {
	t.Helper()

	stores := storetest.Open(t)
	reviews := reviewstore.NewReviewStore(stores.Pool.GetDB())
	snapshots := &gittest.Snapshots{
		SHA:     firstSnapshot,
		BaseSHA: baseSnapshot,
		Files:   map[string][]string{},
	}
	resumer := &recordingResumer{}

	worker := stores.SeedAgent(t, agentstore.Agent{
		Name: "worker",
		Cwd:  "/tree",
	})
	reviewer := stores.SeedAgent(t, agentstore.Agent{
		Name:          "reviewer",
		Cwd:           "/tree",
		WorkflowID:    worker.WorkflowID,
		Role:          agentstore.RoleReviewer,
		ParentAgentID: &worker.ID,
	})
	stranger := stores.SeedAgent(t, agentstore.Agent{
		Name: "stranger",
		Cwd:  "/elsewhere",
	})

	service := NewReviewService(NewReviewServiceParams{
		Threads:   reviews,
		Messages:  reviews,
		Viewed:    reviews,
		Rounds:    reviews,
		Reviews:   reviews,
		Snapshots: snapshots,
		Workflows: stores.Workflows,
		Agents:    stores.Agents,
		Mailer: mail.NewMailService(mail.NewMailServiceParams{
			MailReadWriter: stores.Mail,
			Agents:         stores.Agents,
			Config:         config.Mail{},
		}),
		Resumer:        resumer,
		StateDir:       t.TempDir(),
		NoticeDebounce: debounce,
	})

	_, err := service.OpenRound(t.Context(), worker.ID, personAuthor)
	require.NoError(t, err)

	return &reviewFixture{
		service:   service,
		reviews:   reviews,
		stores:    stores,
		snapshots: snapshots,
		resumer:   resumer,
		worker:    worker,
		reviewer:  reviewer,
		stranger:  stranger,
	}
}

func newAgentAuthor(agent *agentstore.Agent) ReviewAuthor {
	return ReviewAuthor{
		Kind:    reviewstore.AuthorAgent,
		AgentID: &agent.ID,
	}
}

// releaseFirstReview has the person release one comment on round one, returning its thread.
func (fx *reviewFixture) releaseFirstReview(t *testing.T) reviewstore.Thread {
	t.Helper()

	written := fx.writeComment(t, personAuthor, "this needs a nil check")
	fx.releaseReview(t, personAuthor, "")

	return written.Thread
}

func (fx *reviewFixture) writeComment(t *testing.T, author ReviewAuthor, body string) *ReviewThreadWithMessages {
	t.Helper()

	written, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:     fx.worker.ID,
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
		AgentID:     fx.worker.ID,
		SnapshotSHA: snapshot,
		FilePath:    "greet.go",
		Body:        "this needs a nil check",
		Author:      author,
	})
	require.NoError(t, err)

	return written
}

// tryComment writes a comment on the snapshot named, or on whatever round the author is reading when it names none.
func (fx *reviewFixture) tryComment(t *testing.T, author ReviewAuthor, snapshot string) error {
	t.Helper()

	_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		AgentID:     fx.worker.ID,
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
		AgentID: fx.worker.ID,
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
		Author:   newAgentAuthor(fx.worker),
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

// advanceTree has the worker write something and the person take round two of it.
func (fx *reviewFixture) advanceTree(t *testing.T) *reviewstore.Round {
	t.Helper()

	fx.snapshots.SHA = secondSnapshot
	round, err := fx.service.OpenRound(t.Context(), fx.worker.ID, personAuthor)
	require.NoError(t, err)

	return round
}

// listMail is every piece of mail addressed to an agent, oldest first.
func (fx *reviewFixture) listMail(t *testing.T, agentID string) []mailstore.Mail {
	t.Helper()

	listed, err := fx.stores.Mail.ListPage(t.Context(), agentID, 200, nil)
	require.NoError(t, err)

	slices.Reverse(listed)

	return listed
}

func (r *recordingResumer) ResumeAgent(_ context.Context, id string) (*agentstore.Agent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.resumed = append(r.resumed, id)

	return &agentstore.Agent{ID: id}, nil
}

func (r *recordingResumer) listResumed() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.resumed)
}
