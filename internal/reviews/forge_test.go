package reviews

import (
	"encoding/json"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/gittest"
	reviewstore "komrad/internal/store/review"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forgeMirrored is what the worker holds after the forge's threads were mirrored onto it.
type forgeMirrored struct {
	threads  []reviewstore.Thread
	messages []reviewstore.Message
	reviews  []reviewstore.Review
	released []Released
}

// TestForgeMirror covers forge threads mirrored onto the worker's round, released when the author can push and held when not.
func TestForgeMirror(t *testing.T) {
	edited := time.Date(2026, time.September, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		syncs [][]forge.ReviewThread
		check func(t *testing.T, got forgeMirrored)
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
					AgentID:    got.reviews[0].AgentID,
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
				require.Len(t, got.reviews, 1)
				assert.Nil(t, got.reviews[0].ReleasedAt)
				assert.Empty(t, got.released)
			},
		},
		{
			name: "a thread resolved on the forge before it was seen is left out",
			syncs: [][]forge.ReviewThread{{func() forge.ReviewThread {
				thread := newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))
				thread.Resolved = true

				return thread
			}()}},
			check: func(t *testing.T, got forgeMirrored) {
				assert.Empty(t, got.threads)
				assert.Empty(t, got.released)
			},
		},
		{
			name: "an edit on the forge rewrites the message",
			syncs: [][]forge.ReviewThread{
				{newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))},
				{newForgeThread(func() forge.ReviewComment {
					comment := newForgeComment("c1", "alice", true, "use an early return")
					comment.EditedAt = &edited

					return comment
				}())},
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.messages, 1)
				assert.Equal(t, "use an early return", got.messages[0].Body)
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
				{func() forge.ReviewThread {
					thread := newForgeThread(newForgeComment("c1", "alice", true, "use a guard"))
					thread.Resolved = true

					return thread
				}()},
			},
			check: func(t *testing.T, got forgeMirrored) {
				require.Len(t, got.threads, 1)
				assert.NotNil(t, got.threads[0].ResolvedAt)
				assert.Equal(t, reviewstore.AuthorForge, got.threads[0].ResolvedByKind)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)

			for _, sync := range tt.syncs {
				fx.mirrorForge(t, sync...)
			}

			tt.check(t, fx.readForgeMirrored(t))
		})
	}
}

// TestReleasingHeldForgeComments covers the person letting a held forge comment through to the agent.
func TestReleasingHeldForgeComments(t *testing.T) {
	tests := []struct {
		name    string
		release func(t *testing.T, fx *reviewFixture, threadID string)
	}{
		{
			name: "the person sends a review with nothing of their own in it",
			release: func(t *testing.T, fx *reviewFixture, _ string) {
				fx.releaseReview(t, personAuthor, "")
			},
		},
		{
			name: "the person replies in the held thread",
			release: func(t *testing.T, fx *reviewFixture, threadID string) {
				_, err := fx.service.AddReviewMessage(t.Context(), AddReviewMessageParams{
					ThreadID: threadID,
					Body:     "go ahead",
					Author:   personAuthor,
				})
				require.NoError(t, err)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newReviewFixture(t)
			fx.mirrorForge(t, newForgeThread(newForgeComment("c1", "mallory", false, "use a guard")))
			held := fx.readForgeMirrored(t)
			require.Len(t, held.threads, 1)
			require.Empty(t, held.released)

			tt.release(t, fx, held.threads[0].ID)

			got := fx.readForgeMirrored(t)
			forgeReviews := xslice.Filter(got.reviews, func(review reviewstore.Review) bool {
				return review.ExternalLogin != nil
			})
			require.Len(t, forgeReviews, 1)
			assert.NotNil(t, forgeReviews[0].ReleasedAt)
			forgeReleased := xslice.Filter(got.released, func(released Released) bool {
				return released.ForgeLogin == "mallory"
			})
			assert.Len(t, forgeReleased, 1)
		})
	}
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

// mirrorForge has the forge hold threads on the worker's pull request and mirrors them onto it.
func (fx *reviewFixture) mirrorForge(t *testing.T, threads ...forge.ReviewThread) {
	t.Helper()

	stored := fx.stores.SeedForgeThreads(t, fx.worker.ID, threads...)
	require.NotEmpty(t, stored)

	mirror := NewForgeMirror(NewForgeMirrorParams{
		Reviews:      fx.reviews,
		ForgeThreads: fx.stores.ForgeThreads,
		Git:          gittest.NoRepo{},
		Events:       fx.events,
	})
	require.NoError(t, mirror.Mirror(t.Context(), stored[0].PullRequestID))
}

func (fx *reviewFixture) readForgeMirrored(t *testing.T) forgeMirrored {
	t.Helper()
	ctx := t.Context()

	threads, err := fx.reviews.ListThreads(ctx, fx.worker.ID)
	require.NoError(t, err)

	messages, err := fx.reviews.ListMessages(ctx, xslice.Map(threads, func(thread reviewstore.Thread) string { return thread.ID }))
	require.NoError(t, err)

	reviews, err := fx.reviews.ListReviews(ctx, fx.worker.ID)
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
