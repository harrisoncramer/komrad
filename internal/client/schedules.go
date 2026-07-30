package client

import (
	"context"

	paginationv1 "weave/proto/gen/pagination/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"

	"connectrpc.com/connect"
)

// ListAllSchedules returns every schedule, following the page tokens until the
// listing is exhausted.
func ListAllSchedules(ctx context.Context, schedules schedulev1connect.ScheduleServiceClient) ([]*schedulev1.Schedule, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*schedulev1.Schedule, string, error) {
		resp, err := schedules.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{Pagination: page}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetSchedules(), resp.Msg.GetNextPageToken(), nil
	})
}
