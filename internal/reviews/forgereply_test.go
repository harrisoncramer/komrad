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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeReplyForge struct {
	forge.Forge
	posted  []forge.ReplyToThreadParams
	refusal error
	threads []forge.ReviewThread
}

func (f *fakeReplyForge) ReplyToThread(_ context.Context, params forge.ReplyToThreadParams) (string, error) {
	if f.refusal != nil {
		return "", f.refusal
	}
	f.posted = append(f.posted, params)

	return "PRRC_posted", nil
}

func (f *fakeReplyForge) ReviewThreads(context.Context, git.RemoteURL, int) (*forge.Threads, error) {
	return &forge.Threads{Threads: f.threads}, nil
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
	threadID string
}

func newForgeReplyFixture(t *testing.T) *forgeReplyFixture {
	t.Helper()

	fx := newReviewFixture(t)
	fx.mirrorForge(t, newForgeThread(newForgeComment("c1", "alice", true, "use a guard")))
	mirrored := fx.readForgeMirrored(t)
	require.Len(t, mirrored.threads, 1)

	fake := &fakeReplyForge{}
	hosts := &fakeForgeHosts{
		forge:      fake,
		connection: forgestore.Connection{Host: "github.com", Method: "token", Login: "komrad-bot"},
	}

	return &forgeReplyFixture{
		reviewFixture: fx,
		replies:       NewForgeReplyService(NewForgeReplyServiceParams{Reviews: fx.reviews, Forges: hosts}),
		forge:         fake,
		hosts:         hosts,
		threadID:      mirrored.threads[0].ID,
	}
}

func (fx *forgeReplyFixture) propose(t *testing.T) *reviewstore.ForgeReply {
	t.Helper()

	draft, err := fx.replies.ProposeForgeReply(t.Context(), ProposeForgeReplyParams{
		ThreadID: fx.threadID,
		Author:   newAgentAuthor(fx.worker),
		Body:     "added the guard",
	})
	require.NoError(t, err)

	return draft
}

// TestProposingForgeReply covers who may draft a reply for the forge on a mirrored thread.
func TestProposingForgeReply(t *testing.T) {
	tests := []struct {
		name    string
		author  func(fx *forgeReplyFixture) ReviewAuthor
		wantErr error
	}{
		{
			name:   "the agent whose pull request it is drafts a reply",
			author: func(fx *forgeReplyFixture) ReviewAuthor { return newAgentAuthor(fx.worker) },
		},
		{
			name:    "another agent is refused",
			author:  func(fx *forgeReplyFixture) ReviewAuthor { return newAgentAuthor(fx.stranger) },
			wantErr: ErrReviewNotYours,
		},
		{
			name:    "the person is refused",
			author:  func(*forgeReplyFixture) ReviewAuthor { return personAuthor },
			wantErr: ErrReviewNotYours,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)

			draft, err := fx.replies.ProposeForgeReply(t.Context(), ProposeForgeReplyParams{
				ThreadID: fx.threadID,
				Author:   tt.author(fx),
				Body:     "added the guard",
			})
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, reviewstore.ForgeReplyDraft, draft.State)
			assert.Empty(t, fx.forge.posted)
		})
	}
}

// TestSendingForgeReply covers the person's say over what reaches the forge, and the reply posted once.
func TestSendingForgeReply(t *testing.T) {
	edited := "added the guard, thanks"

	tests := []struct {
		name      string
		refusal   error
		send      func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error)
		wantErr   error
		wantState reviewstore.ForgeReplyState
		wantBody  string
	}{
		{
			name: "approving a draft posts it",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				draft := fx.propose(t)

				return fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{ID: draft.ID, Author: personAuthor})
			},
			wantState: reviewstore.ForgeReplySent,
			wantBody:  "added the guard",
		},
		{
			name: "approving an edited draft posts the edit",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				draft := fx.propose(t)

				return fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{ID: draft.ID, Author: personAuthor, Body: &edited})
			},
			wantState: reviewstore.ForgeReplySent,
			wantBody:  edited,
		},
		{
			name: "the person's own reply is posted straight away",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				return fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{ThreadID: fx.threadID, Author: personAuthor, Body: "done"})
			},
			wantState: reviewstore.ForgeReplySent,
			wantBody:  "done",
		},
		{
			name:    "a reply the forge refuses fails with what the forge said",
			refusal: errors.New("forbidden"),
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				return fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{ThreadID: fx.threadID, Author: personAuthor, Body: "done"})
			},
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name: "a failed reply is posted again on retry",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				fx.forge.refusal = errors.New("forbidden")
				failed, err := fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{ThreadID: fx.threadID, Author: personAuthor, Body: "done"})
				require.NoError(t, err)
				fx.forge.refusal = nil

				return fx.replies.RetryForgeReply(t.Context(), failed.ID, personAuthor)
			},
			wantState: reviewstore.ForgeReplySent,
			wantBody:  "done",
		},
		{
			name: "an agent cannot approve its own draft",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				draft := fx.propose(t)

				return fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{ID: draft.ID, Author: newAgentAuthor(fx.worker)})
			},
			wantErr: ErrReviewHumanOnly,
		},
		{
			name: "an agent cannot reply on the forge directly",
			send: func(t *testing.T, fx *forgeReplyFixture) (*reviewstore.ForgeReply, error) {
				return fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{ThreadID: fx.threadID, Author: newAgentAuthor(fx.worker), Body: "done"})
			},
			wantErr: ErrReviewHumanOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)
			fx.forge.refusal = tt.refusal

			reply, err := tt.send(t, fx)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantState, reply.State)
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

