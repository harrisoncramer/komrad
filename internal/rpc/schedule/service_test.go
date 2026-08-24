package schedule

import (
	"testing"
	"time"

	"weave/internal/agents"
	"weave/internal/storetest"
	paginationv1 "weave/proto/gen/pagination/v1"
	schedulev1 "weave/proto/gen/schedule/v1"

	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const missingID = "00000000-0000-0000-0000-000000000000"

func TestCreateSchedule(t *testing.T) {
	tests := []struct {
		name     string
		req      *schedulev1.CreateScheduleRequest
		wantCode connect.Code
		check    func(t *testing.T, created *schedulev1.Schedule, workflow string)
	}{
		{
			name: "a schedule is stored as written",
			req: &schedulev1.CreateScheduleRequest{
				Name: "morning-ci", Cron: "0 9 * * 1-5", Prompt: "check CI", Enabled: true,
			},
			check: func(t *testing.T, created *schedulev1.Schedule, _ string) {
				assert.NotEmpty(t, created.GetId())
				assert.Equal(t, "0 9 * * 1-5", created.GetCron())
				assert.Equal(t, "check CI", created.GetPrompt())
				assert.True(t, created.GetEnabled())
			},
		},
		{
			name: "the workflow it launches into is what it stores",
			req: &schedulev1.CreateScheduleRequest{
				Name: "nightly", Cron: "0 2 * * *", Prompt: "sweep", Enabled: true,
			},
			check: func(t *testing.T, created *schedulev1.Schedule, workflow string) {
				assert.Equal(t, workflow, created.GetWorkflowId())
			},
		},
		{
			name: "an armed schedule reports when it next fires",
			req: &schedulev1.CreateScheduleRequest{
				Name: "hourly", Cron: "0 * * * *", Prompt: "poll", Enabled: true,
			},
			check: func(t *testing.T, created *schedulev1.Schedule, _ string) {
				require.NotNil(t, created.GetNextRunAt())
				assert.True(t, created.GetNextRunAt().AsTime().After(time.Now()))
			},
		},
		{
			name: "a paused schedule has no next run to report",
			req: &schedulev1.CreateScheduleRequest{
				Name: "draft", Cron: "0 9 * * *", Prompt: "later",
			},
			check: func(t *testing.T, created *schedulev1.Schedule, _ string) {
				assert.False(t, created.GetEnabled())
				assert.Nil(t, created.GetNextRunAt())
			},
		},
		{
			name: "hidden runs are stored as written",
			req: &schedulev1.CreateScheduleRequest{
				Name: "sweeper", Cron: "*/5 * * * *", Prompt: "sweep", Enabled: true,
				HideAgents: true,
			},
			check: func(t *testing.T, created *schedulev1.Schedule, _ string) {
				assert.True(t, created.GetHideAgents())
			},
		},
		{
			name: "an unparseable expression is refused",
			req: &schedulev1.CreateScheduleRequest{
				Name: "broken", Cron: "99 9 * * *", Prompt: "go",
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{
			name: "an unknown timezone is refused",
			req: &schedulev1.CreateScheduleRequest{
				Name: "elsewhere", Cron: "0 9 * * *", Timezone: "Mars/Olympus", Prompt: "go",
			},
			wantCode: connect.CodeInvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)

			tt.req.WorkflowId = workflow
			created, err := svc.CreateSchedule(t.Context(), connect.NewRequest(tt.req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			tt.check(t, created.Msg.GetSchedule(), workflow)
		})
	}
}

// TestScheduleWorkflowMustExist covers a caller naming a workflow that is not
// there. It is the caller's mistake rather than a server fault, so it comes back
// as not_found, the same code creating an agent into a missing workflow reports.
func TestScheduleWorkflowMustExist(t *testing.T) {
	svc, workflow := service(t)
	existing := createSchedule(t, svc, workflow, "nightly")

	const missing = "00000000-0000-0000-0000-0000000000ff"

	_, err := svc.CreateSchedule(t.Context(), connect.NewRequest(&schedulev1.CreateScheduleRequest{
		Name: "morning", Cron: "0 9 * * *", Prompt: "go", Enabled: true, WorkflowId: missing,
	}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "creating into a workflow that is not there")

	_, err = svc.UpdateSchedule(t.Context(), connect.NewRequest(&schedulev1.UpdateScheduleRequest{
		Id: existing, Name: "nightly", Cron: "0 9 * * *", Prompt: "go", Enabled: true, WorkflowId: missing,
	}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "moving one into it")
}

// TestCreateScheduleNameConflict covers the unique index surfacing as
// already_exists rather than as a server fault.
func TestCreateScheduleNameConflict(t *testing.T) {
	tests := []struct {
		name     string
		second   string
		wantCode connect.Code
	}{
		{
			name:     "a name a live schedule holds",
			second:   "morning-ci",
			wantCode: connect.CodeAlreadyExists,
		},
		{
			name:   "a free name",
			second: "evening-ci",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)
			createSchedule(t, svc, workflow, "morning-ci")

			_, err := svc.CreateSchedule(t.Context(), connect.NewRequest(&schedulev1.CreateScheduleRequest{
				Name: tt.second, Cron: "0 9 * * *", Prompt: "go", WorkflowId: workflow,
			}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestGetSchedule(t *testing.T) {
	tests := []struct {
		name     string
		create   bool
		id       string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			id:       missingID,
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "a deleted schedule is gone from the read side",
			create:   true,
			wantCode: connect.CodeNotFound,
		},
		{
			name:   "a schedule that exists",
			create: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)
			ctx := t.Context()

			id := tt.id
			if tt.create {
				id = createSchedule(t, svc, workflow, "morning-ci")
			}
			if tt.create && tt.wantCode == connect.CodeNotFound {
				_, err := svc.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: id}))
				require.NoError(t, err)
			}

			got, err := svc.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}

			require.NoError(t, err)
			assert.Equal(t, id, got.Msg.GetSchedule().GetId())
			assert.Equal(t, "morning-ci", got.Msg.GetSchedule().GetName())
		})
	}
}

func TestUpdateSchedule(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		req      *schedulev1.UpdateScheduleRequest
		wantCode connect.Code
		wantName string
		wantCron string
	}{
		{
			name:     "an id nobody holds",
			target:   missingID,
			req:      &schedulev1.UpdateScheduleRequest{Name: "renamed", Cron: "0 9 * * *", Prompt: "go"},
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "the editable fields are replaced",
			req:      &schedulev1.UpdateScheduleRequest{Name: "renamed", Cron: "0 17 * * 5", Prompt: "weekly"},
			wantName: "renamed",
			wantCron: "0 17 * * 5",
		},
		{
			name:     "an unparseable expression leaves the record alone",
			req:      &schedulev1.UpdateScheduleRequest{Name: "renamed", Cron: "0 99 * * *", Prompt: "go"},
			wantCode: connect.CodeInvalidArgument,
			wantName: "morning-ci",
			wantCron: "0 9 * * 1-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)
			ctx := t.Context()

			id := createSchedule(t, svc, workflow, "morning-ci")
			target := tt.target
			if target == "" {
				target = id
			}

			req := tt.req
			req.Id = target
			req.WorkflowId = workflow
			_, err := svc.UpdateSchedule(ctx, connect.NewRequest(req))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
			} else {
				require.NoError(t, err)
			}

			if tt.wantName == "" {
				return
			}

			got, err := svc.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, got.Msg.GetSchedule().GetName())
			assert.Equal(t, tt.wantCron, got.Msg.GetSchedule().GetCron())
		})
	}
}

