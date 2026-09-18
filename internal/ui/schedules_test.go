package ui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"weave/internal/ui/paint"

	"weave/config"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	"weave/lib/xptr"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestScheduleKeys pins where each key lands and what it writes, including doing nothing at
// all on an empty list.
func TestScheduleKeys(t *testing.T) {
	keys := config.DefaultKeybindings()

	tests := []struct {
		name         string
		empty        bool
		noWorkflows  bool
		key          string
		wantScreen   screen
		wantForm     formKind
		wantWrites   int
		wantReported bool
	}{
		{
			name:       "back returns to the agents tree",
			key:        "esc",
			wantScreen: screenAgents,
		},
		{
			name:       "new opens the form",
			key:        "c",
			wantScreen: screenSchedules,
			wantForm:   formSchedule,
		},
		{
			name:       "edit opens the form on the hovered schedule",
			key:        "e",
			wantScreen: screenSchedules,
			wantForm:   formSchedule,
		},
		{
			name:       "archive opens the confirm prompt",
			key:        "z",
			wantScreen: screenSchedules,
			wantForm:   formScheduleArchive,
		},
		{
			name:       "toggle writes straight through, with no form in between",
			key:        " ",
			wantScreen: screenSchedules,
			wantWrites: 1,
		},
		{
			name:       "edit on an empty list does nothing",
			empty:      true,
			key:        "e",
			wantScreen: screenSchedules,
		},
		{
			name:       "archive on an empty list does nothing",
			empty:      true,
			key:        "z",
			wantScreen: screenSchedules,
		},
		{
			name:       "toggle on an empty list asks for no write",
			empty:      true,
			key:        " ",
			wantScreen: screenSchedules,
		},
		{
			name:         "new with no workflows reports rather than opening the form",
			noWorkflows:  true,
			key:          "c",
			wantScreen:   screenSchedules,
			wantReported: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSchedules{}
			m := scheduleModel(client, keys)
			if tt.empty {
				m.schedules.items = nil
			}
			if tt.noWorkflows {
				m.listing.workflows = nil
			}

			updated, cmd := m.updateSchedules(keyMsg(tt.key))
			pressed, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, pressed, cmd)

			assert.Equal(t, tt.wantScreen, got.screen)
			assert.Equal(t, tt.wantForm, got.form.kind, "the form that opened, if any")
			assert.Equal(t, tt.wantWrites, client.writes)
			assert.Equal(t, tt.wantReported, len(got.messages.items) > 0, "whether the panel had something to say")
		})
	}
}

// TestSchedulesView pins the row saying which of the two states a schedule is in.
func TestSchedulesView(t *testing.T) {
	tests := []struct {
		name      string
		schedules []scheduleItem
		wants     []string
		notWants  []string
	}{
		{
			name:  "an empty list says so rather than rendering nothing",
			wants: []string{"Schedules", "no schedules yet"},
		},
		{
			name: "a schedule shows its name and its expression",
			schedules: []scheduleItem{
				{schedule: schedulestore.Schedule{
					Name:    "morning-ci",
					Cron:    "0 9 * * 1-5",
					Enabled: true,
				}},
			},
			wants: []string{"morning-ci", "0 9 * * 1-5", scheduleIcon(true)},
		},
		{
			name: "a paused one is marked differently from an armed one",
			schedules: []scheduleItem{
				{schedule: schedulestore.Schedule{Name: "paused", Cron: "0 9 * * *"}},
			},
			wants:    []string{"paused", scheduleIcon(false)},
			notWants: []string{scheduleIcon(true)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
			m.schedules.items = tt.schedules
			m.schedules.cursor = 0

			view := m.schedulesView()
			for _, want := range tt.wants {
				assert.Contains(t, view, want)
			}
			for _, notWant := range tt.notWants {
				assert.NotContains(t, view, notWant)
			}
		})
	}
}

// TestScheduleFormButtonsLineUp pins the confirm buttons left, which huh otherwise centres
// under the field's own prompt.
func TestScheduleFormButtonsLineUp(t *testing.T) {
	form, _ := scheduleForm(&schedulestore.Config{
		Name:       "sweeper",
		Cron:       "*/5 * * * *",
		Prompt:     "sweep",
		Enabled:    true,
		WorkflowID: "w1",
	}, []workflowstore.Workflow{{
		ID:   "w1",
		Name: "api",
		Cwd:  "/work/api",
	}}, config.FixedFormKeys())
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	var indents []int
	for _, line := range strings.Split(plain(form.View()), "\n") {
		buttons := strings.TrimLeft(line, " ")
		if !strings.HasPrefix(buttons, "Armed ") && !strings.HasPrefix(buttons, "Hidden ") {
			continue
		}

		indents = append(indents, len(line)-len(buttons))
	}

	require.Len(t, indents, 2, "one row of buttons per confirm")
	for _, indent := range indents {
		assert.Equal(t, indents[0], indent, "every confirm draws its buttons in the same column")
	}
}

