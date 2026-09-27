package schedule

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"komrad/internal/rpc/render"
	"komrad/internal/schedules"
	"komrad/internal/store"
	paginationv1 "komrad/proto/gen/pagination/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	"komrad/proto/gen/schedule/v1/schedulev1komrad"
	viewv1 "komrad/proto/gen/view/v1"

	schedulestore "komrad/internal/store/schedule"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const scheduleID = "01a0d228-0000-7000-8000-00000000000a"

type updateCall struct {
	id  string
	cfg schedulestore.Config
}

type enableCall struct {
	id      string
	enabled bool
}

// fakeSchedules answers from its fields and records what each call asked of it.
type fakeSchedules struct {
	stored   []schedulestore.Schedule
	runs     []schedulestore.Run
	err      error
	created  []schedulestore.Config
	updated  []updateCall
	enabled  []enableCall
	gone     []string
	restored []string
	deleted  []string
	pages    []schedules.ListSchedulesParams
	listed   []schedules.ListScheduleRunsParams
}

// GetSchedule answers the first stored schedule unless err refuses it.
func (f *fakeSchedules) GetSchedule(context.Context, string) (*schedulestore.Schedule, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.stored[0], nil
}

// NewSchedule records the config and answers it as stored unless err refuses it.
func (f *fakeSchedules) NewSchedule(_ context.Context, cfg schedulestore.Config) (*schedulestore.Schedule, error) {
	f.created = append(f.created, cfg)
	if f.err != nil {
		return nil, f.err
	}

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

// UpdateSchedule records the update and answers err.
func (f *fakeSchedules) UpdateSchedule(_ context.Context, id string, cfg schedulestore.Config) error {
	f.updated = append(f.updated, updateCall{
		id:  id,
		cfg: cfg,
	})

	return f.err
}

// SetScheduleEnabled records the toggle and answers err.
func (f *fakeSchedules) SetScheduleEnabled(_ context.Context, id string, enabled bool) error {
	f.enabled = append(f.enabled, enableCall{
		id:      id,
		enabled: enabled,
	})

	return f.err
}

// ArchiveSchedule records the id and answers err.
func (f *fakeSchedules) ArchiveSchedule(_ context.Context, id string) error {
	f.gone = append(f.gone, id)

	return f.err
}

// RestoreSchedule records the id and answers err.
func (f *fakeSchedules) RestoreSchedule(_ context.Context, id string) error {
	f.restored = append(f.restored, id)

	return f.err
}

// DeleteSchedule records the id and answers err.
func (f *fakeSchedules) DeleteSchedule(_ context.Context, id string) error {
	f.deleted = append(f.deleted, id)

	return f.err
}

// ListSchedules records the listing and answers as many stored schedules as the limit allows
// unless err refuses it.
func (f *fakeSchedules) ListSchedules(_ context.Context, params schedules.ListSchedulesParams) ([]schedulestore.Schedule, error) {
	f.pages = append(f.pages, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.stored[:min(int(params.Limit), len(f.stored))], nil
}

// ListScheduleRuns records the listing and answers runs unless err refuses it.
func (f *fakeSchedules) ListScheduleRuns(_ context.Context, params schedules.ListScheduleRunsParams) ([]schedulestore.Run, error) {
	f.listed = append(f.listed, params)
	if f.err != nil {
		return nil, f.err
	}

	return f.runs, nil
}

// TestScheduleRefusalsMapToCodes pins the code each refusal of the schedule service reaches the caller as.
func TestScheduleRefusalsMapToCodes(t *testing.T) {
	create := func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
		_, err := svc.CreateSchedule(ctx, connect.NewRequest(&schedulev1.CreateScheduleRequest{Name: "morning-ci"}))

		return err
	}
	update := func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
		_, err := svc.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{Id: scheduleID}))

		return err
	}
	restore := func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
		_, err := svc.RestoreSchedule(ctx, connect.NewRequest(&schedulev1.RestoreScheduleRequest{Id: scheduleID}))

		return err
	}

	tests := []struct {
		name     string
		call     func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error
		err      error
		wantCode connect.Code
	}{
		{
			name:     "an expression that cannot be read",
			call:     create,
			err:      schedules.ErrBadCron,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a workflow nothing holds",
			call:     create,
			err:      schedules.ErrWorkflowNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a workflow that has been archived",
			call:     update,
			err:      schedules.ErrWorkflowArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "an orchestrator nothing holds",
			call:     create,
			err:      schedules.ErrOrchestratorNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "an orchestrator that has been archived",
			call:     update,
			err:      schedules.ErrOrchestratorArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "an agent that is not an orchestrator",
			call:     create,
			err:      schedules.ErrNotAnOrchestrator,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a prompt too long to mail",
			call:     create,
			err:      schedules.ErrPromptTooLong,
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name:     "a name a live schedule holds",
			call:     create,
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:     "updating a schedule nothing holds",
			call:     update,
			err:      schedules.ErrScheduleNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "pausing a schedule nothing holds",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{Id: scheduleID}))

				return err
			},
			err:      schedules.ErrScheduleNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "archiving a schedule nothing holds",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: scheduleID}))

				return err
			},
			err:      schedules.ErrScheduleNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "restoring a schedule the archive does not hold",
			call:     restore,
			err:      schedules.ErrScheduleNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "restoring into a workflow that has been archived",
			call:     restore,
			err:      schedules.ErrWorkflowArchived,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "restoring over a name a live schedule holds",
			call:     restore,
			err:      store.ErrNameTaken,
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name: "deleting a schedule the archive does not hold",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.DeleteSchedule(ctx, connect.NewRequest(&schedulev1.DeleteScheduleRequest{Id: scheduleID}))

				return err
			},
			err:      schedules.ErrScheduleNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "reading a schedule nothing holds",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: scheduleID}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "listing the runs of a schedule nothing holds",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ListScheduleRuns(ctx, connect.NewRequest(&schedulev1.ListScheduleRunsRequest{Id: scheduleID}))

				return err
			},
			err:      store.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "listing with a page token that holds no id",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{
					Pagination: &paginationv1.Pagination{Cursor: proto.String("eyJpIjoibm90LWFuLWlkIn0=")},
				}))

				return err
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "a listing the store could not read",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{}))

				return err
			},
			err:      errors.New("connection refused"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newScheduleHandler(&fakeSchedules{err: tt.err})

			err := tt.call(t.Context(), svc)
			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			if tt.err != nil {
				require.ErrorIs(t, err, tt.err)
			}
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
		WorkflowID: "01a0d228-0000-7000-8000-0000000000ff",
		Kind:       schedulestore.KindImplementer,
	}
	orchestratorID := "01a0d228-0000-7000-8000-0000000000aa"
	mailing := schedulestore.Config{
		Name:                cfg.Name,
		Cron:                cfg.Cron,
		Timezone:            cfg.Timezone,
		Enabled:             cfg.Enabled,
		Prompt:              cfg.Prompt,
		Kind:                schedulestore.KindOrchestrator,
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
					Target:     &schedulev1.CreateScheduleRequest_WorkflowId{WorkflowId: cfg.WorkflowID},
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
					Target:   &schedulev1.CreateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: orchestratorID},
				}))

				return err
			},
			want: fakeSchedules{created: []schedulestore.Config{mailing}},
		},
		{
			name: "updating hands down the id and every field",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.UpdateSchedule(ctx, connect.NewRequest(&schedulev1.UpdateScheduleRequest{
					Id:         scheduleID,
					Name:       cfg.Name,
					Cron:       cfg.Cron,
					Timezone:   cfg.Timezone,
					Enabled:    cfg.Enabled,
					HideAgents: cfg.HideAgents,
					Prompt:     cfg.Prompt,
					Target:     &schedulev1.UpdateScheduleRequest_WorkflowId{WorkflowId: cfg.WorkflowID},
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
					Id:       scheduleID,
					Name:     mailing.Name,
					Cron:     mailing.Cron,
					Timezone: mailing.Timezone,
					Enabled:  mailing.Enabled,
					Prompt:   mailing.Prompt,
					Target:   &schedulev1.UpdateScheduleRequest_OrchestratorAgentId{OrchestratorAgentId: orchestratorID},
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
			name: "pausing hands down the id and the toggle",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
					Id:      scheduleID,
					Enabled: true,
				}))

				return err
			},
			want: fakeSchedules{enabled: []enableCall{
				{
					id:      scheduleID,
					enabled: true,
				},
			}},
		},
		{
			name: "archiving hands down the id",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: scheduleID}))

				return err
			},
			want: fakeSchedules{gone: []string{scheduleID}},
		},
		{
			name: "restoring hands down the id",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.RestoreSchedule(ctx, connect.NewRequest(&schedulev1.RestoreScheduleRequest{Id: scheduleID}))

				return err
			},
			want: fakeSchedules{restored: []string{scheduleID}},
		},
		{
			name: "deleting hands down the id",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.DeleteSchedule(ctx, connect.NewRequest(&schedulev1.DeleteScheduleRequest{Id: scheduleID}))

				return err
			},
			want: fakeSchedules{deleted: []string{scheduleID}},
		},
		{
			name: "listing the archive hands down which half to list",
			call: func(ctx context.Context, svc schedulev1komrad.ScheduleServiceAdapter) error {
				_, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{
					Pagination: &paginationv1.Pagination{Limit: proto.Int32(5)},
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

// TestCreateSchedule pins that a created schedule comes back rendered, with its kind and its next run when it is armed.
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
			wantKind: schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER,
		},
		{
			name:        "an armed schedule reports when it next fires",
			enabled:     true,
			wantNextRun: true,
			wantKind:    schedulev1.ScheduleKind_SCHEDULE_KIND_IMPLEMENTER,
		},
		{
			name:             "an orchestrator schedule names the orchestrator it mails",
			orchestrator:     "01a0d228-0000-7000-8000-0000000000aa",
			wantKind:         schedulev1.ScheduleKind_SCHEDULE_KIND_ORCHESTRATOR,
			wantOrchestrator: proto.String("01a0d228-0000-7000-8000-0000000000aa"),
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
			assert.Equal(t, scheduleID, schedule.GetId())
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
				Pagination: &paginationv1.Pagination{Limit: proto.Int32(1)},
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
					ID:     "01a0d228-0000-7000-8000-00000000000b",
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

	got, err := svc.GetSchedule(t.Context(), connect.NewRequest(&schedulev1.GetScheduleRequest{Id: scheduleID}))
	require.NoError(t, err)
	assert.Equal(t, long, got.Msg.GetSchedule().GetPrompt())
	assert.False(t, got.Msg.GetSchedule().GetPromptTruncated())
}

// TestListScheduleRuns pins how many runs a listing asks for and that each comes back rendered.
func TestListScheduleRuns(t *testing.T) {
	run := schedulestore.Run{
		ID:           7,
		ScheduledFor: time.Date(2026, time.July, 28, 9, 0, 0, 0, time.UTC),
		State:        schedulestore.RunStateCancelled,
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
				Id:    scheduleID,
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
