package main

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"weave/cmd/paneldemo/scenario"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
)

// reviewFake is the review half of the fake service, which holds what the panel writes so a
// comment typed into the demo is there when the sidebar draws again.
type reviewFake struct {
	agentv1connect.UnimplementedAgentServiceHandler
	mu      sync.Mutex
	rounds  []*agentv1.ReviewRound
	reviews []*agentv1.Review
	threads []*agentv1.ReviewThread
	viewed  []*agentv1.ReviewViewedFile
	// written counts the comments typed into the demo, which names the ones it makes up.
	written int
}

func newReviewFake(held scenario.Scenario) *reviewFake {
	return &reviewFake{
		rounds:  held.Rounds,
		reviews: held.Reviews,
		threads: held.Threads,
		viewed:  held.Viewed,
	}
}

func (r *reviewFake) ListReviewRounds(
	_ context.Context,
	req *connect.Request[agentv1.ListReviewRoundsRequest],
) (*connect.Response[agentv1.ListReviewRoundsResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return connect.NewResponse(&agentv1.ListReviewRoundsResponse{
		Rounds: roundsOf(r.rounds, req.Msg.GetAgentId()),
	}), nil
}

// OpenReviewRound answers with the newest round rather than taking a snapshot, since the demo's
// worktree is not one an agent is writing in.
func (r *reviewFake) OpenReviewRound(
	_ context.Context,
	req *connect.Request[agentv1.OpenReviewRoundRequest],
) (*connect.Response[agentv1.OpenReviewRoundResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	held := roundsOf(r.rounds, req.Msg.GetAgentId())
	if len(held) == 0 {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no round for agent %q", req.Msg.GetAgentId()))
	}

	return connect.NewResponse(&agentv1.OpenReviewRoundResponse{Round: held[len(held)-1]}), nil
}

func (r *reviewFake) ListReviews(
	_ context.Context,
	req *connect.Request[agentv1.ListReviewsRequest],
) (*connect.Response[agentv1.ListReviewsResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var found []*agentv1.Review

	for _, review := range r.reviews {
		if review.GetAgentId() == req.Msg.GetAgentId() {
			found = append(found, review)
		}
	}

	return connect.NewResponse(&agentv1.ListReviewsResponse{Reviews: found}), nil
}

func (r *reviewFake) ListReviewThreads(
	_ context.Context,
	req *connect.Request[agentv1.ListReviewThreadsRequest],
) (*connect.Response[agentv1.ListReviewThreadsResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var found []*agentv1.ReviewThread

	for _, thread := range r.threads {
		if thread.GetAgentId() == req.Msg.GetAgentId() {
			found = append(found, thread)
		}
	}

	return connect.NewResponse(&agentv1.ListReviewThreadsResponse{Threads: found}), nil
}

func (r *reviewFake) ListReviewViewedFiles(
	_ context.Context,
	req *connect.Request[agentv1.ListReviewViewedFilesRequest],
) (*connect.Response[agentv1.ListReviewViewedFilesResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var found []*agentv1.ReviewViewedFile

	for _, mark := range r.viewed {
		if mark.GetRoundId() == req.Msg.GetRoundId() {
			found = append(found, mark)
		}
	}

	return connect.NewResponse(&agentv1.ListReviewViewedFilesResponse{Viewed: found}), nil
}

// CreateReviewThread opens a thread the way the real service does, so a comment typed into the
// demo is pinned where it was written and drawn on the next tick.
func (r *reviewFake) CreateReviewThread(
	_ context.Context,
	req *connect.Request[agentv1.CreateReviewThreadRequest],
) (*connect.Response[agentv1.CreateReviewThreadResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.written++

	round := r.latestRound(req.Msg.GetAgentId())

	thread := &agentv1.ReviewThread{
		Id:           fmt.Sprintf("thread-typed-%d", r.written),
		AgentId:      req.Msg.GetAgentId(),
		ReviewId:     "batch-open",
		RoundId:      round.GetId(),
		RoundOrdinal: round.GetOrdinal(),
		BaseSha:      req.Msg.GetBaseSha(),
		FilePath:     req.Msg.GetFilePath(),
		OldStart:     req.Msg.GetOldStart(),
		OldEnd:       req.Msg.GetOldEnd(),
		NewStart:     req.Msg.GetNewStart(),
		NewEnd:       req.Msg.GetNewEnd(),
		QuotedLines:  req.Msg.GetQuotedLines(),
		CreatedAt:    now(),
		UpdatedAt:    now(),
		Messages: []*agentv1.ReviewMessage{{
			Id:         fmt.Sprintf("message-typed-%d", r.written),
			ThreadId:   fmt.Sprintf("thread-typed-%d", r.written),
			AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
			Body:       req.Msg.GetBody(),
			Suggestion: req.Msg.GetSuggestion(),
			Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
			CreatedAt:  now(),
		}},
	}

	r.threads = append(r.threads, thread)
	r.openBatch(req.Msg.GetAgentId(), round)

	return connect.NewResponse(&agentv1.CreateReviewThreadResponse{Thread: thread}), nil
}

// AddReviewMessage says something into a thread already there.
func (r *reviewFake) AddReviewMessage(
	_ context.Context,
	req *connect.Request[agentv1.AddReviewMessageRequest],
) (*connect.Response[agentv1.AddReviewMessageResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.written++

	thread := r.threadByID(req.Msg.GetThreadId())
	if thread == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no thread %q", req.Msg.GetThreadId()))
	}

	message := &agentv1.ReviewMessage{
		Id:         fmt.Sprintf("message-typed-%d", r.written),
		ThreadId:   thread.GetId(),
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       req.Msg.GetBody(),
		Suggestion: req.Msg.GetSuggestion(),
		Delivery:   agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
		CreatedAt:  now(),
	}

	thread.Messages = append(thread.Messages, message)
	thread.UpdatedAt = now()
	r.openBatch(thread.GetAgentId(), r.roundByID(thread.GetRoundId()))

	return connect.NewResponse(&agentv1.AddReviewMessageResponse{Message: message}), nil
}

// ReleaseReview sends the batch, which in the demo means marking every comment in it sent.
func (r *reviewFake) ReleaseReview(
	_ context.Context,
	req *connect.Request[agentv1.ReleaseReviewRequest],
) (*connect.Response[agentv1.ReleaseReviewResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, review := range r.reviews {
		if review.GetAgentId() != req.Msg.GetAgentId() || review.GetReleasedAt() != "" {
			continue
		}

		review.ReleasedAt = now()
		review.Verdict = req.Msg.GetVerdict()
		review.Body = req.Msg.GetBody()
		review.Path = "/tmp/weave-demo-review.md"
	}

	for _, thread := range r.threads {
		if thread.GetAgentId() != req.Msg.GetAgentId() {
			continue
		}

		for _, message := range thread.GetMessages() {
			if message.GetDelivery() == agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT {
				message.Delivery = agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT
				message.SentAt = now()
			}
		}
	}

	return connect.NewResponse(&agentv1.ReleaseReviewResponse{Path: "/tmp/weave-demo-review.md"}), nil
}

func (r *reviewFake) SetReviewThreadResolved(
	_ context.Context,
	req *connect.Request[agentv1.SetReviewThreadResolvedRequest],
) (*connect.Response[agentv1.SetReviewThreadResolvedResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	thread := r.threadByID(req.Msg.GetId())
	if thread == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no thread %q", req.Msg.GetId()))
	}

	thread.ResolvedAt = ""
	if req.Msg.GetResolved() {
		thread.ResolvedAt = now()
	}

	return connect.NewResponse(&agentv1.SetReviewThreadResolvedResponse{Thread: thread}), nil
}

func (r *reviewFake) MarkReviewThreadRead(
	_ context.Context,
	req *connect.Request[agentv1.MarkReviewThreadReadRequest],
) (*connect.Response[agentv1.MarkReviewThreadReadResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	thread := r.threadByID(req.Msg.GetId())
	if thread == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no thread %q", req.Msg.GetId()))
	}

	thread.UnreadByHuman = false

	return connect.NewResponse(&agentv1.MarkReviewThreadReadResponse{Thread: thread}), nil
}

func (r *reviewFake) SetReviewFileViewed(
	_ context.Context,
	req *connect.Request[agentv1.SetReviewFileViewedRequest],
) (*connect.Response[agentv1.SetReviewFileViewedResponse], error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	at := slices.IndexFunc(r.viewed, func(mark *agentv1.ReviewViewedFile) bool {
		return mark.GetRoundId() == req.Msg.GetRoundId() && mark.GetFilePath() == req.Msg.GetFilePath()
	})

	if !req.Msg.GetViewed() {
		if at >= 0 {
			r.viewed = slices.Delete(r.viewed, at, at+1)
		}

		return connect.NewResponse(&agentv1.SetReviewFileViewedResponse{}), nil
	}

	if at < 0 {
		r.viewed = append(r.viewed, &agentv1.ReviewViewedFile{
			RoundId:  req.Msg.GetRoundId(),
			FilePath: req.Msg.GetFilePath(),
		})
	}

	return connect.NewResponse(&agentv1.SetReviewFileViewedResponse{}), nil
}

// openBatch makes sure an unreleased review of the person's exists, which is what holds the
// comments they have written but not sent.
func (r *reviewFake) openBatch(agentID string, round *agentv1.ReviewRound) {
	for _, review := range r.reviews {
		if review.GetAgentId() == agentID &&
			review.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN &&
			review.GetReleasedAt() == "" {
			return
		}
	}

	r.reviews = append(r.reviews, &agentv1.Review{
		Id: "batch-open", AgentId: agentID, RoundId: round.GetId(), RoundOrdinal: round.GetOrdinal(),
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN, CreatedAt: now(),
	})
}

func (r *reviewFake) threadByID(id string) *agentv1.ReviewThread {
	for _, thread := range r.threads {
		if thread.GetId() == id {
			return thread
		}
	}

	return nil
}

func (r *reviewFake) roundByID(id string) *agentv1.ReviewRound {
	for _, round := range r.rounds {
		if round.GetId() == id {
			return round
		}
	}

	return nil
}

func (r *reviewFake) latestRound(agentID string) *agentv1.ReviewRound {
	held := roundsOf(r.rounds, agentID)
	if len(held) == 0 {
		return nil
	}

	return held[len(held)-1]
}

func roundsOf(rounds []*agentv1.ReviewRound, agentID string) []*agentv1.ReviewRound {
	var found []*agentv1.ReviewRound

	for _, round := range rounds {
		if round.GetAgentId() == agentID {
			found = append(found, round)
		}
	}

	return found
}

func now() string {
	return time.Now().Format(time.RFC3339)
}