func plain(rendered string) string {
	return colourEscape.ReplaceAllString(rendered, "")
}

var colourEscape = regexp.MustCompile("\x1b\\[[0-9;]*m")

// TestScheduleMarkdown covers the document the stage pages beside the list: when the schedule
// next fires, and the prompt each run is launched with, which is the part of a schedule long
// enough to want a pane of its own.
func TestScheduleMarkdown(t *testing.T) {
	next := time.Date(2026, time.July, 28, 9, 0, 0, 0, time.Local)

	tests := []struct {
		name     string
		item     scheduleItem
		wants    []string
		notWants []string
	}{
		{
			name: "an armed schedule leads with when it next fires",
			item: scheduleItem{
				schedule: schedulestore.Schedule{
					Name:    "morning-ci",
					Enabled: true,
					Prompt:  "check CI",
				},
				nextRun: next,
			},
			wants:    []string{"morning-ci", "next run", "check CI"},
			notWants: []string{"paused"},
		},
		{
			name:     "a paused one says it will not run",
			item:     scheduleItem{schedule: schedulestore.Schedule{Name: "draft", Prompt: "later"}},
			wants:    []string{"draft", "paused"},
			notWants: []string{"next run"},
		},
		{
			name:  "a schedule with no prompt says so rather than paging a blank",
			item:  scheduleItem{schedule: schedulestore.Schedule{Name: "bare", Enabled: true}},
			wants: []string{"bare", "no prompt set"},
		},
		{
			name: "one whose runs start hidden says where they will not appear",
			item: scheduleItem{schedule: schedulestore.Schedule{
				Name:       "quiet",
				Enabled:    true,
				HideAgents: true,
			}},
			wants: []string{"quiet", "start hidden"},
		},
		{
			name: "a schedule that has never fired says nothing about a result",
			item: scheduleItem{
				schedule: schedulestore.Schedule{
					Name:    "fresh",
					Enabled: true,
					Prompt:  "go",
				},
			},
			notWants: []string{"last result"},
		},
		{
			name: "a status without a time does not render rather than guess one",
			item: scheduleItem{
				schedule: schedulestore.Schedule{
					Name:             "odd",
					Enabled:          true,
					Prompt:           "go",
					LastResultStatus: schedulestore.LaunchResultLaunched,
				},
			},
			notWants: []string{"last result"},
		},
		{
			name: "a schedule that failed to launch shows what happened and why",
			item: scheduleItem{
				schedule: schedulestore.Schedule{
					Name:              "flaky",
					Enabled:           true,
					Prompt:            "go",
					LastResultStatus:  schedulestore.LaunchResultCannotLaunch,
					LastResultMessage: "bad directory",
					LastResultAt:      &next,
				},
			},
			wants: []string{"last result", "cannot_launch", "bad directory"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := scheduleMarkdown(tt.item)

			for _, want := range tt.wants {
				assert.Contains(t, rendered, want)
			}
			for _, notWant := range tt.notWants {
				assert.NotContains(t, rendered, notWant)
			}
		})
	}
}

// TestScheduleFields covers what the details under the list draw for a schedule: its own
// settings first, then the defaults of the workflow it fires into, which is what each run is
// launched with.
func TestScheduleFields(t *testing.T) {
	sched := schedulestore.Schedule{
		Name:     "morning-ci",
		Cron:     "0 9 * * 1-5",
		Timezone: "America/New_York",
		Enabled:  true,
	}

	t.Run("its own settings, then what the workflow hands each run", func(t *testing.T) {
		fields := scheduleFields(sched, &workflowstore.Workflow{
			Name:    "api",
			Harness: "codex",
			Cwd:     "/work/api",
			Flags:   "--model opus",
		})

		assert.Equal(t, []recordField{
			{"name", "morning-ci"},
			{"cron", "0 9 * * 1-5"},
			{"timezone", "America/New_York"},
			{"workflow", "api"},
			{"default agent", "codex"},
			{"directory", "/work/api"},
			{"flags", "--model opus"},
			{"worktree", "off"},
		}, fields)
	})

	t.Run("a workflow with no defaults falls back to dashes", func(t *testing.T) {
		fields := scheduleFields(sched, &workflowstore.Workflow{Name: "api"})

		assert.Contains(t, fields, recordField{"directory", "—"})
		assert.Contains(t, fields, recordField{"worktree", "off"})
	})

	t.Run("a schedule whose workflow was archived says so rather than showing a blank", func(t *testing.T) {
		fields := scheduleFields(sched, nil)

		assert.Contains(t, fields, recordField{"workflow", "— (archived)"})
		assert.NotContains(t, fields, recordField{"directory", "—"}, "an archived workflow hands out no defaults")
	})
}