// TestSetScheduleEnabled covers the pause toggle, which is the edit the panel
// makes with one keystroke.
func TestSetScheduleEnabled(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		enabled     bool
		wantCode    connect.Code
		wantEnabled bool
		wantNextRun bool
	}{
		{
			name:     "an id nobody holds",
			target:   missingID,
			wantCode: connect.CodeNotFound,
		},
		{
			name:        "pausing drops the next run",
			enabled:     false,
			wantEnabled: false,
		},
		{
			name:        "resuming brings it back",
			enabled:     true,
			wantEnabled: true,
			wantNextRun: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)
			ctx := t.Context()

			id := createSchedule(t, svc, workflow, "morning-ci")
			target := tt.target
			if target == "" {
				target = id
			}

			_, err := svc.SetScheduleEnabled(ctx, connect.NewRequest(&schedulev1.SetScheduleEnabledRequest{
				Id: target, Enabled: tt.enabled,
			}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}
			require.NoError(t, err)

			got, err := svc.GetSchedule(ctx, connect.NewRequest(&schedulev1.GetScheduleRequest{Id: id}))
			require.NoError(t, err)
			assert.Equal(t, tt.wantEnabled, got.Msg.GetSchedule().GetEnabled())
			assert.Equal(t, tt.wantNextRun, got.Msg.GetSchedule().GetNextRunAt() != nil,
				"only an armed schedule reports a next run")
		})
	}
}

