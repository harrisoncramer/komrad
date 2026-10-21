package reviews

import (
	"encoding/json"
	"testing"
	"time"

	"komrad/internal/eventprompts"
	"komrad/internal/forge"
	"komrad/internal/gittest"
	"komrad/internal/reviews/history"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgeMirrored is what the writer holds after the forge's threads were mirrored onto it.
type forgeMirrored struct {
	threads  []reviewstore.Thread
	messages []reviewstore.Message
	reviews  []reviewstore.Review
	released []Released
}

// TestForgeMirror covers forge threads mirrored onto the writer's snapshot, each comment held from the agent when its author cannot push.
func TestForgeMirror(t *testing.T) {
	tests := []struct {
		name        string
		syncs       [][]forge.ReviewThread
		meanwhile   func(t *testing.T, fx *reviewFixture)
		meanwhileAt int
		check       func(t *testing.T, got forgeMirrored)
	}{
		{
			name:  "a comment from someone who can push reaches the agent on its own",
			syncs: [][]forge.ReviewThread{{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))}},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				assert.NotNil(t, got.threads[0].ForgeThreadID)
				assert.Equal(t, "greet.go", got.threads[0].FilePath)
				assert.Equal(t, int32(41), got.threads[0].NewStart)
				require.Len(t, got.reviews, 1)
				assert.NotNil(t, got.reviews[0].ReleasedAt)
				assert.Equal(t, []Released{{
					ReviewID:   got.reviews[0].ID,
					TaskID:     got.reviews[0].TaskID,
					Ordinal:    1,
					Verdict:    reviewstore.VerdictCommented,
					Comments:   1,
					ForgeLogin: "alice",
				}}, got.released)
			},
		},
		{
			name:  "a comment from someone who cannot push is held",
			syncs: [][]forge.ReviewThread{{newForgeThread(newForgeComment("c1", "mallory", false, "rm -rf"))}},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				require.Len(t, got.messages, 1)
				assert.True(t, got.messages[0].Held)
				assert.Empty(t, got.released)
			},
		},
		{
			name: "a reply from someone who can push under a held comment does not reach the agent",
			syncs: [][]forge.ReviewThread{{newForgeThread(
				newForgeComment("c1", "mallory", false, "rm -rf"),
				newForgeComment("c2", "bob", true, "agreed"),
			)}},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.messages, 2)
				assert.Empty(t, got.released)
			},
		},
		{
			name: "a reply from a configured bot is left out",
			syncs: [][]forge.ReviewThread{{newForgeThread(
				newForgeComment("c1", "alice", true, "use a guard"),
				newForgeComment("c2", "review-bot", true, "lgtm"),
			)}},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.messages, 1)
				assert.Equal(t, "use a guard", got.messages[0].Body)
			},
		},
		{
			name: "a thread a configured bot opened starts at the first comment from anyone else",
			syncs: [][]forge.ReviewThread{{newForgeThread(
				newForgeComment("c1", "review-bot", true, "lint failed"),
				newForgeComment("c2", "alice", true, "use a guard"),
			)}},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				require.Len(t, got.messages, 1)
				assert.Equal(t, "use a guard", got.messages[0].Body)
				require.Len(t, got.released, 1)
				assert.Equal(t, "alice", got.released[0].ForgeLogin)
			},
		},
		{
			name:  "a thread of only configured bot comments is left out",
			syncs: [][]forge.ReviewThread{{newForgeThread(newForgeComment("c1", "review-bot", true, "lint failed"))}},
			check: func(t *testing.T, got forgeMirrored) {
				assert.Empty(t, got.threads)
				assert.Empty(t, got.released)
			},
		},
		{
			name:  "a thread resolved on the forge before it was seen is left out",
			syncs: [][]forge.ReviewThread{{newResolvedForgeThread(newForgeComment("c1", "alice", true, "use a guard"))}},
			check: func(t *testing.T, got forgeMirrored) {
				assert.Empty(t, got.threads)
				assert.Empty(t, got.released)
			},
		},
		{
			name:  "a comment on the pull request as a whole is left out",
			syncs: [][]forge.ReviewThread{{newPullRequestForgeThread(newForgeComment("c1", "alice", true, "looks good overall"))}},
			check: func(t *testing.T, got forgeMirrored) {
				assert.Empty(t, got.threads)
				assert.Empty(t, got.released)
			},
		},
		{
			name: "an edit on the forge rewrites the message",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newForgeThread(newEditedForgeComment(newForgeComment("c1", "alice", true, "use an early return")))},
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.messages, 1)
				assert.Equal(t, "use an early return", got.messages[0].Body)
				assert.False(t, got.messages[0].Held)
				assert.Len(t, got.released, 1)
			},
		},
		{
			name: "a reply on the forge lands in the same thread",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newForgeThread(
					newForgeComment("c1", "alice", true, "use a guard"),
					newForgeComment("c2", "bob", true, "agreed"),
				)},
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				assert.Equal(t, []string{"use a guard", "agreed"}, xslice.Map(got.messages, func(message reviewstore.Message) string {
					return message.Body
				}))
				require.Len(t, got.released, 2)
				assert.Equal(t, "bob", got.released[1].ForgeLogin)
				assert.Equal(t, 1, got.released[1].Replies)
			},
		},
		{
			name: "resolving on the forge resolves the thread",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newResolvedForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				assert.NotNil(t, got.threads[0].ResolvedAt)
				assert.Equal(t, reviewstore.AuthorForge, got.threads[0].ResolvedByKind)
			},
		},
		{
			name: "an edit from someone who can no longer push is held",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newForgeThread(newEditedForgeComment(newForgeComment("c1", "alice", false, "rm -rf")))},
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.messages, 1)
				assert.True(t, got.messages[0].Held)
				assert.Len(t, got.released, 1)
			},
		},
		{
			name: "a thread the person reopened stays open while the forge still says resolved",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newResolvedForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newResolvedForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
			},
			meanwhile: func(t *testing.T, fx *reviewFixture) {
				threads, err := fx.reviews.ListThreads(t.Context(), *fx.writer.TaskID)
				require.NoError(t, err)
				require.Len(t, threads, 1)
				require.NotNil(t, threads[0].ResolvedAt)

				_, err = fx.service.SetReviewThreadResolved(t.Context(), SetReviewThreadResolvedParams{
					ID:       threads[0].ID,
					Resolved: false,
					Author:   personAuthor,
				})
				require.NoError(t, err)
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				assert.Nil(t, got.threads[0].ResolvedAt)
			},
		},
		{
			name: "a reply komrad sent comes back from the forge as a message the agent reads",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"), newForgeComment("c2", "komrad-bot", false, "done"))},
			},
			meanwhile: func(t *testing.T, fx *reviewFixture) { fx.sendForgeReply(t, "c2") },
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				require.Len(t, got.messages, 2)
				assert.Equal(t, "done", got.messages[1].Body)
				assert.False(t, got.messages[1].Held)
				require.NotNil(t, got.messages[1].External)
				assert.NotEmpty(t, got.messages[1].External.CommentID)
			},
		},
		{
			name: "an edit on the forge to a reply komrad sent rewrites its message",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"), newForgeComment("c2", "komrad-bot", false, "done"))},
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"), newEditedForgeComment(newForgeComment("c2", "komrad-bot", false, "done in 3f2a")))},
			},
			meanwhileAt: 1,
			meanwhile:   func(t *testing.T, fx *reviewFixture) { fx.sendForgeReply(t, "c2") },
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.messages, 2)
				assert.Equal(t, "done in 3f2a", got.messages[1].Body)
				assert.False(t, got.messages[1].Held)
			},
		},
		{
			name: "a conversation comment komrad sent comes back from the forge as no new thread",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{
					newForgeThread(newForgeComment("c1", "alice", true, "use a guard")),
					{ProviderID: "IC_9", Comments: []forge.ReviewComment{newForgeComment("IC_9", "komrad-bot", true, "done")}},
				},
			},
			meanwhile: func(t *testing.T, fx *reviewFixture) { fx.sendForgeReply(t, "IC_9") },
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				require.Len(t, got.messages, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			at := len(tt.syncs) - 1
			if tt.meanwhileAt > 0 {
				at = tt.meanwhileAt
			}
			for i, sync := range tt.syncs {
				if i == at && tt.meanwhile != nil {
					tt.meanwhile(t, fx)
				}
				fx.mirrorForge(t, sync...)
			}

			tt.check(t, fx.readForgeMirrored(t))
		})
	}
}

