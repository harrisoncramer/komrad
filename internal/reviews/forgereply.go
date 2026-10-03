package reviews

import (
	"context"
	"errors"
	"fmt"
	"time"

	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/store"
	forgestore "komrad/internal/store/forge"
	reviewstore "komrad/internal/store/review"
)

// errForgeReplyLost is the error a reply is failed with when komrad stopped while posting it and
// the forge shows no comment it made.
const errForgeReplyLost = "komrad stopped while posting this reply and the forge shows no comment from it"

type forgeHosts interface {
	ForHost(ctx context.Context, host string) (forge.Forge, error)
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
}

// NewForgeReplyServiceParams is what the forge reply service reads and posts through.
type NewForgeReplyServiceParams struct {
	Reviews *reviewstore.ReviewStore
	Forges  forgeHosts
}

// ForgeReplyService posts replies on mirrored threads to the forge, once the person approves them.
type ForgeReplyService struct {
	reviews *reviewstore.ReviewStore
	forges  forgeHosts
}

// NewForgeReplyService builds a forge reply service over the review store and the forge connections.
func NewForgeReplyService(params NewForgeReplyServiceParams) *ForgeReplyService {
	return &ForgeReplyService{
		reviews: params.Reviews,
		forges:  params.Forges,
	}
}

// ProposeForgeReplyParams is an agent's reply on a thread mirrored from its pull request.
type ProposeForgeReplyParams struct {
	ThreadID string
	Author   ReviewAuthor
	Body     string
}

// ProposeForgeReply drafts the agent's reply for the person to approve.
func (s *ForgeReplyService) ProposeForgeReply(ctx context.Context, params ProposeForgeReplyParams) (*reviewstore.ForgeReply, error) {
	if params.Author.isHuman() {
		return nil, fmt.Errorf("%w: the person replies on the forge directly", ErrReviewNotYours)
	}

	thread, err := s.reviews.GetThread(ctx, params.ThreadID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%w: %q", ErrReviewThreadNotFound, params.ThreadID)
	}
	if err != nil {
		return nil, err
	}
	if !doesOwnAgent(params.Author, thread.AgentID) {
		return nil, fmt.Errorf("%w: %q", ErrReviewNotYours, params.ThreadID)
	}

	return s.reviews.CreateAgentForgeReply(ctx, reviewstore.CreateAgentForgeReplyParams{
		ThreadID: thread.ID,
		AgentID:  thread.AgentID,
		Body:     params.Body,
	})
}

// ReplyOnForgeParams is the person's own reply on a mirrored thread.
type ReplyOnForgeParams struct {
	ThreadID string
	Author   ReviewAuthor
	Body     string
}

// ReplyOnForge posts the person's reply to the forge straight away.
func (s *ForgeReplyService) ReplyOnForge(ctx context.Context, params ReplyOnForgeParams) (*reviewstore.ForgeReply, error) {
	if !params.Author.isHuman() {
		return nil, ErrReviewHumanOnly
	}

	reply, err := s.reviews.CreateHumanForgeReply(ctx, params.ThreadID, params.Body)
	if err != nil {
		return nil, err
	}

	return s.send(ctx, reply)
}

// ApproveForgeReplyParams is the person's approval of an agent's draft, with their edit when they made one.
type ApproveForgeReplyParams struct {
	ID     string
	Author ReviewAuthor
	Body   *string
}

