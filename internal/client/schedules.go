package client

import (
	"context"
	"fmt"

	paginationv1 "claude-agents/proto/gen/pagination/v1"
	schedulev1 "claude-agents/proto/gen/schedule/v1"
	"claude-agents/proto/gen/schedule/v1/schedulev1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

// ListAllSchedules returns every schedule, following the page tokens the same
// way ListAllAgents does.
func ListAllSchedules(ctx context.Context, schedules schedulev1connect.ScheduleServiceClient) ([]*schedulev1.Schedule, error) {
	var (
		all    []*schedulev1.Schedule
		cursor string
	)

	for range maxPages {
		page := &paginationv1.Pagination{Limit: proto.Int32(pageSize)}
		if cursor != "" {
			page.Cursor = proto.String(cursor)
		}

		resp, err := schedules.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{Pagination: page}))
		if err != nil {
			return nil, fmt.Errorf("failed to list schedules: %w", err)
		}

		all = append(all, resp.Msg.GetSchedules()...)

		cursor = resp.Msg.GetNextPageToken()
		if cursor == "" {
			return all, nil
		}
	}

	return all, nil
}
