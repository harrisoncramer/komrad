package agent

import (
	"context"

	"komrad/internal/fail"
	"komrad/internal/rpcerr"

	screenstore "komrad/internal/store/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type screenReader interface {
	GetLastScreen(ctx context.Context, agentID string) (*screenstore.Screen, error)
}

// GetLastScreen returns the last screen an agent's pane was saved showing.
func (s *Handler) GetLastScreen(ctx context.Context, req *agentv1.GetLastScreenRequest) (*agentv1.GetLastScreenResponse, *fail.Error) {
	screen, err := s.screens.GetLastScreen(ctx, req.GetAgentId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get the last screen")
	}

	return &agentv1.GetLastScreenResponse{
		Frame:      screen.Frame,
		Width:      int32(screen.Width),
		Height:     int32(screen.Height),
		CapturedAt: timestamppb.New(screen.CapturedAt),
	}, nil
}