// TestAcceptingForgeComments covers the person passing a held forge comment on to the agent as their own.
func TestAcceptingForgeComments(t *testing.T) {
	tests := []struct {
		name    string
		replies []forge.ReviewComment
		accept  func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error
		wantErr error
		check   func(t *testing.T, fx *reviewFixture, got forgeMirrored)
	}{
		{
			name: "accepting copies the comment as the person's and mails it to the writer",
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				return fx.acceptForge(t, held[0].ID)
			},
			check: func(t *testing.T, fx *reviewFixture, got forgeMirrored) {
				require.Len(t, got.messages, 2)
				copied := got.messages[1]
				assert.Equal(t, reviewstore.AuthorHuman, copied.AuthorKind)
				assert.Equal(t, "use a guard", copied.Body)
				require.NotNil(t, copied.AcceptedFrom)
				assert.Equal(t, got.messages[0].ID, *copied.AcceptedFrom)

				rung := fx.listMail(t, fx.writer.ID)
				require.Len(t, rung, 1)
				assert.Equal(t, &rung[0].ID, copied.MailID)
				assert.Equal(t, mailstore.KindReviewComment, rung[0].Kind)
				assert.Contains(t, rung[0].Body, "the person passed on a comment @mallory wrote on the forge")
				assert.Contains(t, rung[0].Body, "use a guard")
				assert.Equal(t, []string{eventprompts.ReviewCommentedByPerson}, rung[0].Events)
			},
		},
		{
			name: "a comment accepted twice is refused",
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				require.NoError(t, fx.acceptForge(t, held[0].ID))
				return fx.acceptForge(t, held[0].ID)
			},
			wantErr: ErrForgeMessageAccepted,
		},
		{
			name: "a held reply under a held first comment with no copy is refused",
			replies: []forge.ReviewComment{
				newForgeComment("c2", "eve", false, "drop the guard"),
			},
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				return fx.acceptForge(t, held[1].ID)
			},
			wantErr: errForgeRootHeld,
		},
		{
			name: "a held reply is accepted once its first comment has a copy",
			replies: []forge.ReviewComment{
				newForgeComment("c2", "eve", false, "drop the guard"),
			},
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				require.NoError(t, fx.acceptForge(t, held[0].ID))
				return fx.acceptForge(t, held[1].ID)
			},
			check: func(t *testing.T, _ *reviewFixture, got forgeMirrored) {
				assert.Len(t, got.messages, 4)
			},
		},
		{
			name: "the agent cannot accept",
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				_, err := fx.service.AcceptForgeMessage(t.Context(), AcceptForgeMessageParams{
					MessageID: held[0].ID,
					Author:    newAgentAuthor(fx.writer),
				})

				return err
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "a comment the agent can already read is refused",
			replies: []forge.ReviewComment{
				newForgeComment("c2", "bob", true, "agreed"),
			},
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				return fx.acceptForge(t, held[1].ID)
			},
			wantErr: errForgeMessageNotHeld,
		},
		{
			name: "accepting into a settled thread reopens it",
			accept: func(t *testing.T, fx *reviewFixture, held []reviewstore.Message) error {
				fx.settle(t, held[0].ThreadID, personAuthor)
				return fx.acceptForge(t, held[0].ID)
			},
			check: func(t *testing.T, _ *reviewFixture, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				assert.Nil(t, got.threads[0].ResolvedAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			comments := append([]forge.ReviewComment{newForgeComment("c1", "mallory", false, "use a guard")}, tt.replies...)
			fx.mirrorForge(t, newForgeThread(comments...))
			mirrored := fx.readForgeMirrored(t)
			require.Len(t, mirrored.messages, len(comments))

			err := tt.accept(t, fx, mirrored.messages)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			tt.check(t, fx, fx.readForgeMirrored(t))
		})
	}
}