// TestEditedForgeReplyKeepsProposal covers the agent's words kept beside the person's edit that reached the forge.
func TestEditedForgeReplyKeepsProposal(t *testing.T) {
	fx := newForgeReplyFixture(t)
	draft := fx.propose(t)
	edited := "added the guard, thanks"

	sent, err := fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{ID: draft.ID, Author: personAuthor, Body: &edited})
	require.NoError(t, err)

	assert.Equal(t, edited, sent.Body)
	require.NotNil(t, sent.ProposedBody)
	assert.Equal(t, "added the guard", *sent.ProposedBody)
	require.NotNil(t, sent.ForgeCommentProviderID)
	assert.Equal(t, "PRRC_posted", *sent.ForgeCommentProviderID)
}

// TestDiscardingForgeReply covers the person throwing away a draft before it reaches the forge.
func TestDiscardingForgeReply(t *testing.T) {
	fx := newForgeReplyFixture(t)
	draft := fx.propose(t)

	require.ErrorIs(t, fx.replies.DiscardForgeReply(t.Context(), draft.ID, newAgentAuthor(fx.worker)), ErrReviewHumanOnly)
	require.NoError(t, fx.replies.DiscardForgeReply(t.Context(), draft.ID, personAuthor))

	replies, err := fx.reviews.ListForgeReplies(t.Context(), []string{fx.threadID})
	require.NoError(t, err)
	assert.Empty(t, replies)
}

// TestReconcilingForgeReplies covers replies komrad stopped partway through posting, settled from what the forge holds.
func TestReconcilingForgeReplies(t *testing.T) {
	later := time.Now().Add(time.Hour)
	earlier := time.Now().Add(-time.Hour)

	posted := func(login, body string, written time.Time) forge.ReviewComment {
		comment := newForgeComment("c9", login, true, body)
		comment.WrittenAt = written

		return comment
	}

	tests := []struct {
		name      string
		method    string
		held      []forge.ReviewComment
		wantState reviewstore.ForgeReplyState
	}{
		{
			name:      "a reply the forge holds from komrad is sent",
			held:      []forge.ReviewComment{posted("komrad-bot", "done", later)},
			wantState: reviewstore.ForgeReplySent,
		},
		{
			name:      "a reply the forge does not hold is failed",
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "the same words from someone else do not count",
			held:      []forge.ReviewComment{posted("alice", "done", later)},
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "the same words written before the posting began do not count",
			held:      []forge.ReviewComment{posted("komrad-bot", "done", earlier)},
			wantState: reviewstore.ForgeReplyFailed,
		},
		{
			name:      "a GitHub App, whose login is unknown, matches on the words alone",
			method:    forge.MethodGitHubApp,
			held:      []forge.ReviewComment{posted("komrad[bot]", "done", later)},
			wantState: reviewstore.ForgeReplySent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)
			if tt.method != "" {
				fx.hosts.connection.Method = tt.method
			}

			reply, err := fx.reviews.CreateHumanForgeReply(t.Context(), fx.threadID, "done")
			require.NoError(t, err)
			_, err = fx.reviews.BeginForgeReplySend(t.Context(), reply.ID)
			require.NoError(t, err)

			thread := newForgeThread(append([]forge.ReviewComment{newForgeComment("c1", "alice", true, "use a guard")}, tt.held...)...)
			fx.forge.threads = []forge.ReviewThread{thread}

			require.NoError(t, fx.replies.ReconcileForgeReplies(t.Context()))

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
