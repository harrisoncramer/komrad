package reviews

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	forgestore "komrad/internal/store/forge"
	reviewstore "komrad/internal/store/review"
	forgev1 "komrad/proto/gen/forge/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReplyForge struct {
	forge.Forge
	posted    []forge.ReplyToThreadParams
	refusal   error
	cancel    context.CancelFunc
	threads   []forge.ReviewThread
	truncated bool
}

func (f *fakeReplyForge) ReplyToThread(ctx context.Context, params forge.ReplyToThreadParams) (string, error) {
	if f.cancel != nil {
		f.cancel()

		return "", ctx.Err()
	}
	if f.refusal != nil {
		return "", f.refusal
	}
	f.posted = append(f.posted, params)

	return "PRRC_posted", nil
}

func (f *fakeReplyForge) ReviewThreads(context.Context, git.RemoteURL, int) (*forge.Threads, error) {
	return &forge.Threads{Threads: f.threads, Truncated: f.truncated}, nil
}

type fakeForgeHosts struct {
	forge      *fakeReplyForge
	connection forgestore.Connection
}

func (f *fakeForgeHosts) ForHost(context.Context, string) (forge.Forge, error) {
	return f.forge, nil
}

func (f *fakeForgeHosts) ConnectionFor(context.Context, string) (*forgestore.Connection, error) {
	return &f.connection, nil
}

type forgeReplyFixture struct {
	*reviewFixture
	replies  *ForgeReplyService
	forge    *fakeReplyForge
	hosts    *fakeForgeHosts
	threadID uuid.UUID
}

func newForgeReplyFixture(t *testing.T) *forgeReplyFixture {
	t.Helper()

	fx := newReviewFixture(t)
	fx.mirrorForge(t, newForgeThread(newForgeComment("c1", "alice", true, "use a guard")))
	mirrored := fx.readForgeMirrored(t)
	require.Len(t, mirrored.threads, 1)

	fake := &fakeReplyForge{}
	hosts := &fakeForgeHosts{
		forge: fake,
		connection: forgestore.Connection{
			Host:   "github.com",
			Method: forgev1.CredentialMethod_CREDENTIAL_METHOD_TOKEN,
			Login:  "komrad-bot",
		},
	}

	return &forgeReplyFixture{
		reviewFixture: fx,
		replies:       NewForgeReplyService(NewForgeReplyServiceParams{Reviews: fx.reviews, Forges: hosts}),
		forge:         fake,
		hosts:         hosts,
		threadID:      mirrored.threads[0].ID,
	}
}

func (fx *forgeReplyFixture) holdOnForge(comments ...forge.ReviewComment) {
	fx.forge.threads = []forge.ReviewThread{
		newForgeThread(append([]forge.ReviewComment{newForgeComment("c1", "alice", true, "use a guard")}, comments...)...),
	}
}

func (fx *forgeReplyFixture) latestReply(t *testing.T) reviewstore.ForgeReply {
	t.Helper()

	replies, err := fx.reviews.ListForgeReplies(t.Context(), []uuid.UUID{fx.threadID})
	require.NoError(t, err)
	require.NotEmpty(t, replies)

	return replies[len(replies)-1]
}

func postedOnForge(login string, written time.Time) forge.ReviewComment {
	comment := newForgeComment("c9", login, true, "done")
	comment.WrittenAt = written

	return comment
}

// TestSendingForgeReply covers the person's say over what reaches the forge, and the reply posted once.
func TestSendingForgeReply(t *testing.T) {
	reset := errors.New("connection reset")
	later := time.Now().Add(time.Hour)

	replyOnForge := func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
		return fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{
			ThreadID: fx.threadID,
			Author:   personAuthor,
			Body:     "done",
		})
	}

	tests := []struct {
		name      string
		refusal   error
		held      []forge.ReviewComment
		truncated bool
		send      func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error)
		wantErr   error
		wantState reviewstore.ForgeReplyState
		wantBody  string
	}{
		{
			name:      "the person's own reply is posted straight away",
			send:      replyOnForge,
			wantState: reviewstore.ForgeReplySent,
			wantBody:  "done",
		},
		{
			name:      "a reply the forge refuses fails with what the forge said",
			refusal:   forge.ErrUnauthorized,
			held:      []forge.ReviewComment{postedOnForge("komrad-bot", later)},
			send:      replyOnForge,
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "a reply the forge answers unclearly is sent when the forge holds it",
			refusal:   reset,
			held:      []forge.ReviewComment{postedOnForge("komrad-bot", later)},
			send:      replyOnForge,
			wantState: reviewstore.ForgeReplySent,
		},
		{
			name:      "a reply the forge answers unclearly is failed when the forge does not hold it",
			refusal:   reset,
			send:      replyOnForge,
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "a reply the forge answers unclearly stays being sent when the forge answers only in part",
			refusal:   reset,
			truncated: true,
			send:      replyOnForge,
			wantErr:   reset,
			wantState: reviewstore.ForgeReplySending,
		},
		{
			name: "a reply whose caller gave up is still settled from the forge",
			held: []forge.ReviewComment{postedOnForge("komrad-bot", later)},
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				ctx, cancel := context.WithCancel(t.Context())
				fx.forge.cancel = cancel

				return fx.replies.ReplyOnForge(ctx, ReplyOnForgeParams{
					ThreadID: fx.threadID,
					Author:   personAuthor,
					Body:     "done",
				})
			},
			wantState: reviewstore.ForgeReplySent,
		},
		{
			name: "a failed reply is posted again on retry",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				fx.forge.refusal = forge.ErrUnauthorized
				failed, err := replyOnForge(t, fx)
				require.NoError(t, err)
				fx.forge.refusal = nil

				return fx.replies.RetryForgeReply(t.Context(), failed.ID, personAuthor)
			},
			wantState: reviewstore.ForgeReplySent,
			wantBody:  "done",
		},
		{
			name: "an agent cannot reply on the forge directly",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				return fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{
					ThreadID: fx.threadID,
					Author:   newAgentAuthor(fx.writer),
					Body:     "done",
				})
			},
			wantErr: ErrReviewHumanOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)
			fx.forge.refusal = tt.refusal
			fx.forge.truncated = tt.truncated
			fx.holdOnForge(tt.held...)

			reply, err := tt.send(t, fx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantState, reply.State)
			}
			if tt.wantState != "" {
				assert.Equal(t, tt.wantState, fx.latestReply(t).State)
			}

			if tt.wantBody == "" {
				assert.Empty(t, fx.forge.posted)

				return
			}
			require.Len(t, fx.forge.posted, 1)
			assert.Equal(t, forge.ReplyToThreadParams{
				Repo:     fx.forge.posted[0].Repo,
				Number:   1,
				ThreadID: "T1",
				OnCode:   true,
				Body:     tt.wantBody,
			}, fx.forge.posted[0])
			assert.Equal(t, "github.com", fx.forge.posted[0].Repo.Host)
		})
	}
}

