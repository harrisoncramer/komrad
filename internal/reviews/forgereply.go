package reviews

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/git"
	forgestore "komrad/internal/store/forge"
	reviewstore "komrad/internal/store/review"
)

// errForgeReplyLost is the error a reply is failed with when komrad stopped while posting it and
// the forge shows no comment it made.
var errForgeReplyLost = fail.Internal("komrad stopped while posting this reply and the forge shows no comment from it")

// refusalKinds are the failures a forge answers with when it turned a reply down without posting it.
var refusalKinds = []fail.Kind{fail.KindNotFound, fail.KindDenied, fail.KindExhausted, fail.KindPrecondition, fail.KindInvalidArgument}

// clockSkew is how far the forge's clock may run behind komrad's when matching a posted reply.
const clockSkew = time.Minute

type forgeHosts interface {
	ForHost(ctx context.Context, host string) (forge.Forge, error)
	ConnectionFor(ctx context.Context, host string) (*forgestore.Connection, error)
}

// NewForgeReplyServiceParams is what the forge reply service reads and posts through.
type NewForgeReplyServiceParams struct {
	Reviews *reviewstore.ReviewStore
	Forges  forgeHosts
}

// ForgeReplyService posts the person's replies on mirrored threads to the forge.
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

// DiscardForgeReply deletes a failed reply.
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
	destination, err := s.resolveForgeReply(ctx, reply.ThreadID)
	if err != nil {
		return err
	}

	_, err = s.settle(ctx, settleParams{
		Destination: *destination,
		Reply:       reply,
		Cause:       errForgeReplyLost,
	})

	return err
}

type forgeReplyDestination struct {
	Target reviewstore.ForgeReplyTarget
	Remote git.RemoteURL
	Client forge.Forge
}

// resolveForgeReply returns the forge thread, repository and client a reply on a review thread goes through.
func (s *ForgeReplyService) resolveForgeReply(ctx context.Context, threadID string) (*forgeReplyDestination, error) {
	target, err := s.reviews.GetForgeReplyTarget(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the forge for review thread %q: %w", threadID, err)
	}
	remote, err := git.ParseRemoteURL(target.RepositoryURL)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the forge for review thread %q: %w", threadID, err)
	}
	client, err := s.forges.ForHost(ctx, remote.Host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve the forge for review thread %q: %w", threadID, err)
	}

	return &forgeReplyDestination{
		Target: *target,
		Remote: remote,
		Client: client,
	}, nil
}

type settleParams struct {
	Destination forgeReplyDestination
	Reply       reviewstore.ForgeReply
	Cause       error
}

// settle marks a reply being posted sent when the forge holds its comment and failed with the
// cause when a whole read of the forge shows none, leaving it being sent when the read falls short.
func (s *ForgeReplyService) settle(ctx context.Context, params settleParams) (*reviewstore.ForgeReply, error) {
	destination := params.Destination
	connection, err := s.forges.ConnectionFor(ctx, destination.Remote.Host)
	if err != nil {
		return nil, errors.Join(params.Cause, err)
	}
	threads, err := destination.Client.ReviewThreads(ctx, destination.Remote, destination.Target.Number)
	if err != nil {
		return nil, errors.Join(params.Cause, err)
	}

	posted := findPostedReply(findPostedReplyParams{
		Threads: threads.Threads,
		Target:  destination.Target,
		Reply:   params.Reply,
		Viewer:  getViewerLogin(*connection),
	})
	if posted != "" {
		return s.reviews.MarkForgeReplySent(ctx, params.Reply.ID, posted)
	}
	if threads.Truncated {
		return nil, fmt.Errorf("%w: the forge answered only part of pull request %d's threads", params.Cause, destination.Target.Number)
	}

	return s.reviews.MarkForgeReplyFailed(ctx, params.Reply.ID, params.Cause.Error())
}

type findPostedReplyParams struct {
	Threads []forge.ReviewThread
	Target  reviewstore.ForgeReplyTarget
	Reply   reviewstore.ForgeReply
	Viewer  string
}

// findPostedReply returns the id of the forge comment a reply being posted became, or empty when
// the forge holds none with the reply's words by komrad's identity written later than clockSkew
// before the posting began.
func findPostedReply(params findPostedReplyParams) string {
	var began time.Time
	if params.Reply.SendingStartedAt != nil {
		began = params.Reply.SendingStartedAt.Truncate(time.Second).Add(-clockSkew)
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

// getViewerLogin returns the login komrad posts as, empty for a GitHub App whose login is unknown.
func getViewerLogin(connection forgestore.Connection) string {
	if connection.Method == forge.MethodGitHubApp {
		return ""
	}

	return connection.Login
}

// send posts an approved or failed reply to the forge at most once, reading the forge back rather
// than posting again when its answer leaves unknown whether the reply landed.
func (s *ForgeReplyService) send(ctx context.Context, reply *reviewstore.ForgeReply) (*reviewstore.ForgeReply, error) {
	destination, err := s.resolveForgeReply(ctx, reply.ThreadID)
	if err != nil {
		return nil, err
	}

	sending, err := s.reviews.BeginForgeReplySend(ctx, reply.ID)
	if err != nil {
		return nil, err
	}

	posted, err := destination.Client.ReplyToThread(ctx, forge.ReplyToThreadParams{
		Repo:     destination.Remote,
		Number:   destination.Target.Number,
		ThreadID: destination.Target.ProviderID,
		OnCode:   destination.Target.Path != "",
		Body:     sending.Body,
	})
	if err == nil {
		return s.reviews.MarkForgeReplySent(ctx, sending.ID, posted)
	}
	if isRefusal(err) {
		return s.reviews.MarkForgeReplyFailed(ctx, sending.ID, err.Error())
	}

	return s.settle(context.WithoutCancel(ctx), settleParams{
		Destination: *destination,
		Reply:       *sending,
		Cause:       err,
	})
}

// isRefusal says whether the forge turned a reply down without posting it, as opposed to an
// answer that leaves unknown whether the reply landed.
func isRefusal(err error) bool {
	failure := fail.Of(err)

	return failure != nil && slices.Contains(refusalKinds, failure.Kind())
}