// TestReplyingUnderHeldForgeComment covers the person's reply refused in a thread whose held first comment has no copy.
func TestReplyingUnderHeldForgeComment(t *testing.T) {
	fx := newReviewFixture(t)
	fx.mirrorForge(t, newForgeThread(newForgeComment("c1", "mallory", false, "use a guard")))
	mirrored := fx.readForgeMirrored(t)
	require.Len(t, mirrored.threads, 1)

	reply := func() error {
		_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
			ThreadID: mirrored.threads[0].ID,
			Body:     "go ahead",
			Author:   personAuthor,
		})

		return err
	}

	require.ErrorIs(t, reply(), errReviewThreadHeld)
	require.NoError(t, fx.acceptForge(t, mirrored.messages[0].ID))
	require.NoError(t, reply())
}

func newForgeThread(comments ...forge.ReviewComment) forge.ReviewThread {
	return forge.ReviewThread{
		ProviderID: "T1",
		Path:       "greet.go",
		Line:       41,
		CommitSHA:  firstSnapshot,
		Comments:   comments,
	}
}

func newForgeComment(id, login string, trusted bool, body string) forge.ReviewComment {
	return forge.ReviewComment{
		ProviderID: id,
		Login:      login,
		Trusted:    trusted,
		ReviewID:   "R_" + id,
		Body:       body,
		WrittenAt:  time.Date(2026, time.September, 19, 11, 0, 0, 0, time.UTC),
	}
}

