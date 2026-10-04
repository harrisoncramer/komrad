package reviews

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"komrad/internal/eventprompts"
	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/jobs/internaljobs"
	forgestore "komrad/internal/store/forge"
	mailstore "komrad/internal/store/mail"
	reviewstore "komrad/internal/store/review"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
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
		forge: fake,
		connection: forgestore.Connection{
			Host:   "github.com",
			Method: "token",
			Login:  "komrad-bot",
		},
	}

	return &forgeReplyFixture{
		reviewFixture: fx,
		replies:       NewForgeReplyService(NewForgeReplyServiceParams{Reviews: fx.reviews, Forges: hosts, Events: fx.events}),
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

	replies, err := fx.reviews.ListForgeReplies(t.Context(), []string{fx.threadID})
	require.NoError(t, err)
	require.NotEmpty(t, replies)

	return replies[len(replies)-1]
}

func postedOnForge(login string, written time.Time) forge.ReviewComment {
	comment := newForgeComment("c9", login, true, "done")
	comment.WrittenAt = written

	return comment
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

				return fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{
					ID:     draft.ID,
					Author: personAuthor,
					Body:   &edited,
				})
			},
			wantState: reviewstore.ForgeReplySent,
			wantBody:  edited,
		},
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
				return fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{
					ThreadID: fx.threadID,
					Author:   newAgentAuthor(fx.worker),
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

// TestEditedForgeReplyKeepsProposal covers the agent's words kept beside the person's edit that reached the forge.
func TestEditedForgeReplyKeepsProposal(t *testing.T) {
	fx := newForgeReplyFixture(t)
	draft := fx.propose(t)
	edited := "added the guard, thanks"

	sent, err := fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{
		ID:     draft.ID,
		Author: personAuthor,
		Body:   &edited,
	})
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
	behind := time.Now().Add(-30 * time.Second)

	tests := []struct {
		name      string
		method    string
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
			method:    forge.MethodGitHubApp,
			held:      []forge.ReviewComment{postedOnForge("komrad[bot]", later)},
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

func (fx *forgeReplyFixture) mailAbout(t *testing.T, event string) []mailstore.Mail {
	t.Helper()

	return slices.DeleteFunc(fx.listMail(t, fx.worker.ID), func(m mailstore.Mail) bool {
		return !slices.Contains(m.Events, event)
	})
}

// TestForgeReplyDecisionMail covers the agent hearing what the person did with its draft.
func TestForgeReplyDecisionMail(t *testing.T) {
	edited := "added the guard, thanks"

	tests := []struct {
		name     string
		decide   func(t *testing.T, fx *forgeReplyFixture) string
		wantBody []string
	}{
		{
			name: "a draft sent as written",
			decide: func(t *testing.T, fx *forgeReplyFixture) string {
				draft := fx.propose(t)
				_, err := fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{ID: draft.ID, Author: personAuthor})
				require.NoError(t, err)

				return draft.ID
			},
			wantBody: []string{"as you wrote it"},
		},
		{
			name: "a draft the person edited",
			decide: func(t *testing.T, fx *forgeReplyFixture) string {
				draft := fx.propose(t)
				_, err := fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{
					ID:     draft.ID,
					Author: personAuthor,
					Body:   &edited,
				})
				require.NoError(t, err)

				return draft.ID
			},
			wantBody: []string{"edited your draft", edited},
		},
		{
			name: "a draft the person discarded",
			decide: func(t *testing.T, fx *forgeReplyFixture) string {
				draft := fx.propose(t)
				require.NoError(t, fx.replies.DiscardForgeReply(t.Context(), draft.ID, personAuthor))

				return draft.ID
			},
			wantBody: []string{"discarded your draft", "Nothing was posted"},
		},
		{
			name: "a draft retried after the forge refused it",
			decide: func(t *testing.T, fx *forgeReplyFixture) string {
				draft := fx.propose(t)
				fx.forge.refusal = forge.ErrUnauthorized
				_, err := fx.replies.ApproveForgeReply(t.Context(), ApproveForgeReplyParams{ID: draft.ID, Author: personAuthor})
				require.NoError(t, err)
				assert.Empty(t, fx.mailAbout(t, eventprompts.ReviewForgeReplyDecided), "a refused reply has not gone out yet")
				fx.forge.refusal = nil
				_, err = fx.replies.RetryForgeReply(t.Context(), draft.ID, personAuthor)
				require.NoError(t, err)

				return draft.ID
			},
			wantBody: []string{"as you wrote it"},
		},
		{
			name: "the person's own reply",
			decide: func(t *testing.T, fx *forgeReplyFixture) string {
				_, err := fx.replies.ReplyOnForge(t.Context(), ReplyOnForgeParams{
					ThreadID: fx.threadID,
					Author:   personAuthor,
					Body:     "done",
				})
				require.NoError(t, err)

				return ""
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)
			fx.holdOnForge()

			replyID := tt.decide(t, fx)

			mailed := fx.mailAbout(t, eventprompts.ReviewForgeReplyDecided)
			assert.Empty(t, fx.events.ListFollowUps(), "a decision does not wake the agent")
			if tt.wantBody == nil {
				assert.Empty(t, mailed)

				return
			}
			require.Len(t, mailed, 1)
			assert.Equal(t, mailstore.KindReviewActivity, mailed[0].Kind)
			assert.Equal(t, &replyID, mailed[0].SubjectID)
			assert.Contains(t, mailed[0].Body, fx.threadID)
			for _, want := range tt.wantBody {
				assert.Contains(t, mailed[0].Body, want)
			}
		})
	}
}

