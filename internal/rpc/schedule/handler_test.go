package schedule

import (
	"context"
	"strings"
	"testing"
	"time"

	"komrad/internal/rpc/render"
	"komrad/internal/schedules"
	schedulestore "komrad/internal/store/schedule"
	"komrad/lib/xptr"
	paginationv1 "komrad/proto/gen/pagination/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	"komrad/proto/gen/schedule/v1/schedulev1komrad"
	viewv1 "komrad/proto/gen/view/v1"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

var scheduleID = uuid.MustParse("01a0d228-0000-7000-8000-00000000000a")

type updateCall struct {
	id  uuid.UUID
	cfg schedulestore.Config
}

// fakeSchedules answers from its fields and records what each call asked of it.
type fakeSchedules struct {
	stored  []schedulestore.Schedule
	runs    []schedulestore.Run
	created []schedulestore.Config
	updated []updateCall
	pages   []schedules.ListSchedulesParams
	listed  []schedules.ListScheduleRunsParams
}

// GetSchedule answers the first stored schedule.
func (f *fakeSchedules) GetSchedule(context.Context, uuid.UUID) (*schedulestore.Schedule, error) {
	return &f.stored[0], nil
}

// NewSchedule records the config and answers it as stored.
func (f *fakeSchedules) NewSchedule(_ context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error) {
	f.created = append(f.created, cfg)

	return &schedulestore.Schedule{
		ID:                  scheduleID,
		Name:                cfg.Name,
		Cron:                cfg.Cron,
		Enabled:             cfg.Enabled,
		Prompt:              cfg.Prompt,
		WorkflowID:          cfg.WorkflowID,
		Kind:                cfg.Kind,
		OrchestratorAgentID: cfg.OrchestratorAgentID,
	}, nil
}

// UpdateSchedule records the update.
func (f *fakeSchedules) UpdateSchedule(_ context.Context, id uuid.UUID, cfg schedulestore.Config) error {
	f.updated = append(f.updated, updateCall{
		id:  id,
		cfg: cfg,
	})

	return nil
}

// SetScheduleEnabled answers nil.
func (f *fakeSchedules) SetScheduleEnabled(context.Context, uuid.UUID, bool) error {
	return nil
}

// ArchiveSchedule answers nil.
func (f *fakeSchedules) ArchiveSchedule(context.Context, uuid.UUID) error {
	return nil
}

// RestoreSchedule answers nil.
func (f *fakeSchedules) RestoreSchedule(context.Context, uuid.UUID) error {
	return nil
}

// DeleteSchedule answers nil.
func (f *fakeSchedules) DeleteSchedule(context.Context, uuid.UUID) error {
	return nil
}

// ListSchedules records the listing and answers as many stored schedules as the limit allows.
func (f *fakeSchedules) ListSchedules(_ context.Context, params schedules.ListSchedulesParams) ([]schedulestore.Schedule, error) {
	f.pages = append(f.pages, params)

	return f.stored[:min(int(params.Limit), len(f.stored))], nil
}

// ListScheduleRuns records the listing and answers runs.
func (f *fakeSchedules) ListScheduleRuns(_ context.Context, params schedules.ListScheduleRunsParams) ([]schedulestore.Run, error) {
	f.listed = append(f.listed, params)

	return f.runs, nil
}

// TestScheduleRefusalsMapToCodes pins the code each refusal of the schedule service reaches the caller as.
func TestScheduleRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		call     func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error
		wantCode connect.Code
	}{
		{
			name: "listing with a page token that holds no id",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{
					Pagination: &paginationv1.Pagination{Cursor: xptr.Ptr("eyJpIjoibm90LWFuLWlkIn0=")},
				}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newScheduleHandler(&fakeSchedules{})

			err := tt.call(t.Context(), svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

// TestScheduleWritesPassDown pins what each call hands the schedule service.
func TestScheduleWritesPassDown(t *testing.T) {
	cfg := schedulestore.Config{
		Name:       "morning-ci",
		Cron:       "0 9 * * 1-5",
		Timezone:   "America/New_York",
		Enabled:    true,
		HideAgents: true,
		Prompt:     "check CI",
		WorkflowID: uuid.MustParse("01a0d228-0000-7000-8000-0000000000ff"),
		Kind:       schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
	}
	orchestratorID := uuid.MustParse("01a0d228-0000-7000-8000-0000000000aa")
	mailing := schedulestore.Config{
		Name:                cfg.Name,
		Cron:                cfg.Cron,
		Timezone:            cfg.Timezone,
		Enabled:             cfg.Enabled,
		Prompt:              cfg.Prompt,
		Kind:                schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR,
		OrchestratorAgentID: &orchestratorID,
	}

	tests := []struct {
		name string
		call func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error
		want fakeSchedules
	}{
		{
			name: "creating hands down every field",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
					Name:       cfg.Name,
					Cron:       cfg.Cron,
					Timezone:   cfg.Timezone,
					Enabled:    cfg.Enabled,
					HideAgents: cfg.HideAgents,
					Prompt:     cfg.Prompt,
					Target:     &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: cfg.WorkflowID.String()},
				}))

				return err
			},
			want: fakeSchedules{created: []schedulestore.Config{cfg}},
		},
		{
			name: "creating with an orchestrator makes an orchestrator schedule",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{
					Name:     mailing.Name,
					Cron:     mailing.Cron,
					Timezone: mailing.Timezone,
					Enabled:  mailing.Enabled,
					Prompt:   mailing.Prompt,
					Target:   &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: orchestratorID.String()},
				}))

				return err
			},
			want: fakeSchedules{created: []schedulestore.Config{mailing}},
		},
		{
			name: "updating hands down the id and every field",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
					Id:         scheduleID.String(),
					Name:       cfg.Name,
					Cron:       cfg.Cron,
					Timezone:   cfg.Timezone,
					Enabled:    cfg.Enabled,
					HideAgents: cfg.HideAgents,
					Prompt:     cfg.Prompt,
					Target:     &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: cfg.WorkflowID.String()},
				}))

				return err
			},
			want: fakeSchedules{updated: []updateCall{
				{
					id:  scheduleID,
					cfg: cfg,
				},
			}},
		},
		{
			name: "updating with an orchestrator makes it an orchestrator schedule",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
					Id:       scheduleID.String(),
					Name:     mailing.Name,
					Cron:     mailing.Cron,
					Timezone: mailing.Timezone,
					Enabled:  mailing.Enabled,
					Prompt:   mailing.Prompt,
					Target:   &schedulev1.UpdateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: orchestratorID.String()},
				}))

				return err
			},
			want: fakeSchedules{updated: []updateCall{
				{
					id:  scheduleID,
					cfg: mailing,
				},
			}},
		},
		{
			name: "listing the archive hands down which half to list",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{
					Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](5)},
					Archived:   true,
				}))

				return err
			},
			want: fakeSchedules{pages: []schedules.ListSchedulesParams{{Limit: 5, Archived: true}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := &fakeSchedules{}

			require.NoError(t, tt.call(t.Context(), newScheduleHandler(recorded)))
			assert.Equal(t, tt.want, *recorded)
		})
	}
}

