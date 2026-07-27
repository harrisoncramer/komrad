// Package schedule serves ScheduleService over ConnectRPC.
package schedule

import (
	"context"
	"errors"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/lib/pagination"
	schedulev1 "claude-agents/proto/gen/schedule/v1"

	schedulestore "claude-agents/internal/store/schedule"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// scheduleManager is the part of the schedule service the handlers drive. The
// handlers translate and nothing more. What a schedule may be lives behind
// this.
type scheduleManager interface {
	GetSchedule(ctx context.Context, id string) (*schedulestore.Schedule, error)
	NewSchedule(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error)
	UpdateSchedule(ctx context.Context, id string, cfg schedulestore.Config) error
	SetScheduleEnabled(ctx context.Context, id string, enabled bool) error
	DeleteSchedule(ctx context.Context, id string) error
}

// scheduleLister is the read side, kept separate because listing pages straight
// out of the store rather than through the service.
type scheduleLister interface {
	ListPage(ctx context.Context, limit int64, cursor *schedulestore.Cursor) ([]schedulestore.Schedule, error)
}

// Service implements schedulev1connect.ScheduleServiceHandler.
type Service struct {
	lister    scheduleLister
	schedules scheduleManager
}

// NewService constructs the handler from the store it lists through and the
// service that owns the rules.
func NewService(lister scheduleLister, schedules scheduleManager) *Service {
	return &Service{lister: lister, schedules: schedules}
}

// ListSchedules returns one page of schedules, most recently updated first.
func (s *Service) ListSchedules(ctx context.Context, req *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	paginator, err := pagination.New[schedulestore.Cursor, schedule](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	page, err := s.lister.ListPage(ctx, paginator.Limit(), paginator.Cursor())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list schedules: %w", err))
	}

	// One clock for the whole page, so two rows with the same expression report
	// the same next run rather than times a microsecond apart.
	now := time.Now()

	resources := make([]schedule, 0, len(page))
	for _, stored := range page {
		resources = append(resources, renderSchedule(stored, now))
	}

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to build the next page token: %w", err))
	}

	response := &schedulev1.ListSchedulesResponse{NextPageToken: next}
	for _, resource := range resources {
		response.Schedules = append(response.Schedules, resource.Schedule)
	}

	return connect.NewResponse(response), nil
}

// GetSchedule returns one schedule by id.
func (s *Service) GetSchedule(ctx context.Context, req *connect.Request[schedulev1.GetScheduleRequest]) (*connect.Response[schedulev1.GetScheduleResponse], error) {
	found, err := s.schedules.GetSchedule(ctx, req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get schedule: %w", err))
	}
	if found == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no schedule with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&schedulev1.GetScheduleResponse{
		Schedule: renderSchedule(*found, time.Now()).Schedule,
	}), nil
}

// CreateSchedule records a recurring launch and returns it as stored.
func (s *Service) CreateSchedule(ctx context.Context, req *connect.Request[schedulev1.CreateScheduleRequest]) (*connect.Response[schedulev1.CreateScheduleResponse], error) {
	created, err := s.schedules.NewSchedule(ctx, configFrom(req.Msg))
	switch {
	case errors.Is(err, agents.ErrBadCron):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, schedulestore.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("a schedule named %q already exists", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create schedule: %w", err))
	}

	return connect.NewResponse(&schedulev1.CreateScheduleResponse{
		Schedule: renderSchedule(*created, time.Now()).Schedule,
	}), nil
}

// UpdateSchedule replaces a schedule's editable fields.
func (s *Service) UpdateSchedule(ctx context.Context, req *connect.Request[schedulev1.UpdateScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.schedules.UpdateSchedule(ctx, req.Msg.GetId(), schedulestore.Config{
		Name:       req.Msg.GetName(),
		Cron:       req.Msg.GetCron(),
		Timezone:   req.Msg.GetTimezone(),
		Enabled:    req.Msg.GetEnabled(),
		Prompt:     req.Msg.GetPrompt(),
		WorkflowID: req.Msg.GetWorkflowId(),
	})
	switch {
	case errors.Is(err, agents.ErrScheduleNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no schedule with id %q", req.Msg.GetId()))
	case errors.Is(err, agents.ErrBadCron):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, schedulestore.ErrNameTaken):
		return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("a schedule named %q already exists", req.Msg.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to update schedule: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetScheduleEnabled pauses or resumes a schedule.
func (s *Service) SetScheduleEnabled(ctx context.Context, req *connect.Request[schedulev1.SetScheduleEnabledRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.schedules.SetScheduleEnabled(ctx, req.Msg.GetId(), req.Msg.GetEnabled())
	switch {
	case errors.Is(err, agents.ErrScheduleNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no schedule with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to set schedule enabled: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// DeleteSchedule stops a schedule from firing again.
func (s *Service) DeleteSchedule(ctx context.Context, req *connect.Request[schedulev1.DeleteScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	err := s.schedules.DeleteSchedule(ctx, req.Msg.GetId())
	switch {
	case errors.Is(err, agents.ErrScheduleNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no schedule with id %q", req.Msg.GetId()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete schedule: %w", err))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// configFrom reads a create request into the store's config. Update builds its own
// inline, since the two requests are separate messages that happen to carry the
// same fields.
func configFrom(req *schedulev1.CreateScheduleRequest) schedulestore.Config {
	return schedulestore.Config{
		Name:       req.GetName(),
		Cron:       req.GetCron(),
		Timezone:   req.GetTimezone(),
		Enabled:    req.GetEnabled(),
		Prompt:     req.GetPrompt(),
		WorkflowID: req.GetWorkflowId(),
	}
}