// TestRequestingForgeReply covers the person asking the agent to draft a reply on a forge thread.
func TestRequestingForgeReply(t *testing.T) {
	tests := []struct {
		name             string
		thread           func(t *testing.T, fx *forgeReplyFixture) string
		author           func(fx *forgeReplyFixture) ReviewAuthor
		wantErr          error
		wantPrecondition bool
	}{
		{
			name:   "the person asks on a mirrored thread",
			thread: func(_ *testing.T, fx *forgeReplyFixture) string { return fx.threadID },
			author: func(*forgeReplyFixture) ReviewAuthor { return personAuthor },
		},
		{
			name:    "an agent cannot ask",
			thread:  func(_ *testing.T, fx *forgeReplyFixture) string { return fx.threadID },
			author:  func(fx *forgeReplyFixture) ReviewAuthor { return newAgentAuthor(fx.reviewer) },
			wantErr: ErrReviewHumanOnly,
		},
		{
			name:    "a thread that does not exist",
			thread:  func(*testing.T, *forgeReplyFixture) string { return uuid.NewString() },
			author:  func(*forgeReplyFixture) ReviewAuthor { return personAuthor },
			wantErr: ErrReviewThreadNotFound,
		},
		{
			name: "a thread that mirrors nothing on the forge",
			thread: func(t *testing.T, fx *forgeReplyFixture) string {
				return fx.writeComment(t, personAuthor, "this needs a nil check").Thread.ID
			},
			author:           func(*forgeReplyFixture) ReviewAuthor { return personAuthor },
			wantPrecondition: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fx := newForgeReplyFixture(t)
			threadID := tt.thread(t, fx)

			err := fx.replies.RequestForgeReply(t.Context(), threadID, tt.author(fx))
			switch {
			case tt.wantErr != nil:
				require.ErrorIs(t, err, tt.wantErr)
			case tt.wantPrecondition:
				require.NotNil(t, fail.Of(err))
				assert.Equal(t, fail.KindPrecondition, fail.Of(err).Kind())
			default:
				require.NoError(t, err)
			}

			mailed := fx.mailAbout(t, eventprompts.ReviewForgeReplyRequested)
			if tt.wantErr != nil || tt.wantPrecondition {
				assert.Empty(t, mailed)
				assert.Empty(t, fx.events.ListFollowUps())

				return
			}
			assert.Equal(t, []river.JobArgs{internaljobs.WakeAgentArgs{AgentID: fx.worker.ID}}, fx.events.ListFollowUps())
			require.Len(t, mailed, 1)
			assert.Equal(t, mailstore.KindReviewActivity, mailed[0].Kind)
			assert.Equal(t, &fx.threadID, mailed[0].SubjectID)
			assert.Contains(t, mailed[0].Body, "propose_forge_reply")
		})
	}
}
