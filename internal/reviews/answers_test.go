package reviews

import (
	"testing"

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
				return newAgentAuthor(fx.writer)
			},
			homeless: true,
			snapshot: secondSnapshot,
			wantErr:  errReviewNoAnswerSnapshot,
		},
		{
			name: "the agent under review with nothing changed",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
			},
			snapshot: firstSnapshot,
			wantErr:  errReviewNothingAddressed,
		},
		{
			name: "the agent under review after changing its worktree",
			author: func(fx *reviewFixture) ReviewAuthor {
				return newAgentAuthor(fx.writer)
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
				require.NoError(t, fx.stores.Tasks.SetCheckoutDir(t.Context(), *fx.writer.TaskID, ""))
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
			require.NotNil(t, added.Message.SnapshotID)

			snapshot, err := fx.reviews.GetSnapshot(t.Context(), *added.Message.SnapshotID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantOrdinal, snapshot.Ordinal)
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
				Author:   newAgentAuthor(fx.writer),
			})
			require.NoError(t, err)

			assert.Equal(t, tt.wantHint, added.Hint != "")
			assert.Nil(t, added.Message.SnapshotID)
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
		Author:    newAgentAuthor(fx.writer),
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

	rung := fx.listMail(t, fx.writer.ID)
	require.NotEmpty(t, rung)
	assert.Contains(t, rung[len(rung)-1].Body, "0 comment(s), 1 reply(ies) in earlier threads, 1 of them reopening")
}
