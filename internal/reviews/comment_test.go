package reviews

import (
	"errors"
	"testing"

	"komrad/config"
	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/mail"
	mailstore "komrad/internal/store/mail"
	taskstore "komrad/internal/store/task"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestACommentGoesToTheWriterAtOnce pins that the person's comment is mailed whole as it is written
// and opens no review.
func TestACommentGoesToTheWriterAtOnce(t *testing.T) {
	fx := newReviewFixture(t)

	written := fx.writeComment(t, personAuthor, "this needs a nil check")

	rung := fx.listMail(t, fx.writer.ID)
	require.Len(t, rung, 1)
	assert.Equal(t, mailstore.KindReviewComment, rung[0].Kind)
	assert.Equal(t, []string{eventprompts.ReviewCommentedByPerson}, rung[0].Events)
	assert.Contains(t, rung[0].Body, "the person commented on snapshot 1 at greet.go:41, thread "+written.Thread.ID.String())
	assert.Contains(t, rung[0].Body, "+\tgreet(nil)")
	assert.Contains(t, rung[0].Body, "this needs a nil check")
	require.Len(t, written.Messages, 1)
	assert.Equal(t, &rung[0].ID, written.Messages[0].MailID)

	assert.Nil(t, written.Thread.ReviewID)
	listed, err := fx.service.ListReviews(t.Context(), taskByID(*fx.writer.TaskID), personAuthor)
	require.NoError(t, err)
	assert.Empty(t, listed)
}

// TestEachCommentIsItsOwnMail pins that two comments reach the writer as two pieces of mail.
func TestEachCommentIsItsOwnMail(t *testing.T) {
	fx := newReviewFixture(t)

	fx.writeComment(t, personAuthor, "this needs a nil check")
	fx.writeComment(t, personAuthor, "and the caller above it")

	rung := fx.listMail(t, fx.writer.ID)
	require.Len(t, rung, 2)
	assert.Contains(t, rung[0].Body, "this needs a nil check")
	assert.Contains(t, rung[1].Body, "and the caller above it")
}

// TestAFailedSendLeavesNoComment pins that the comment and its mail are written together or not at all.
func TestAFailedSendLeavesNoComment(t *testing.T) {
	fx := newReviewFixture(t)
	fx.service.mailer = mail.NewMailService(mail.NewMailServiceParams{
		Mail:      fx.stores.Mail,
		Agents:    fx.stores.Agents,
		Workflows: fx.stores.Workflows,
		Config:    config.Mail{DeliveryDeadline: "never"},
		Tx:        fx.stores.Pool,
	})

	_, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:     taskByID(*fx.writer.TaskID),
		FilePath: "greet.go",
		Body:     "this needs a nil check",
		Author:   personAuthor,
	})
	require.Error(t, err)

	threads, err := fx.service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
		Task:   taskByID(*fx.writer.TaskID),
		Author: personAuthor,
	})
	require.NoError(t, err)
	assert.Empty(t, threads)
	assert.Empty(t, fx.listMail(t, fx.writer.ID))
}

// TestACommentReachesAFullMailbox pins that the person's comment is never refused for room.
func TestACommentReachesAFullMailbox(t *testing.T) {
	fx := newReviewFixture(t)
	fillMailbox(t, fx)

	written := fx.writeComment(t, personAuthor, "this needs a nil check")

	require.Len(t, written.Messages, 1)
	assert.NotNil(t, written.Messages[0].MailID)
}

// TestARemarkOnTheWholeChange pins a thread with no file or lines and how its mail places it.
func TestARemarkOnTheWholeChange(t *testing.T) {
	fx := newReviewFixture(t)

	written, err := fx.service.CreateReviewThread(t.Context(), CreateReviewThreadParams{
		Task:   taskByID(*fx.writer.TaskID),
		Body:   "this whole change needs a rethink",
		Author: personAuthor,
	})
	require.NoError(t, err)

	assert.Empty(t, written.Thread.FilePath)
	rung := fx.listMail(t, fx.writer.ID)
	require.Len(t, rung, 1)
	assert.Contains(t, rung[0].Body, "the person commented on the whole change as of snapshot 1, thread "+written.Thread.ID.String())
}