func TestArchiveSchedule(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantCode connect.Code
	}{
		{
			name:     "an id nobody holds",
			target:   missingID,
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a schedule that exists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)
			ctx := t.Context()

			id := createSchedule(t, svc, workflow, "morning-ci")
			target := tt.target
			if target == "" {
				target = id
			}

			_, err := svc.ArchiveSchedule(ctx, connect.NewRequest(&schedulev1.ArchiveScheduleRequest{Id: target}))
			if tt.wantCode != 0 {
				assert.Equal(t, tt.wantCode, connect.CodeOf(err))
				return
			}
			require.NoError(t, err)

			listed, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{}))
			require.NoError(t, err)
			assert.Empty(t, listed.Msg.GetSchedules(), "a deleted schedule leaves the listing")
		})
	}
}

func TestListSchedules(t *testing.T) {
	tests := []struct {
		name     string
		seed     []string
		limit    int32
		wantMore bool
	}{
		{
			name: "an empty store lists nothing",
		},
		{
			name:  "everything fits on one page",
			seed:  []string{"a", "b"},
			limit: 10,
		},
		{
			name:     "a small limit hands back a token",
			seed:     []string{"a", "b", "c"},
			limit:    2,
			wantMore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, workflow := service(t)
			ctx := t.Context()

			for _, name := range tt.seed {
				createSchedule(t, svc, workflow, name)
			}

			req := &schedulev1.ListSchedulesRequest{}
			if tt.limit > 0 {
				req.Pagination = &paginationv1.Pagination{Limit: proto.Int32(tt.limit)}
			}

			first, err := svc.ListSchedules(ctx, connect.NewRequest(req))
			require.NoError(t, err)

			if !tt.wantMore {
				assert.Len(t, first.Msg.GetSchedules(), len(tt.seed))
				assert.Empty(t, first.Msg.GetNextPageToken())
				return
			}

			require.NotEmpty(t, first.Msg.GetNextPageToken())

			second, err := svc.ListSchedules(ctx, connect.NewRequest(&schedulev1.ListSchedulesRequest{
				Pagination: &paginationv1.Pagination{
					Limit:  proto.Int32(tt.limit),
					Cursor: proto.String(first.Msg.GetNextPageToken()),
				},
			}))
			require.NoError(t, err)

			seen := map[string]bool{}
			for _, s := range append(first.Msg.GetSchedules(), second.Msg.GetSchedules()...) {
				assert.False(t, seen[s.GetId()], "a schedule appears on one page only")
				seen[s.GetId()] = true
			}
			assert.Len(t, seen, len(tt.seed))
		})
	}
}

// TestListSchedulesRefusesAMangledToken covers a token that decodes into the right shape
// while holding an id no row can have. The token is the server's own artifact handed back
// verbatim, so one carrying an unusable value is the caller's mistake rather than a
// database failure.
func TestListSchedulesRefusesAMangledToken(t *testing.T) {
	svc, _ := service(t)

	const token = "eyJpIjoibm90LWFuLWlkIn0="

	_, err := svc.ListSchedules(t.Context(), connect.NewRequest(&schedulev1.ListSchedulesRequest{
		Pagination: &paginationv1.Pagination{Cursor: proto.String(token)},
	}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func service(t *testing.T) (*Service, string) {
	t.Helper()

	st := storetest.Open(t)
	manager := agents.NewScheduleService(agents.ScheduleParams{Schedules: st.Schedules, Workflows: st.Workflows})

	return NewService(st.Schedules, manager), st.SeedWorkflow(t, workflowstore.Config{Name: "schedules"}).ID
}

func createSchedule(t *testing.T, svc *Service, workflow, name string) string {
	t.Helper()

	created, err := svc.CreateSchedule(t.Context(), connect.NewRequest(&schedulev1.CreateScheduleRequest{
		Name: name, Cron: "0 9 * * 1-5", Prompt: "check CI", Enabled: true, WorkflowId: workflow,
	}))
	require.NoError(t, err)

	return created.Msg.GetSchedule().GetId()
}
