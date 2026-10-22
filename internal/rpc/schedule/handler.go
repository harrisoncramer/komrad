// Package schedule serves the schedule RPCs, which create, edit, pause and remove the recurring
// launches.
package schedule

import (
	"context"
	"time"

	"komrad/internal/fail"
	"komrad/internal/rpc/render"
	"komrad/internal/rpcerr"
	"komrad/internal/rpcpage"
	"komrad/internal/schedules"
	"komrad/lib/xslice"
	"komrad/proto/gen/oneofs"
	schedulev1 "komrad/proto/gen/schedule/v1"
	"komrad/proto/gen/schedule/v1/schedulev1komrad"

	schedulestore "komrad/internal/store/schedule"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"
)

const defaultRunLimit = 20

type scheduleReadWriter interface {
	GetSchedule(ctx context.Context, id uuid.UUID) (*schedulestore.Schedule, error)
	NewSchedule(ctx context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error)
	UpdateSchedule(ctx context.Context, id uuid.UUID, cfg schedulestore.Config) error
	SetScheduleEnabled(ctx context.Context, id uuid.UUID, enabled bool) error
	ArchiveSchedule(ctx context.Context, id uuid.UUID) error
	RestoreSchedule(ctx context.Context, id uuid.UUID) error
	DeleteSchedule(ctx context.Context, id uuid.UUID) error
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
	page, failure := rpcpage.List(req.GetPagination(), func(limit int64, cursor *schedulestore.Cursor) ([]schedule, *fail.Error) {
		listed, err := s.schedules.ListSchedules(ctx, schedules.ListSchedulesParams{
			Limit:    limit,
			Cursor:   cursor,
			Archived: req.GetArchived(),
		})
		if err != nil {
			return nil, rpcerr.From(err, "failed to list schedules")
		}

		now := time.Now()

		view := scheduleViewOptions(req.GetView())

		return xslice.Map(listed, func(stored schedulestore.Schedule) schedule { return renderSchedule(stored, now, view) }), nil
	})
	if failure != nil {
		return nil, failure
	}

	response := &schedulev1.ListSchedulesResponse{NextPageToken: page.Next}
	response.Schedules = xslice.Map(page.Resources, func(resource schedule) *schedulev1.Schedule { return resource.Schedule })

	return response, nil
}

// GetSchedule returns one schedule by id.
func (s *Handler) GetSchedule(ctx context.Context, req *schedulev1komrad.GetScheduleRequest) (*schedulev1.GetScheduleResponse, *fail.Error) {
	found, err := s.schedules.GetSchedule(ctx, req.Id)
	if err != nil {
		return nil, rpcerr.From(err, "failed to get schedule")
	}

	return &schedulev1.GetScheduleResponse{
		Schedule: renderSchedule(*found, time.Now(), render.ScheduleOptions{}).Schedule,
	}, nil
}

// ListScheduleRuns returns a schedule's most recent firings, newest first.
func (s *Handler) ListScheduleRuns(ctx context.Context, req *schedulev1komrad.ListScheduleRunsRequest) (*schedulev1.ListScheduleRunsResponse, *fail.Error) {
	limit := int(req.Limit)
	if limit == 0 {
		limit = defaultRunLimit
	}

	runs, err := s.schedules.ListScheduleRuns(ctx, schedules.ListScheduleRunsParams{
		ScheduleID: req.Id,
		Limit:      limit,
	})
	if err != nil {
		return nil, rpcerr.From(err, "failed to list the schedule's runs")
	}

	return &schedulev1.ListScheduleRunsResponse{Runs: xslice.Map(runs, render.ScheduleRun)}, nil
}

// CreateSchedule records a recurring launch and returns it as stored.
func (s *Handler) CreateSchedule(ctx context.Context, req *schedulev1komrad.CreateScheduleRequest) (*schedulev1.CreateScheduleResponse, *fail.Error) {
	created, err := s.schedules.NewSchedule(ctx, configFrom(req))
	if err != nil {
		return nil, rpcerr.From(err, "failed to create schedule")
	}

	return &schedulev1.CreateScheduleResponse{
		Schedule: renderSchedule(*created, time.Now(), render.ScheduleOptions{}).Schedule,
	}, nil
}

// UpdateSchedule replaces a schedule's editable fields.
func (s *Handler) UpdateSchedule(ctx context.Context, req *schedulev1komrad.UpdateScheduleRequest) (*emptypb.Empty, *fail.Error) {
	cfg := schedulestore.Config{
		Name:       req.Name,
		Cron:       req.Cron,
		Timezone:   req.Timezone,
		Enabled:    req.Enabled,
		HideAgents: req.HideAgents,
		Prompt:     req.Prompt,
	}
	err := s.schedules.UpdateSchedule(ctx, req.Id, withTarget(cfg, req.Target))
	if err != nil {
		return nil, rpcerr.From(err, "failed to update schedule")
	}

	return &emptypb.Empty{}, nil
}

// SetScheduleEnabled pauses or resumes a schedule.
func (s *Handler) SetScheduleEnabled(ctx context.Context, req *schedulev1komrad.SetScheduleEnabledRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.schedules.SetScheduleEnabled(ctx, req.Id, req.Enabled); err != nil {
		return nil, rpcerr.From(err, "failed to set schedule enabled")
	}

	return &emptypb.Empty{}, nil
}

// ArchiveSchedule stops a schedule from firing again.
func (s *Handler) ArchiveSchedule(ctx context.Context, req *schedulev1komrad.ArchiveScheduleRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.schedules.ArchiveSchedule(ctx, req.Id); err != nil {
		return nil, rpcerr.From(err, "failed to archive schedule")
	}

	return &emptypb.Empty{}, nil
}

// RestoreSchedule takes a schedule out of the archive.
func (s *Handler) RestoreSchedule(ctx context.Context, req *schedulev1komrad.RestoreScheduleRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.schedules.RestoreSchedule(ctx, req.Id); err != nil {
		return nil, rpcerr.From(err, "failed to restore schedule")
	}

	return &emptypb.Empty{}, nil
}

// DeleteSchedule removes an archived schedule for good.
func (s *Handler) DeleteSchedule(ctx context.Context, req *schedulev1komrad.DeleteScheduleRequest) (*emptypb.Empty, *fail.Error) {
	if err := s.schedules.DeleteSchedule(ctx, req.Id); err != nil {
		return nil, rpcerr.From(err, "failed to delete schedule")
	}

	return &emptypb.Empty{}, nil
}

func configFrom(req *schedulev1komrad.CreateScheduleRequest) schedulestore.Config {
	cfg := schedulestore.Config{
		Name:       req.Name,
		Cron:       req.Cron,
		Timezone:   req.Timezone,
		Enabled:    req.Enabled,
		HideAgents: req.HideAgents,
		Prompt:     req.Prompt,
	}

	return withTarget(cfg, req.Target)
}

// withTarget sets what a schedule fires at from whichever of the request's targets the caller gave.
func withTarget(cfg schedulestore.Config, target oneofs.Target) schedulestore.Config {
	if target.OrchestratorAgentId != nil {
		cfg.Kind = schedulestore.KindOrchestrator
		cfg.OrchestratorAgentID = target.OrchestratorAgentId

		return cfg
	}

	cfg.Kind = schedulestore.KindTask
	if target.WorkflowId != nil {
		cfg.WorkflowID = *target.WorkflowId
	}

	return cfg
}