// TestDiscardingForgeReply covers the person throwing away a reply the forge refused.
func TestDiscardingForgeReply(t *testing.T) {
	fx := newForgeReplyFixture(t)
	fx.forge.refusal = forge.ErrUnauthorized
	failed, err := fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{
		ThreadID: fx.threadID,
		Author:   personAuthor,
		Body:     "done",
	})
	require.NoError(t, err)

	require.ErrorIs(t, fx.replies.DiscardForgeReply(t.Context(), failed.ID, newAgentAuthor(fx.writer)), ErrReviewHumanOnly)
	require.NoError(t, fx.replies.DiscardForgeReply(t.Context(), failed.ID, personAuthor))

	replies, err := fx.reviews.ListForgeReplies(t.Context(), []uuid.UUID{fx.threadID})
	require.NoError(t, err)
	assert.Empty(t, replies)
}

// TestReconcilingForgeReplies covers replies komrad stopped partway through posting, settled from what the forge holds.
func TestReconcilingForgeReplies(t *testing.T) {
	later := time.Now().Add(time.Hour)
	earlier := time.Now().Add(-time.Hour)
	behind := time.Now().Add(-30 * time.Second)

	tests := []struct {
		name      string
		method    forgev1.CredentialMethod
		held      []forge.ReviewComment
		truncated bool
		wantErr   bool
		wantState reviewstore.ForgeReplyState
	}{
		{
			name:      "a reply the forge holds from komrad is sent",
			held:      []forge.ReviewComment{postedOnForge("komrad-bot", later)},
			wantState: reviewstore.ForgeReplySent,
		},
		{
			name:      "a reply the forge does not hold is failed",
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "a reply the forge answers only part of the threads for stays being sent",
			truncated: true,
			wantErr:   true,
			wantState: reviewstore.ForgeReplySending,
		},
		{
			name:      "the same words from someone else do not count",
			held:      []forge.ReviewComment{postedOnForge("alice", later)},
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "the same words written before the posting began do not count",
			held:      []forge.ReviewComment{postedOnForge("komrad-bot", earlier)},
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "the forge's clock running a little behind still counts",
			held:      []forge.ReviewComment{postedOnForge("komrad-bot", behind)},
			wantState: reviewstore.ForgeReplySent,
		},
		{
			name:      "a GitHub App, whose login is unknown, matches on the words alone",
			method:    forgev1.CredentialMethod_CREDENTIAL_METHOD_GITHUB_APP,
			held:      []forge.ReviewComment{postedOnForge("komrad[bot]", later)},
			wantState: reviewstore.ForgeReplySent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)
			if tt.method != forgev1.CredentialMethod_CREDENTIAL_METHOD_UNSPECIFIED {
				fx.hosts.connection.Method = tt.method
			}

			reply, err := fx.reviews.CreateForgeReply(t.Context(), fx.threadID, "done")
			require.NoError(t, err)
			_, err = fx.reviews.BeginForgeReplySend(t.Context(), reply.ID)
			require.NoError(t, err)

			fx.holdOnForge(tt.held...)
			fx.forge.truncated = tt.truncated

			err = fx.replies.ReconcileForgeReplies(t.Context())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			settled, err := fx.reviews.GetForgeReply(t.Context(), reply.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, settled.State)
			if tt.wantState == reviewstore.ForgeReplySent {
				require.NotNil(t, settled.ForgeCommentProviderID)
				assert.Equal(t, "c9", *settled.ForgeCommentProviderID)
			}
			assert.Empty(t, fx.forge.posted)
		})
	}
}