// TestReadMail pins that reading mail with a comment waiting takes the snapshot the comment is
// read on, that a read with no comment or no worktree takes none, and that a snapshot that cannot
// be taken leaves the mail waiting.
func TestReadMail(t *testing.T) {
	tests := []struct {
		name        string
		arrange     func(t *testing.T, fx *reviewFixture)
		wantReadOn  int32
		wantClaimed int
		wantErr     bool
	}{
		{
			name: "a comment is read on the worktree as it stands",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				fx.snapshots.SHA = secondSnapshot
			},
			wantReadOn:  2,
			wantClaimed: 1,
		},
		{
			name: "mail with no comment in it takes no snapshot",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.sendPlainMail(t)
				fx.snapshots.SHA = secondSnapshot
			},
			wantClaimed: 1,
		},
		{
			name: "a comment on a task with no directory is read on no snapshot",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				require.NoError(t, fx.stores.Tasks.SetCheckoutDir(t.Context(), *fx.writer.TaskID, ""))
			},
			wantClaimed: 1,
		},
		{
			name: "a comment on a task whose worktree was removed is read on no snapshot",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				require.NoError(t, fx.stores.Tasks.SetHead(t.Context(), *fx.writer.TaskID, taskstore.SetHeadParams{WorktreeRemovalReason: taskstore.WorktreeGone}))
			},
			wantClaimed: 1,
		},
		{
			name: "a comment read while a rebase is part way through is read on the newest snapshot",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				fx.snapshots.Err = fail.Precondition("a rebase is part way through")
			},
			wantReadOn:  1,
			wantClaimed: 1,
		},
		{
			name: "a snapshot that cannot be taken leaves the comment waiting",
			arrange: func(t *testing.T, fx *reviewFixture) {
				fx.writeComment(t, personAuthor, "this needs a nil check")
				fx.snapshots.Err = errors.New("git failed")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			tt.arrange(t, fx)

			claimed, err := fx.service.ReadMail(t.Context(), fx.writer.ID)
			if tt.wantErr {
				require.Error(t, err)
				waiting, err := fx.service.mailer.ListWaitingMail(t.Context(), fx.writer.ID)
				require.NoError(t, err)
				assert.Len(t, waiting, 1)

				return
			}
			require.NoError(t, err)
			assert.Len(t, claimed, tt.wantClaimed)

			threads, err := fx.service.ListReviewThreads(t.Context(), ListReviewThreadsParams{
				Task:   taskByID(*fx.writer.TaskID),
				Author: personAuthor,
			})
			require.NoError(t, err)
			if tt.wantReadOn == 0 {
				for _, thread := range threads {
					assert.NotNil(t, thread.Messages[0].ReadAt)
					assert.Nil(t, thread.Messages[0].ReadSnapshotID)
				}

				return
			}
			require.Len(t, threads, 1)
			require.NotNil(t, threads[0].Messages[0].ReadSnapshotID)
			snapshot, err := fx.reviews.GetSnapshot(t.Context(), *threads[0].Messages[0].ReadSnapshotID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantReadOn, snapshot.Ordinal)
		})
	}
}

// TestAddressedIsMeasuredFromTheReadSnapshot pins that an answer marked addressed is compared
// against the worktree as it stood when the writer read the comment, not when it was written.
func TestAddressedIsMeasuredFromTheReadSnapshot(t *testing.T) {
	fx := newReviewFixture(t)
	thread := fx.releaseFirstComment(t)
	fx.snapshots.SHA = secondSnapshot
	fx.readMail(t)

	_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID:  thread.ID,
		Body:      "added the nil check",
		Author:    newAgentAuthor(fx.writer),
		Addressed: true,
	})
	require.ErrorIs(t, err, errReviewNothingAddressed, "nothing changed since the comment was read")

	fx.snapshots.SHA = thirdSnapshot
	added, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
		ThreadID:  thread.ID,
		Body:      "added the nil check",
		Author:    newAgentAuthor(fx.writer),
		Addressed: true,
	})
	require.NoError(t, err)
	require.NotNil(t, added.Message.SnapshotID)
	snapshot, err := fx.reviews.GetSnapshot(t.Context(), *added.Message.SnapshotID)
	require.NoError(t, err)
	assert.Equal(t, int32(3), snapshot.Ordinal)
}

// readMail has the writer read its waiting mail as read_mail does.
func (fx *reviewFixture) readMail(t *testing.T) {
	t.Helper()

	_, err := fx.service.ReadMail(t.Context(), fx.writer.ID)
	require.NoError(t, err)
}

// sendPlainMail sends the writer one piece of mail from komrad that carries no comment.
func (fx *reviewFixture) sendPlainMail(t *testing.T) {
	t.Helper()

	_, err := fx.service.mailer.SendFromKomrad(t.Context(), mail.SendFromKomradParams{
		ToAgentID: fx.writer.ID,
		Mail:      eventprompts.ThreadAnswered{WriterName: "waiting"},
		SubjectID: uuid.New(),
	})
	require.NoError(t, err)
}