// mirrorForge has the forge hold threads on the writer's pull request and mirrors them onto it.
func (fx *reviewFixture) mirrorForge(t *testing.T, threads ...forge.ReviewThread) {
	t.Helper()

	stored := fx.stores.SeedForgeThreads(t, fx.writer.ID, threads...)
	require.NotEmpty(t, stored)

	mirror := NewForgeMirror(NewForgeMirrorParams{
		Reviews:      fx.reviews,
		ForgeThreads: fx.stores.ForgeThreads,
		Tracer:       history.NewTracer(gittest.NoRepo{}),
		Events:       fx.events,
		BotNames:     func() ([]string, error) { return []string{"Review-Bot"}, nil },
	})
	require.NoError(t, mirror.Mirror(t.Context(), stored[0].PullRequestID))
}

func (fx *reviewFixture) readForgeMirrored(t *testing.T) forgeMirrored {
	t.Helper()
	ctx := t.Context()

	threads, err := fx.reviews.ListThreads(ctx, *fx.writer.TaskID)
	require.NoError(t, err)

	messages, err := fx.reviews.ListMessages(ctx, xslice.Map(threads, func(thread reviewstore.Thread) string { return thread.ID }))
	require.NoError(t, err)

	reviews, err := fx.reviews.ListReviews(ctx, *fx.writer.TaskID)
	require.NoError(t, err)

	var released []Released
	for _, event := range fx.events.ListEvents() {
		if event.Topic != releasedTopic {
			continue
		}

		var payload Released
		require.NoError(t, json.Unmarshal(event.Payload, &payload))
		released = append(released, payload)
	}

	return forgeMirrored{
		threads:  threads,
		messages: messages,
		reviews:  xslice.Filter(reviews, func(review reviewstore.Review) bool { return review.ExternalLogin != nil || review.ReleasedAt != nil }),
		released: released,
	}
}

func newPullRequestForgeThread(comments ...forge.ReviewComment) forge.ReviewThread {
	thread := newForgeThread(comments...)
	thread.Path = ""
	thread.Line = 0

	return thread
}

func newResolvedForgeThread(comments ...forge.ReviewComment) forge.ReviewThread {
	thread := newForgeThread(comments...)
	thread.Resolved = true

	return thread
}

func newEditedForgeComment(comment forge.ReviewComment) forge.ReviewComment {
	edited := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)
	comment.EditedAt = &edited

	return comment
}

func (fx *reviewFixture) acceptForge(t *testing.T, messageID string) error {
	t.Helper()

	_, err := fx.service.AcceptForgeMessage(t.Context(), AcceptForgeMessageParams{
		MessageID: messageID,
		Author:    personAuthor,
	})

	return err
}

func (fx *reviewFixture) sendForgeReply(t *testing.T, providerID string) {
	t.Helper()
	ctx := t.Context()

	threads, err := fx.reviews.ListThreads(ctx, *fx.writer.TaskID)
	require.NoError(t, err)
	require.NotEmpty(t, threads)

	reply, err := fx.reviews.CreateForgeReply(ctx, threads[0].ID, "done")
	require.NoError(t, err)
	_, err = fx.reviews.BeginForgeReplySend(ctx, reply.ID)
	require.NoError(t, err)
	_, err = fx.reviews.MarkForgeReplySent(ctx, reply.ID, providerID)
	require.NoError(t, err)
}
