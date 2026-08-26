package agent

import (
	"context"
	"errors"
	"time"

	"weave/internal/agents"
	"weave/internal/caller"
	reviewstore "weave/internal/store/review"
	agentv1 "weave/proto/gen/agent/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// errAgentCaller is what an agent is told when it reaches for the reviewer's half of the
// conversation. Opening a thread, rewriting the feedback, deleting it, or ringing the doorbell
// are the person's to do; the agent's half is reading, replying and resolving.
var errAgentCaller = errors.New("only the person reviewing can do that")

// ListReviewThreads reads the review pinned to an agent's diff. An agent may look at another
// agent's review, but claiming it is refused: the service checks that, since a claim is what
// decides whether the owning agent is told about the activity again.
func (s *Service) ListReviewThreads(
	ctx context.Context,
	req *connect.Request[agentv1.ListReviewThreadsRequest],
) (*connect.Response[agentv1.ListReviewThreadsResponse], error) {
	author := reviewAuthorFrom(ctx)

	agentID := req.Msg.GetAgentId()
	if agentID == "" && author.AgentID != nil {
		agentID = *author.AgentID
	}
	if agentID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("name the agent whose review you are reading"))
	}

	threads, err := s.review.ListReviewThreads(ctx, agents.ListReviewThreadsParams{
		AgentID:  agentID,
		Reader:   author,
		MarkRead: req.Msg.GetMarkRead(),
	})
	if err != nil {
		return nil, reviewError(err)
	}

	answered := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		answered = append(answered, toReviewThread(thread))
	}

	return connect.NewResponse(&agentv1.ListReviewThreadsResponse{Threads: answered}), nil
}

// CreateReviewThread pins a conversation to a place in a diff and writes its first message.
// Opening a thread is the reviewer's, so an agent cannot leave notes on its own work.
func (s *Service) CreateReviewThread(
	ctx context.Context,
	req *connect.Request[agentv1.CreateReviewThreadRequest],
) (*connect.Response[agentv1.CreateReviewThreadResponse], error) {
	author, err := requireHuman(ctx)
	if err != nil {
		return nil, err
	}

	thread, err := s.review.CreateReviewThread(ctx, agents.CreateReviewThreadParams{
		AgentID:     req.Msg.GetAgentId(),
		FilePath:    req.Msg.GetFilePath(),
		OldStart:    req.Msg.GetOldStart(),
		OldEnd:      req.Msg.GetOldEnd(),
		NewStart:    req.Msg.GetNewStart(),
		NewEnd:      req.Msg.GetNewEnd(),
		QuotedLines: req.Msg.GetQuotedLines(),
		Body:        req.Msg.GetBody(),
		Author:      author,
	})
	if err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{Thread: toReviewThread(*thread)}), nil
}

// AddReviewMessage says something in an existing thread, as whoever is calling. An agent may
// only speak in the conversation about its own diff.
func (s *Service) AddReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.AddReviewMessageRequest],
) (*connect.Response[agentv1.AddReviewMessageResponse], error) {
	message, err := s.review.AddReviewMessage(ctx, agents.AddReviewMessageParams{
		ThreadID: req.Msg.GetThreadId(),
		Body:     req.Msg.GetBody(),
		Author:   reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&agentv1.AddReviewMessageResponse{Message: toReviewMessage(*message)}), nil
}

// EditReviewMessage rewrites what was said. The service refuses a caller that did not write
// the message, so neither side can rewrite the other's words.
func (s *Service) EditReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.EditReviewMessageRequest],
) (*connect.Response[agentv1.EditReviewMessageResponse], error) {
	message, err := s.review.EditReviewMessage(ctx, agents.EditReviewMessageParams{
		ID:     req.Msg.GetId(),
		Body:   req.Msg.GetBody(),
		Author: reviewAuthorFrom(ctx),
	})
	if err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&agentv1.EditReviewMessageResponse{Message: toReviewMessage(*message)}), nil
}

// MarkReviewThreadRead is the person reviewing acknowledging one conversation, which is what
// opening it means. An agent claims its review by listing with markRead instead.
func (s *Service) MarkReviewThreadRead(
	ctx context.Context,
	req *connect.Request[agentv1.MarkReviewThreadReadRequest],
) (*connect.Response[agentv1.MarkReviewThreadReadResponse], error) {
	if _, err := requireHuman(ctx); err != nil {
		return nil, err
	}

	thread, err := s.review.MarkReviewThreadRead(ctx, req.Msg.GetId())
	if err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&agentv1.MarkReviewThreadReadResponse{
		Thread: toReviewThread(reviewstore.ThreadWithMessages{Thread: *thread}),
	}), nil
}

// ResolveReviewThread settles a thread or reopens it.
func (s *Service) ResolveReviewThread(
	ctx context.Context,
	req *connect.Request[agentv1.ResolveReviewThreadRequest],
) (*connect.Response[agentv1.ResolveReviewThreadResponse], error) {
	thread, err := s.review.ResolveReviewThread(ctx, req.Msg.GetId(), req.Msg.GetResolved(), reviewAuthorFrom(ctx))
	if err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&agentv1.ResolveReviewThreadResponse{
		Thread: toReviewThread(reviewstore.ThreadWithMessages{Thread: *thread}),
	}), nil
}