// TestApplySchedules pins the cursor following the schedule it was on.
func TestApplySchedules(t *testing.T) {
	tests := []struct {
		name       string
		start      []scheduleItem
		cursor     int
		fresh      []scheduleItem
		wantCursor int
	}{
		{
			name:       "the cursor follows its schedule when the order changes",
			start:      scheduleItems("a", "b", "c"),
			cursor:     2,
			fresh:      scheduleItems("c", "a", "b"),
			wantCursor: 0,
		},
		{
			name:       "a vanished schedule leaves the cursor clamped in the list",
			start:      scheduleItems("a", "b", "c"),
			cursor:     2,
			fresh:      scheduleItems("a", "b"),
			wantCursor: 1,
		},
		{
			name:       "an emptied list leaves the cursor at zero",
			start:      scheduleItems("a"),
			cursor:     0,
			wantCursor: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
			m.schedules.items = tt.start
			m.schedules.cursor = tt.cursor

			m.applySchedules(tt.fresh)

			assert.Equal(t, tt.wantCursor, m.schedules.cursor)
			assert.Len(t, m.schedules.items, len(tt.fresh))
		})
	}
}

// TestScheduleWriteFailure pins a rejected cron expression reaching the screen as a banner.
func TestScheduleWriteFailure(t *testing.T) {
	tests := []struct {
		name    string
		act     func(t *testing.T, m model) model
		wantErr string
	}{
		{
			name: "a refused pause",
			act: func(t *testing.T, m model) model {
				t.Helper()
				updated, cmd := m.toggleSchedule()
				pressed, ok := updated.(model)
				require.True(t, ok)

				return settle(t, pressed, cmd)
			},
			wantErr: "Could not pause schedule",
		},
		{
			name: "a refused create",
			act: func(t *testing.T, m model) model {
				t.Helper()
				values := schedulestore.Config{Name: "x", Cron: "0 9 * * *"}

				opened, _ := m.openScheduleForm(&values, nil)
				m = asModel(t, opened)

				updated, cmd := m.answerForm()
				submitted, ok := updated.(model)
				require.True(t, ok)

				return settle(t, submitted, cmd)
			},
			wantErr: "Could not create the schedule",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSchedules{err: errors.New("cron must be five fields")}
			m := scheduleModel(client, config.DefaultKeybindings())

			got := tt.act(t, m)

			require.NotEmpty(t, got.messages.items, "a refused write has to be reported")
			assert.Contains(t, newestMessage(got).summary+" "+newestMessage(got).detail, tt.wantErr)
		})
	}
}

// TestEnterSchedules covers reaching the screen from the agents list, which is
// the only way in.
func TestEnterSchedules(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		wantScreen screen
	}{
		{
			name:       "the schedules key opens the screen",
			key:        "S",
			wantScreen: screenSchedules,
		},
		{
			name:       "another key leaves the agents list where it is",
			key:        "i",
			wantScreen: screenAgents,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
			m.screen = screenAgents

			updated, _ := m.updateAgents(keyMsg(tt.key))
			got, ok := updated.(model)
			require.True(t, ok)

			assert.Equal(t, tt.wantScreen, got.screen)
		})
	}
}