// ApproveForgeReply approves a draft and posts it to the forge.
func (s *ForgeReplyService) ApproveForgeReply(ctx context.Context, params ApproveForgeReplyParams) (*reviewstore.ForgeReply, error) {
	if !params.Author.isHuman() {
		return nil, ErrReviewHumanOnly
	}

	draft, err := s.reviews.GetForgeReply(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	body := draft.Body
	if params.Body != nil {
		body = *params.Body
	}

	approved, err := s.reviews.MarkForgeReplyApproved(ctx, reviewstore.MarkForgeReplyApprovedParams{
		ID:           draft.ID,
		Body:         body,
		ApproverKind: reviewstore.ForgeReplyApproverHuman,
	})
	if err != nil {
		return nil, err
	}

	return s.send(ctx, approved)
}

// RetryForgeReply posts a failed reply again.
func (s *ForgeReplyService) RetryForgeReply(ctx context.Context, id string, author ReviewAuthor) (*reviewstore.ForgeReply, error) {
	if !author.isHuman() {
		return nil, ErrReviewHumanOnly
	}

	reply, err := s.reviews.GetForgeReply(ctx, id)
	if err != nil {
		return nil, err
	}

	return s.send(ctx, reply)
}

// DiscardForgeReply deletes a draft or failed reply.
func (s *ForgeReplyService) DiscardForgeReply(ctx context.Context, id string, author ReviewAuthor) error {
	if !author.isHuman() {
		return ErrReviewHumanOnly
	}

	return s.reviews.DeleteForgeReply(ctx, id)
}

// ReconcileForgeReplies settles every reply komrad stopped partway through posting: one the forge
// shows a comment for is sent, and any other is failed for the person to retry.
func (s *ForgeReplyService) ReconcileForgeReplies(ctx context.Context) error {
	replies, err := s.reviews.ListSendingForgeReplies(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, reply := range replies {
		if err := s.reconcile(ctx, reply); err != nil {
			errs = append(errs, fmt.Errorf("failed to settle forge reply %q: %w", reply.ID, err))
		}
	}

	return errors.Join(errs...)
}

func (s *ForgeReplyService) reconcile(ctx context.Context, reply reviewstore.ForgeReply) error {
	target, err := s.reviews.GetForgeReplyTarget(ctx, reply.ThreadID)
	if err != nil {
		return err
	}
	remote, err := git.ParseRemoteURL(target.RepositoryURL)
	if err != nil {
		return err
	}
	connection, err := s.forges.ConnectionFor(ctx, remote.Host)
	if err != nil {
		return err
	}
	client, err := s.forges.ForHost(ctx, remote.Host)
	if err != nil {
		return err
	}
	threads, err := client.ReviewThreads(ctx, remote, target.Number)
	if err != nil {
		return err
	}

	posted := findPostedReply(findPostedReplyParams{
		Threads: threads.Threads,
		Target:  *target,
		Reply:   reply,
		Viewer:  viewerOf(*connection),
	})
	if posted == "" {
		_, err = s.reviews.MarkForgeReplyFailed(ctx, reply.ID, errForgeReplyLost)

		return err
	}
	_, err = s.reviews.MarkForgeReplySent(ctx, reply.ID, posted)

	return err
}

type findPostedReplyParams struct {
	Threads []forge.ReviewThread
	Target  reviewstore.ForgeReplyTarget
	Reply   reviewstore.ForgeReply
	Viewer  string
}

// findPostedReply is the id of the forge comment a reply being posted became, or empty when the
// forge holds none written by komrad's identity after the posting began with the reply's words.
func findPostedReply(params findPostedReplyParams) string {
	began := time.Time{}
	if params.Reply.SendingStartedAt != nil {
		began = *params.Reply.SendingStartedAt
	}

	for _, thread := range params.Threads {
		if params.Target.Path != "" && thread.ProviderID != params.Target.ProviderID {
			continue
		}
		if params.Target.Path == "" && thread.Path != "" {
			continue
		}
		for _, comment := range thread.Comments {
			if comment.Body == params.Reply.Body &&
				!comment.WrittenAt.Before(began) &&
				(params.Viewer == "" || comment.Login == params.Viewer) {
				return comment.ProviderID
			}
		}
	}

	return ""
}

func viewerOf(connection forgestore.Connection) string {
	if connection.Method == forge.MethodGitHubApp {
		return ""
	}

	return connection.Login
}

// send posts an approved or failed reply to the forge at most once, recording the comment it made
// or what the forge said when it refused.
func (s *ForgeReplyService) send(ctx context.Context, reply *reviewstore.ForgeReply) (*reviewstore.ForgeReply, error) {
	target, err := s.reviews.GetForgeReplyTarget(ctx, reply.ThreadID)
	if err != nil {
		return nil, err
	}
	remote, err := git.ParseRemoteURL(target.RepositoryURL)
	if err != nil {
		return nil, err
	}
	client, err := s.forges.ForHost(ctx, remote.Host)
	if err != nil {
		return nil, err
	}

	sending, err := s.reviews.BeginForgeReplySend(ctx, reply.ID)
	if err != nil {
		return nil, err
	}

	posted, err := client.ReplyToThread(ctx, forge.ReplyToThreadParams{
		Repo:     remote,
		Number:   target.Number,
		ThreadID: target.ProviderID,
		OnCode:   target.Path != "",
		Body:     sending.Body,
	})
	if err != nil {
		return s.reviews.MarkForgeReplyFailed(ctx, sending.ID, err.Error())
	}

	return s.reviews.MarkForgeReplySent(ctx, sending.ID, posted)
}
