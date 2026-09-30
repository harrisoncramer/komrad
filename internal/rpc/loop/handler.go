// Package loop serves the wakes of the service's background loops.
package loop

import (
	"context"

	"komrad/internal/fail"
	loopv1 "komrad/proto/gen/loop/v1"

	"google.golang.org/protobuf/types/known/emptypb"
)

// ErrNotWakeable is a loop the service holds no waker for.
var ErrNotWakeable = fail.InvalidArgument("the loop cannot be woken")

type waker interface {
	Wake()
}

type NewHandlerParams struct {
	CwdSync         waker
	TranscriptSync  waker
	PullRequestSync waker
}

type Handler struct {
	wakers map[loopv1.Loop]waker
}

// NewHandler constructs the handler from the waker of each loop a caller may wake.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{wakers: map[loopv1.Loop]waker{
		loopv1.Loop_LOOP_CWD_SYNC:          params.CwdSync,
		loopv1.Loop_LOOP_TRANSCRIPT_SYNC:   params.TranscriptSync,
		loopv1.Loop_LOOP_PULL_REQUEST_SYNC: params.PullRequestSync,
	}}
}

// WakeLoop asks the named loop to run before its next tick.
func (h *Handler) WakeLoop(_ context.Context, req *loopv1.WakeLoopRequest) (*emptypb.Empty, *fail.Error) {
	loop, ok := h.wakers[req.GetLoop()]
	if !ok {
		return nil, ErrNotWakeable.Saying("%s cannot be woken", req.GetLoop())
	}

	loop.Wake()

	return &emptypb.Empty{}, nil
}