// DeleteReviewThread removes a thread and everything said in it, which is the reviewer's to do
// with their own notes.
func (s *Service) DeleteReviewThread(
	ctx context.Context,
	req *connect.Request[agentv1.DeleteReviewThreadRequest],
) (*connect.Response[emptypb.Empty], error) {
	if _, err := requireHuman(ctx); err != nil {
		return nil, err
	}

	if err := s.review.DeleteReviewThread(ctx, req.Msg.GetId()); err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteReviewMessage removes one message, leaving the thread it was in. The service refuses a
// caller that did not write it.
func (s *Service) DeleteReviewMessage(
	ctx context.Context,
	req *connect.Request[agentv1.DeleteReviewMessageRequest],
) (*connect.Response[emptypb.Empty], error) {
	if err := s.review.DeleteReviewMessage(ctx, req.Msg.GetId(), reviewAuthorFrom(ctx)); err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// NotifyReviewThreads tells an agent its diff has been reviewed, and answers what that
// achieved rather than assuming it landed.
func (s *Service) NotifyReviewThreads(
	ctx context.Context,
	req *connect.Request[agentv1.NotifyReviewThreadsRequest],
) (*connect.Response[agentv1.NotifyReviewThreadsResponse], error) {
	if _, err := requireHuman(ctx); err != nil {
		return nil, err
	}

	notified, err := s.review.NotifyReviewThreads(ctx, agents.NotifyReviewThreadsParams{
		AgentID:   req.Msg.GetAgentId(),
		Interrupt: req.Msg.GetInterrupt(),
	})
	if err != nil {
		return nil, reviewError(err)
	}

	return connect.NewResponse(&agentv1.NotifyReviewThreadsResponse{
		Unread:      int32(notified.Unread),
		Rang:        notified.Rang,
		Interrupted: notified.Interrupted,
		Waiting:     notified.Waiting,
	}), nil
}

// reviewAuthorFrom decides who a write belongs to from the identity on the call rather than
// from anything the caller asked for. The panel names no agent and writes as the person; an
// agent names itself and writes as itself, so it cannot post as the reviewer.
func reviewAuthorFrom(ctx context.Context) reviewstore.Author {
	from := caller.From(ctx)
	if from.AgentID == "" {
		return reviewstore.Author{Kind: reviewstore.ReviewAuthorHuman}
	}

	agentID := from.AgentID

	return reviewstore.Author{Kind: reviewstore.ReviewAuthorAgent, AgentID: &agentID}
}

// requireHuman refuses the calls that are the reviewer's alone. An agent header the panel
// never sets is what tells the two apart, so this is the check that keeps an agent from
// leaving itself feedback or ringing its own doorbell.
func requireHuman(ctx context.Context) (reviewstore.Author, error) {
	author := reviewAuthorFrom(ctx)
	if author.Kind != reviewstore.ReviewAuthorHuman {
		return author, connect.NewError(connect.CodePermissionDenied, errAgentCaller)
	}

	return author, nil
}

func reviewError(err error) error {
	switch {
	case errors.Is(err, agents.ErrReviewThreadNotFound), errors.Is(err, agents.ErrReviewMessageNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, agents.ErrReviewNotOwner), errors.Is(err, reviewstore.ErrNotAuthor):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, agents.ErrAgentNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}

func toReviewThread(thread reviewstore.ThreadWithMessages) *agentv1.ReviewThread {
	answered := &agentv1.ReviewThread{
		Id:            thread.Thread.ID,
		AgentId:       thread.Thread.AgentID,
		FilePath:      thread.Thread.FilePath,
		OldStart:      thread.Thread.OldStart,
		OldEnd:        thread.Thread.OldEnd,
		NewStart:      thread.Thread.NewStart,
		NewEnd:        thread.Thread.NewEnd,
		QuotedLines:   thread.Thread.QuotedLines,
		UnreadByAgent: thread.Thread.UnreadByAgent,
		UnreadByHuman: thread.Thread.UnreadByHuman,
		CreatedAt:     thread.Thread.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     thread.Thread.UpdatedAt.Format(time.RFC3339),
	}

	if thread.Thread.ResolvedAt != nil {
		resolved := thread.Thread.ResolvedAt.Format(time.RFC3339)
		answered.ResolvedAt = &resolved
	}

	for _, message := range thread.Messages {
		answered.Messages = append(answered.Messages, toReviewMessage(message))
	}

	return answered
}

func toReviewMessage(message reviewstore.ReviewMessage) *agentv1.ReviewMessage {
	kind := agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
	if message.AuthorKind == reviewstore.ReviewAuthorAgent {
		kind = agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT
	}

	return &agentv1.ReviewMessage{
		Id:            message.ID,
		ThreadId:      message.ThreadID,
		AuthorKind:    kind,
		AuthorAgentId: message.AuthorAgentID,
		Body:          message.Body,
		CreatedAt:     message.CreatedAt.Format(time.RFC3339),
		UpdatedAt:     message.UpdatedAt.Format(time.RFC3339),
	}
}