func scheduleModel(client schedulev1connect.ScheduleServiceClient, keys config.Keybindings) model {
	tmux := &recordingTmux{}

	return model{
		api:  api{schedules: client},
		tmux: tmux,
		listing: listing{workflows: []workflowstore.Workflow{{
			ID:   "w1",
			Name: "api",
			Cwd:  "/work/api",
		}}},
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenSchedules,
		schedules: scheduleState{
			items: []scheduleItem{
				{schedule: schedulestore.Schedule{
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
	out := make([]scheduleItem, 0, len(names))
	for _, name := range names {
		out = append(out, scheduleItem{schedule: schedulestore.Schedule{
			ID:      name,
			Name:    name,
			Enabled: true,
		}})
	}

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

func keyMsg(key string) tea.KeyMsg {
	if key == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	if named, ok := namedKeys[key]; ok {
		return tea.KeyMsg{Type: named}
	}

	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
}

var namedKeys = map[string]tea.KeyType{
	"esc":    tea.KeyEsc,
	"ctrl+t": tea.KeyCtrlT,
}

// TestScheduledMark pins the clock beside a scheduled agent's row and nowhere else.
func TestScheduledMark(t *testing.T) {
	tests := []struct {
		name  string
		agent agentstore.Agent
		want  bool
	}{
		{
			name:  "an agent a schedule launched is marked",
			agent: agentstore.Agent{Name: "nightly-sweep", ScheduleID: xptr.Ptr("s1")},
			want:  true,
		},
		{
			name:  "one a person started is not",
			agent: agentstore.Agent{Name: "by-hand"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, strings.Contains(scheduledMark(paint.Panel, tt.agent), scheduledIcon))
		})
	}
}

// TestScheduleName covers matching the schedule an agent came from against the list
// the panel polls, which is what the record in the stage names.
func TestScheduleName(t *testing.T) {
	tests := []struct {
		name  string
		agent agentstore.Agent
		want  string
	}{
		{
			name:  "a schedule still listed maps to its name",
			agent: agentstore.Agent{ScheduleID: xptr.Ptr("s1")},
			want:  "morning-ci",
		},
		{
			name:  "a schedule since deleted maps to nothing",
			agent: agentstore.Agent{ScheduleID: xptr.Ptr("gone")},
		},
		{
			name:  "an agent with no schedule maps to nothing",
			agent: agentstore.Agent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())

			assert.Equal(t, tt.want, m.scheduleName(tt.agent))
		})
	}
}

// TestScheduleFormTakesNewWorkflows covers a workflow created while the schedule form is up,
// which the form has to offer without being closed and opened again.
func TestScheduleFormTakesNewWorkflows(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	m.schedules.items = []scheduleItem{{schedule: schedulestore.Schedule{
		ID:         "s1",
		Name:       "morning-ci",
		Cron:       "0 9 * * 1-5",
		Enabled:    true,
		WorkflowID: "w1",
	}}}
	m.width, m.height = 120, 40

	opened, _ := m.editScheduleForm()
	edit, ok := opened.(model)
	require.True(t, ok)
	require.True(t, edit.form.isActive())
	edit.form.form.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	require.NotContains(t, plain(edit.form.form.View()), "brandnew")

	settle(t, edit, edit.form.form.NextField())
	require.Equal(t, "cron", edit.form.form.GetFocusedField().GetKey())

	cmd := edit.applyRefresh(refreshResult{workflows: []workflowstore.Workflow{
		{
			ID:   "w1",
			Name: "api",
			Cwd:  "/work/api",
		},
		{
			ID:   "w2",
			Name: "brandnew",
			Cwd:  "/work/new",
		},
	}})
	settle(t, edit, cmd)
	edit.form.form.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	assert.Contains(t, plain(edit.form.form.View()), "brandnew")
	assert.Equal(t, "cron", edit.form.form.GetFocusedField().GetKey(), "the cursor stays where it was")
}

// TestScheduleFormKeepsAWorkflowThatIsGone covers the workflow a schedule is bound to being
// archived while its form is up, which huh would otherwise answer by picking another one.
func TestScheduleFormKeepsAWorkflowThatIsGone(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	m.width, m.height = 120, 40

	values := schedulestore.Config{
		Name:       "morning-ci",
		Cron:       "0 9 * * 1-5",
		Enabled:    true,
		WorkflowID: "w1",
	}

	opened, _ := m.openScheduleForm(&values, &schedulestore.Schedule{ID: "s1", Name: "morning-ci"})
	edit, ok := opened.(model)
	require.True(t, ok)
	edit.form.form.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	cmd := edit.applyRefresh(refreshResult{workflows: []workflowstore.Workflow{
		{
			ID:   "w2",
			Name: "other",
			Cwd:  "/work/other",
		},
	}})
	settle(t, edit, cmd)
	edit.form.form.Update(tea.WindowSizeMsg{Width: 80, Height: 40})

	assert.Equal(t, "w1", values.WorkflowID, "the schedule stays on the workflow it was written against")
	assert.Contains(t, plain(edit.form.form.View()), "w1  gone")
}

// TestScheduleFormOffersEveryWorkflow covers the options a schedule on any workflow but the
// first is offered.
func TestScheduleFormOffersEveryWorkflow(t *testing.T) {
	values := schedulestore.Config{
		Name:       "bughunt-fixer-dispatcher",
		Cron:       "*/5 * * * *",
		Prompt:     "sweep",
		Enabled:    true,
		WorkflowID: "w2",
	}

	form, _ := scheduleForm(&values, []workflowstore.Workflow{
		{ID: "w1", Name: "bughunt", Cwd: "/repos/weave"},
		{ID: "w2", Name: "weave", Cwd: "/repos/weave"},
	}, config.FixedFormKeys())
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 80, Height: 60})

	view := plain(form.View())
	assert.Contains(t, view, "bughunt  /repos/weave")
	assert.Contains(t, view, "weave  /repos/weave")
	assert.Equal(t, "w2", values.WorkflowID, "the schedule stays on the workflow it named")
}
