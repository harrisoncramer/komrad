package reviews

import (
	"testing"

	agentstore "komrad/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddressedReply pins who may answer a comment with addressed and what the answer must carry.
func TestAddressedReply(t *testing.T) {
	tests := []struct {
		name        string
		author      func(fx *reviewFixture) ReviewAuthor
		homeless    bool
		snapshot    string
		wantErr     error
		wantOrdinal int32
	}{
		{
			name: "the person",
			author: func(*reviewFixture) ReviewAuthor {
				return personAuthor
			},
			snapshot: secondSnapshot,
			wantErr:  errReviewAddressedByOwner,
		},
		{
			name: "the agent under review with no worktree",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.worker)
			},
			homeless: true,
			snapshot: secondSnapshot,
			wantErr:  errReviewNoAnswerRound,
		},
		{
			name: "the agent under review with nothing changed",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.worker)
			},
			snapshot: firstSnapshot,
			wantErr:  errReviewNothingAddressed,
		},
		{
			name: "the agent under review after changing its worktree",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.worker)
			},
			snapshot:    secondSnapshot,
			wantOrdinal: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			thread := fx.releaseFirstReview(t)
			fx.snapshots.SHA = tt.snapshot
			if tt.homeless {
				require.NoError(t, fx.stores.Tasks.SetCheckoutDir(t.Context(), *fx.worker.TaskID, ""))
			}

			added, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
				ThreadID:  thread.ID,
				Body:      "added the nil check",
				Author:    tt.author(fx),
				Addressed: true,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			require.NotNil(t, added.Message.RoundID)

			round, err := fx.reviews.GetRound(t.Context(), *added.Message.RoundID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrdinal, round.Ordinal)
		})
	}
}

// TestANewWriterAnswersTheThreadsItInherits pins that a task handed on moves its threads to the new writer, whose addressed answer settles them.
func TestANewWriterAnswersTheThreadsItInherits(t *testing.T) {
	tests := []struct {
		name    string
		author  func(fx *reviewFixture, successor *agentstore.Agent) ReviewAuthor
		wantErr error
	}{
		{
			name: "the writer that handed the task on",
			author: func(fx *reviewFixture, _ *agentstore.Agent) ReviewAuthor {
				return newAgentAuthor(fx.worker)
			},
			wantErr: ErrReviewNotYours,
		},
		{
			name: "the new writer",
			author: func(_ *reviewFixture, successor *agentstore.Agent) ReviewAuthor {
				return newAgentAuthor(successor)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			thread := fx.releaseFirstReview(t)
			_, err := fx.stores.Agents.Archive(t.Context(), []string{fx.worker.ID})
			require.NoError(t, err)
			successor := fx.seedSuccessor(t)
			fx.snapshots.SHA = secondSnapshot

			_, err = fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
				ThreadID:  thread.ID,
				Body:      "added the nil check",
				Author:    tt.author(fx, successor),
				Addressed: true,
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			settled, err := fx.reviews.GetThread(t.Context(), thread.ID)
			require.NoError(t, err)
			assert.NotNil(t, settled.ResolvedAt)
			assert.Equal(t, &successor.ID, settled.ResolvedByAgentID)
		})
	}
}

// TestPlainReplyHint pins that a plain reply is pointed at addressed only once the worktree moved.
func TestPlainReplyHint(t *testing.T) {
	tests := []struct {
		name     string
		snapshot string
		wantHint bool
	}{
		{
			name:     "a worktree that has not moved",
			snapshot: firstSnapshot,
			wantHint: false,
		},
		{
			name:     "a worktree that moved since the comment",
			snapshot: secondSnapshot,
			wantHint: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			thread := fx.releaseFirstReview(t)
			fx.snapshots.SHA = tt.snapshot

			added, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
				ThreadID: thread.ID,
				Body:     "done",
				Author:   newAgentAuthor(fx.worker),
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantHint, added.Hint != "")
			assert.Nil(t, added.Message.RoundID)
		})
	}
}

// TestReplyingToAnAnswerReopensIt pins that the person's reply to an addressed comment is a
// release of its own, which reopens the comment and says so in the mail.
func TestReplyingToAnAnswerReopensIt(t *testing.T) {
	fx := newReviewFixture(t)
	thread := fx.releaseFirstReview(t)
	fx.snapshots.SHA = secondSnapshot
	_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID:  thread.ID,
		Body:      "added the nil check",
		Author:    newAgentAuthor(fx.worker),
		Addressed: true,
	})
	require.NoError(t, err)

	_, err = fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID: thread.ID,
		Body:     "the empty list still panics",
		Author:   personAuthor,
	})
	require.NoError(t, err)
	fx.releaseReview(t, personAuthor, "")

	rung := fx.listMail(t, fx.worker.ID)
	require.NotEmpty(t, rung)
	assert.Contains(t, rung[len(rung)-1].Body, "0 comment(s), 1 reply(ies) in earlier threads, 1 of them reopening")
}