// TestCreateSchedule pins that a new schedule comes back rendered, with its kind and its next run when it is armed.
func TestCreateSchedule(t *testing.T) {
	tests := []struct {
		name             string
		enabled          bool
		orchestrator     string
		wantNextRun      bool
		wantKind         schedulev1.ScheduleKind
		wantOrchestrator *string
	}{
		{
			name:     "a paused schedule has no next run to report",
			wantKind: schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
		},
		{
			name:        "an armed schedule reports when it next fires",
			enabled:     true,
			wantNextRun: true,
			wantKind:    schedulev1.ScheduleKind_SCHEDULE_KIND_TASK,
		},
		{
			name:             "an orchestrator schedule names the orchestrator it mails",
			orchestrator:     "01a0d228-0000-7000-8000-0000000000aa",
			wantKind:         schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR,
			wantOrchestrator: xptr.Ptr("01a0d228-0000-7000-8000-0000000000aa"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newScheduleHandler(&fakeSchedules{})

			req := &schedulev1.CreateScheduleRequest{
				Name:    "morning-ci",
				Cron:    "0 9 * * 1-5",
				Enabled: tt.enabled,
				Prompt:  "check CI",
			}
			if tt.orchestrator != "" {
				req.Target = &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: tt.orchestrator}
			}
			created, err := svc.CreateSchedule(t.Context(), connect.NewRequest(req))
			require.NoError(t, err)

			schedule := created.Msg.GetSchedule()
			assert.Equal(t, scheduleID.String(), schedule.GetId())
			assert.Equal(t, tt.wantKind, schedule.GetKind())
			assert.Equal(t, tt.wantOrchestrator, schedule.OrchestratorAgentId)
			assert.Equal(t, "check CI", schedule.GetPrompt())
			assert.Equal(t, tt.wantNextRun, schedule.GetNextRunAt() != nil)
			if tt.wantNextRun {
				assert.True(t, schedule.GetNextRunAt().AsTime().After(time.Now()))
			}
		})
	}
}

