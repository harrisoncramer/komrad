package schedule

import (
	"context"
	"time"

	"weave/internal/agents"
	"weave/internal/rpcerr"
	"weave/internal/store"
	"weave/lib/pagination"
	"weave/lib/xslice"
	schedulev1 "weave/proto/gen/schedule/v1"

	schedulestore "weave/internal/store/schedule"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

type scheduleWriter interface {
	GetSchedule(ctx context.Context, id string) (*schedulestore.Schedule, error)
	NewSchedule(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error)
	UpdateSchedule(ctx context.Context, id string, cfg schedulestore.Config) error
	SetScheduleEnabled(ctx context.Context, id string, enabled bool) error
	ArchiveSchedule(ctx context.Context, id string) error
}

type scheduleLister interface {
	ListPage(ctx context.Context, limit int64, cursor *schedulestore.Cursor) ([]schedulestore.Schedule, error)
}

type Service struct {
	lister    scheduleLister
	schedules scheduleWriter
}

// NewService constructs the handler from the store it lists through and the
// service that owns the rules.
func NewService(lister scheduleLister, schedules scheduleWriter) *Service {
	return &Service{lister: lister, schedules: schedules}
}

// ListSchedules returns one page of schedules, newest first.
func (s *Service) ListSchedules(ctx context.Context, req *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	paginator, err := pagination.New[schedulestore.Cursor, schedule](req.Msg.GetPagination())
	if err != nil {
		return nil, err
	}

	page, err := s.lister.ListPage(ctx, paginator.Limit(), paginator.Cursor())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to list schedules")
	}

	now := time.Now()

	resources := xslice.Map(page, func(stored schedulestore.Schedule) schedule { return renderSchedule(stored, now) })

	next, err := paginator.NextToken(resources)
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to build the next page token")
	}

	response := &schedulev1.ListSchedulesResponse{NextPageToken: next}
	response.Schedules = xslice.Map(resources, func(resource schedule) *schedulev1.Schedule { return resource.Schedule })

	return connect.NewResponse(response), nil
}

// GetSchedule returns one schedule by id.
func (s *Service) GetSchedule(ctx context.Context, req *connect.Request[schedulev1.GetScheduleRequest]) (*connect.Response[schedulev1.GetScheduleResponse], error) {
	found, err := s.schedules.GetSchedule(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to get schedule",
			rpcerr.Saying(store.ErrNotFound, "no schedule with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&schedulev1.GetScheduleResponse{
		Schedule: renderSchedule(*found, time.Now()).Schedule,
	}), nil
}

// CreateSchedule records a recurring launch and returns it as stored.
func (s *Service) CreateSchedule(ctx context.Context, req *connect.Request[schedulev1.CreateScheduleRequest]) (*connect.Response[schedulev1.CreateScheduleResponse], error) {
	created, err := s.schedules.NewSchedule(ctx, configFrom(req.Msg))
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to create schedule",
			rpcerr.Saying(store.ErrNameTaken, "a schedule named %q already exists", req.Msg.GetName()))
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
		HideAgents: req.Msg.GetHideAgents(),
		Prompt:     req.Msg.GetPrompt(),
		WorkflowID: req.Msg.GetWorkflowId(),
	})
	if err != nil {
		return nil, rpcerr.Wrap(err, "failed to update schedule",
			rpcerr.Saying(agents.ErrScheduleNotFound, "no schedule with id %q", req.Msg.GetId()),
			rpcerr.Saying(store.ErrNameTaken, "a schedule named %q already exists", req.Msg.GetName()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// SetScheduleEnabled pauses or resumes a schedule.
func (s *Service) SetScheduleEnabled(ctx context.Context, req *connect.Request[schedulev1.SetScheduleEnabledRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.schedules.SetScheduleEnabled(ctx, req.Msg.GetId(), req.Msg.GetEnabled()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to set schedule enabled",
			rpcerr.Saying(agents.ErrScheduleNotFound, "no schedule with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// ArchiveSchedule stops a schedule from firing again.
func (s *Service) ArchiveSchedule(ctx context.Context, req *connect.Request[schedulev1.ArchiveScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	if err := s.schedules.ArchiveSchedule(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcerr.Wrap(err, "failed to archive schedule",
			rpcerr.Saying(agents.ErrScheduleNotFound, "no schedule with id %q", req.Msg.GetId()))
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
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
