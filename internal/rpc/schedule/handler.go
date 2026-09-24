// Package schedule serves the schedule RPCs, which create, edit, pause and remove the recurring
// launches.
package schedule

import (
	"context"
	"time"

	"weave/internal/fail"
	"weave/internal/rpc/render"
	"weave/internal/rpcerr"
	"weave/internal/schedules"
	"weave/lib/pagination"
	"weave/lib/xslice"
	schedulev1 "weave/proto/gen/schedule/v1"

	schedulestore "weave/internal/store/schedule"

	"google.golang.org/protobuf/types/known/emptypb"
)

const defaultRunLimit = 20

type scheduleReadWriter interface {
	GetSchedule(ctx context.Context, id string) (*schedulestore.Schedule, error)
	NewSchedule(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error)
	UpdateSchedule(ctx context.Context, id string, cfg schedulestore.Config) error
	SetScheduleEnabled(ctx context.Context, id string, enabled bool) error
	ArchiveSchedule(ctx context.Context, id string) error
	ListSchedules(ctx context.Context, params schedules.ListSchedulesParams) ([]schedulestore.Schedule, error)
	ListScheduleRuns(ctx context.Context, params schedules.ListScheduleRunsParams) ([]schedulestore.Run, error)
}

type NewHandlerParams struct {
	Schedules scheduleReadWriter
}

type Handler struct {
	schedules scheduleReadWriter
}

// NewHandler constructs the handler over the service that owns the rules.
func NewHandler(params NewHandlerParams) *Handler {
	return &Handler{schedules: params.Schedules}
}

// ListSchedules returns one page of schedules, newest first.
func (s *Handler) ListSchedules(ctx context.Context, req *schedulev1.ListSchedulesRequest) (*schedulev1.ListSchedulesResponse, *fail.Error) {
	paginator, err := pagination.New[schedulestore.Cursor, schedule](req.GetPagination())
	if err != nil {
		return nil, rpcerr.From(err, "failed to read the pagination")
	}

	page, err := s.schedules.ListSchedules(ctx, schedules.ListSchedulesParams{
		Limit:  paginator.Limit(),
		Cursor: paginator.Cursor(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list schedules")
	}

	now := time.Now()

	view := scheduleViewOptions(req.GetView())
	resources := xslice.Map(page, func(stored schedulestore.Schedule) schedule { return renderSchedule(stored, now, view...) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.From(err, "failed to build the next page token")
	}

	response := &schedulev1.ListSchedulesResponse{NextPageToken: next}
	response.Schedules = xslice.Map(resources, func(resource schedule) *schedulev1.Schedule { return resource.Schedule })

	return response, nil
}

// GetSchedule returns one schedule by id.
func (s *Handler) GetSchedule(ctx context.Context, req *schedulev1.GetScheduleRequest) (*schedulev1.GetScheduleResponse, *fail.Error) {
	found, err := s.schedules.GetSchedule(ctx, req.GetId())
	if err != nil {
		return nil, rpcerr.From(err, "failed to get schedule")
	}

	return &schedulev1.GetScheduleResponse{
		Schedule: renderSchedule(*found, time.Now()).Schedule,
	}, nil
}

// ListScheduleRuns returns a schedule's most recent firings, newest first.
func (s *Handler) ListScheduleRuns(ctx context.Context, req *schedulev1.ListScheduleRunsRequest) (*schedulev1.ListScheduleRunsResponse, *fail.Error) {
	limit := int(req.GetLimit())
	if limit == 0 {
		limit = defaultRunLimit
	}

	runs, err := s.schedules.ListScheduleRuns(ctx, schedules.ListScheduleRunsParams{
		ScheduleID: req.GetId(),
		Limit:      limit,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the schedule's runs")
	}

	return &schedulev1.ListScheduleRunsResponse{Runs: xslice.Map(runs, render.ScheduleRun)}, nil
}

// CreateSchedule records a recurring launch and returns it as stored.
func (s *Handler) CreateSchedule(ctx context.Context, req *schedulev1.CreateScheduleRequest) (*schedulev1.CreateScheduleResponse, *fail.Error) {
	created, err := s.schedules.NewSchedule(ctx, configFrom(req))
	if err != nil {
		return nil, rpcerr.From(err, "failed to create schedule")
	}

	return &schedulev1.CreateScheduleResponse{
		Schedule: renderSchedule(*created, time.Now()).Schedule,
	}, nil
}

// UpdateSchedule replaces a schedule's editable fields.
func (s *Handler) UpdateSchedule(ctx context.Context, req *schedulev1.UpdateScheduleRequest) (*emptypb.Empty, *fail.Error) {
	err := s.schedules.UpdateSchedule(ctx, req.GetId(), schedulestore.Config{
		Name:       req.GetName(),
		Cron:       req.GetCron(),
		Timezone:   req.GetTimezone(),
		Enabled:    req.GetEnabled(),
		HideAgents: req.GetHideAgents(),
		Prompt:     req.GetPrompt(),
		WorkflowID: req.GetWorkflowId(),
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to update schedule")
	}

	return &emptypb.Empty{}, nil
}

// SetScheduleEnabled pauses or resumes a schedule.
func (s *Handler) SetScheduleEnabled(ctx context.Context, req *schedulev1.SetScheduleEnabledRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.schedules.SetScheduleEnabled(ctx, req.GetId(), req.GetEnabled()); err != nil {
		return nil, rpcerr.From(err, "failed to set schedule enabled")
	}

	return &emptypb.Empty{}, nil
}

// ArchiveSchedule stops a schedule from firing again.
func (s *Handler) ArchiveSchedule(ctx context.Context, req *schedulev1.ArchiveScheduleRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.schedules.ArchiveSchedule(ctx, req.GetId()); err != nil {
		return nil, rpcerr.From(err, "failed to archive schedule")
	}

	return &emptypb.Empty{}, nil
}

func configFrom(req *schedulev1.CreateScheduleRequest) schedulestore.Config {
	return schedulestore.Config{
		Name:       req.GetName(),
		Cron:       req.GetCron(),
		Timezone:   req.GetTimezone(),
		Enabled:    req.GetEnabled(),
		HideAgents: req.GetHideAgents(),
		Prompt:     req.GetPrompt(),
		WorkflowID: req.GetWorkflowId(),
	}
}