// TestListSchedules pins which view draws the whole prompt and when a page hands back a token for the next.
func TestListSchedules(t *testing.T) {
	long := strings.Repeat("p", render.SummaryHead+50)

	tests := []struct {
		name          string
		req           *schedulev1.ListSchedulesRequest
		wantPrompts   []string
		wantTruncated []bool
		wantNext      bool
	}{
		{
			name:          "the default view draws the head of a long prompt",
			req:           &schedulev1.ListSchedulesRequest{},
			wantPrompts:   []string{long[:render.SummaryHead], "check CI"},
			wantTruncated: []bool{true, false},
		},
		{
			name: "the full view draws the whole of it",
			req: &schedulev1.ListSchedulesRequest{
				View: viewv1.View_VIEW_FULL,
			},
			wantPrompts:   []string{long, "check CI"},
			wantTruncated: []bool{false, false},
		},
		{
			name: "a full page hands back a token for the next",
			req: &schedulev1.ListSchedulesRequest{
				Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](1)},
			},
			wantPrompts:   []string{long[:render.SummaryHead]},
			wantTruncated: []bool{true},
			wantNext:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newScheduleHandler(&fakeSchedules{stored: []schedulestore.Schedule{
				{
					ID:     scheduleID,
					Name:   "nightly",
					Prompt: long,
				},
				{
					ID:     uuid.MustParse("01a0d228-0000-7000-8000-00000000000b"),
					Name:   "hourly",
					Prompt: "check CI",
				},
			}})

			listed, err := svc.ListSchedules(t.Context(), connect.NewRequest(tt.req))
			require.NoError(t, err)

			prompts := make([]string, 0, len(listed.Msg.GetSchedules()))
			truncated := make([]bool, 0, len(listed.Msg.GetSchedules()))
			for _, schedule := range listed.Msg.GetSchedules() {
				prompts = append(prompts, schedule.GetPrompt())
				truncated = append(truncated, schedule.GetPromptTruncated())
			}
			assert.Equal(t, tt.wantPrompts, prompts)
			assert.Equal(t, tt.wantTruncated, truncated)
			assert.Equal(t, tt.wantNext, listed.Msg.GetNextPageToken() != "")
		})
	}
}

// TestGetScheduleKeepsTheWholePrompt pins that reading one schedule is never narrowed to a summary.
func TestGetScheduleKeepsTheWholePrompt(t *testing.T) {
	long := strings.Repeat("p", render.SummaryHead+50)
	svc := newScheduleHandler(&fakeSchedules{stored: []schedulestore.Schedule{
		{
			ID:     scheduleID,
			Name:   "nightly",
			Prompt: long,
		},
	}})

	got, err := svc.GetSchedule(t.Context(), connect.NewRequest(&schedulev1.GetScheduleRequest{Id: scheduleID.String()}))
	require.NoError(t, err)
	assert.Equal(t, long, got.Msg.GetSchedule().GetPrompt())
	assert.False(t, got.Msg.GetSchedule().GetPromptTruncated())
}

// TestListScheduleRuns pins how many runs a listing asks for and that each comes back rendered.
func TestListScheduleRuns(t *testing.T) {
	run := schedulestore.Run{
		ID:           7,
		ScheduledFor: time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC),
		State:        schedulev1.ScheduleRunState_SCHEDULE_RUN_STATE_CANCELLED,
		Attempt:      1,
		MaxAttempts:  5,
		Message:      "schedule is paused",
	}

	tests := []struct {
		name      string
		limit     int32
		wantLimit int
	}{
		{
			name:      "an unset limit asks for twenty",
			wantLimit: 20,
		},
		{
			name:      "a limit is handed down as given",
			limit:     2,
			wantLimit: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorded := &fakeSchedules{
				stored: []schedulestore.Schedule{{ID: scheduleID}},
				runs:   []schedulestore.Run{run},
			}
			svc := newScheduleHandler(recorded)

			listed, err := svc.ListScheduleRuns(t.Context(), connect.NewRequest(&schedulev1.ListScheduleRunsRequest{
				Id:    scheduleID.String(),
				Limit: tt.limit,
			}))
			require.NoError(t, err)
			assert.Equal(t, []schedules.ListScheduleRunsParams{
				{
					ScheduleID: scheduleID,
					Limit:      tt.wantLimit,
				},
			}, recorded.listed)
			require.Len(t, listed.Msg.GetRuns(), 1)
			assert.True(t, proto.Equal(render.ScheduleRun(run), listed.Msg.GetRuns()[0]))
		})
	}
}

func newScheduleHandler(fake *fakeSchedules) schedulev1komrad.ScheduleServiceAdapter {
	return schedulev1komrad.ScheduleServiceAdapter{Impl: NewHandler(NewHandlerParams{Schedules: fake})}
}
