package ui

import (
	"context"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"
	"weave/lib/xslice"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

func scheduleModel(client schedulev1connect.ScheduleServiceClient, keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api:    api{schedules: client},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenSchedules,
		listing: listing{
			workflows: []panel.Workflow{{
				ID:   "w1",
				Name: "api",
				Cwd:  "/work/api",
			}},
			schedules: []scheduleItem{
				{schedule: panel.Schedule{
					ID:      "s1",
					Name:    "morning-ci",
					Cron:    "0 9 * * 1-5",
					Enabled: true,
				}},
			},
		},
	}
}

func scheduleItems(names ...string) []scheduleItem {
	out := xslice.Map(names, func(name string) scheduleItem {
		return scheduleItem{schedule: panel.Schedule{
			ID:      name,
			Name:    name,
			Enabled: true,
		}}
	})

	return out
}

type fakeSchedules struct {
	schedulev1connect.ScheduleServiceClient
	err    error
	writes int
}

func (f *fakeSchedules) ListSchedules(context.Context, *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	return connect.NewResponse(&schedulev1.ListSchedulesResponse{}), nil
}

func (f *fakeSchedules) GetSchedule(_ context.Context, req *connect.Request[schedulev1.GetScheduleRequest]) (*connect.Response[schedulev1.GetScheduleResponse], error) {
	return connect.NewResponse(&schedulev1.GetScheduleResponse{
		Schedule: &schedulev1.Schedule{
			Id:      req.Msg.GetId(),
			Name:    "morning-ci",
			Cron:    "0 9 * * 1-5",
			Enabled: true,
		},
	}), nil
}

func (f *fakeSchedules) CreateSchedule(context.Context, *connect.Request[schedulev1.CreateScheduleRequest]) (*connect.Response[schedulev1.CreateScheduleResponse], error) {
	f.writes++
	if f.err != nil {
		return nil, f.err
	}

	return connect.NewResponse(&schedulev1.CreateScheduleResponse{Schedule: &schedulev1.Schedule{Id: "new"}}), nil
}

func (f *fakeSchedules) UpdateSchedule(context.Context, *connect.Request[schedulev1.UpdateScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	f.writes++
	if f.err != nil {
		return nil, f.err
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *fakeSchedules) SetScheduleEnabled(context.Context, *connect.Request[schedulev1.SetScheduleEnabledRequest]) (*connect.Response[emptypb.Empty], error) {
	f.writes++
	if f.err != nil {
		return nil, f.err
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *fakeSchedules) ArchiveSchedule(context.Context, *connect.Request[schedulev1.ArchiveScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	f.writes++
	if f.err != nil {
		return nil, f.err
	}

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// scheduleHelp is the schedules legend as the frame draws it.
func (m model) scheduleHelp() keyMap {
	return m.listHelpOf(m.schedules.listParams(m.shared()))
}
