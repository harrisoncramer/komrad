package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

type formRecorder struct {
	agentv1connect.AgentServiceClient
	workflowv1connect.WorkflowServiceClient
	schedulev1connect.ScheduleServiceClient
	hookv1connect.HookServiceClient
	refuse              error
	createdAgent        *agentv1.CreateAgentRequest
	startedOrchestrator *agentv1.StartOrchestratorRequest
	adoptedAgent        *agentv1.AdoptRequest
	renamedAgent        *agentv1.UpdateAgentRequest
	deletedAgents       *agentv1.DeleteAgentsRequest
	archivedAgents      *agentv1.ArchiveAgentsRequest
	createdWorkflow     *workflowv1.CreateWorkflowRequest
	updatedWorkflow     *workflowv1.UpdateWorkflowRequest
	archivedWorkflow    *workflowv1.ArchiveWorkflowRequest
	deletedWorkflow     *workflowv1.DeleteWorkflowRequest
	createdSchedule     *schedulev1.CreateScheduleRequest
	updatedSchedule     *schedulev1.UpdateScheduleRequest
	archivedSchedule    *schedulev1.ArchiveScheduleRequest
}

func (f *formRecorder) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

func (f *formRecorder) ListHooks(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[hookv1.ListHooksResponse], error) {
	return connect.NewResponse(&hookv1.ListHooksResponse{}), nil
}

func (f *formRecorder) CreateAgent(_ context.Context, req *connect.Request[agentv1.CreateAgentRequest]) (*connect.Response[agentv1.CreateAgentResponse], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.createdAgent = req.Msg

	return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{
		Id:         "new",
		Name:       req.Msg.GetTitle(),
		PaneId:     "%new",
		WorkflowId: req.Msg.GetWorkflowId(),
	}}), nil
}

func (f *formRecorder) Adopt(_ context.Context, req *connect.Request[agentv1.AdoptRequest]) (*connect.Response[agentv1.AdoptResponse], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.adoptedAgent = req.Msg

	return connect.NewResponse(&agentv1.AdoptResponse{Agent: &agentv1.Agent{
		Id:         "adopted",
		Name:       "pr-1069",
		PaneId:     "%adopted",
		WorkflowId: req.Msg.GetWorkflowId(),
	}}), nil
}

func (f *formRecorder) UpdateAgent(_ context.Context, req *connect.Request[agentv1.UpdateAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.renamedAgent = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) DeleteAgents(_ context.Context, req *connect.Request[agentv1.DeleteAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.deletedAgents = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ArchiveAgents(_ context.Context, req *connect.Request[agentv1.ArchiveAgentsRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.archivedAgents = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ListWorkflows(context.Context, *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	return connect.NewResponse(&workflowv1.ListWorkflowsResponse{}), nil
}

func (f *formRecorder) CreateWorkflow(_ context.Context, req *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.createdWorkflow = req.Msg

	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "new"}}), nil
}

func (f *formRecorder) StartOrchestrator(
	_ context.Context,
	req *connect.Request[agentv1.StartOrchestratorRequest],
) (*connect.Response[agentv1.StartOrchestratorResponse], error) {
	f.startedOrchestrator = req.Msg

	return connect.NewResponse(&agentv1.StartOrchestratorResponse{}), nil
}

func (f *formRecorder) UpdateWorkflow(_ context.Context, req *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.updatedWorkflow = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ArchiveWorkflow(_ context.Context, req *connect.Request[workflowv1.ArchiveWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.archivedWorkflow = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) DeleteWorkflow(_ context.Context, req *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.deletedWorkflow = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ListSchedules(context.Context, *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	return connect.NewResponse(&schedulev1.ListSchedulesResponse{}), nil
}

func (f *formRecorder) CreateSchedule(_ context.Context, req *connect.Request[schedulev1.CreateScheduleRequest]) (*connect.Response[schedulev1.CreateScheduleResponse], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.createdSchedule = req.Msg

	return connect.NewResponse(&schedulev1.CreateScheduleResponse{Schedule: &schedulev1.Schedule{Id: "new"}}), nil
}

func (f *formRecorder) UpdateSchedule(_ context.Context, req *connect.Request[schedulev1.UpdateScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.updatedSchedule = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ArchiveSchedule(_ context.Context, req *connect.Request[schedulev1.ArchiveScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	if f.refuse != nil {
		return nil, f.refuse
	}

	f.archivedSchedule = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

// TestConfirmingArchiveTracksTheWriteAsPending covers the confirmed archive counting as a write in flight.
func TestConfirmingArchiveTracksTheWriteAsPending(t *testing.T) {
	sent := &formRecorder{}

	opened, _ := newFormModel(sent).confirmArchiveAgents([]panel.Agent{{ID: "a1", Name: "api-refactor"}})
	m := requireModel(t, opened)
	m.form.form.State = huh.StateCompleted

	submitted, cmd := m.form.update(m, keyMsg("enter"))
	got := requireModel(t, submitted)

	assert.Positive(t, got.pending, "the in-flight write is tracked on the model that comes back")
	require.NotNil(t, cmd)
}

// TestSubmittingNoticeCountsUp covers the submitting notice counting once a write is slow.
func TestSubmittingNoticeCountsUp(t *testing.T) {
	since := time.Now()

	tests := []struct {
		name  string
		since time.Time
		now   time.Time
		want  string
	}{
		{
			name:  "a write with no start stands still",
			since: time.Time{},
			now:   since,
			want:  "  submitting…",
		},
		{
			name:  "a quick write stands still",
			since: since,
			now:   since.Add(time.Second),
			want:  "  submitting…",
		},
		{
			name:  "a slow write counts its seconds",
			since: since,
			now:   since.Add(12 * time.Second),
			want:  "  submitting… 12s · esc to keep working",
		},
		{
			name:  "a very slow write counts its minutes",
			since: since,
			now:   since.Add(400 * time.Second),
			want:  "  submitting… 6m40s · esc to keep working",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, submittingNotice(tt.since, tt.now))
		})
	}
}

// TestAHeldFormCarriesItsOwnNotice covers the submitting notice being drawn under the form.
func TestAHeldFormCarriesItsOwnNotice(t *testing.T) {
	tests := []struct {
		name   string
		height int
	}{
		{
			name:   "a short window",
			height: 24,
		},
		{
			name:   "a middling window",
			height: 32,
		},
		{
			name:   "a tall window",
			height: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			height := tt.height
			m := resizeModel(t, newFormColumnModel(t), tea.WindowSizeMsg{Width: 160, Height: height})

			opened, cmd := m.newWorkflowForm()
			m = requireModel(t, opened)
			m = deliverAll(t, m, cmd)

			m.pending, m.held = 1, heldWrite{active: true, since: time.Now()}
			m = deliverAll(t, m, formResized())

			lines := strings.Split(strings.TrimRight(ansi.Strip(m.render()), "\n"), "\n")

			assert.NotContains(t, lines[0], "submitting", "height=%d the panel keeps its top row", height)
			assert.Contains(t, findLastFilled(lines), "submitting…", "height=%d the notice sits under the form", height)
			assert.LessOrEqual(t, paint.CountLines(m.render()), height, "height=%d", height)
		})
	}
}

// TestFormStillsTheCursorOnTheListBesideIt covers the list cursor standing still while a form has the keys.
func TestFormStillsTheCursorOnTheListBesideIt(t *testing.T) {
	m := newFormColumnModel(t)
	m.rowCursor = newRowCursor()
	m.rowCursor.IsBlinked = true
	require.True(t, m.isListFocused(), "the list holds the keys to start with")
	require.Equal(t, blankCursor, m.rowCursorPrefix(), "so its cursor is mid-blink")

	m = openWorkflowForm(t, m)

	assert.False(t, m.isListFocused(), "the form holds them once it is up")
	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.rowCursorPrefix(), "and its cursor stands still")
}

func deliver(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()

	updated, cmd := m.Update(msg)

	return deliverAll(t, requireModel(t, updated), cmd)
}

func deliverAll(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range collectMessages(cmd) {
		m = deliver(t, m, msg)
	}

	return m
}

func requireModel(t *testing.T, updated tea.Model) model {
	t.Helper()

	m, ok := updated.(model)
	require.True(t, ok)

	return m
}

func newFormModel(sent *formRecorder) model {
	tmux := &recordingTmux{}

	return model{
		api: api{
			agents:    sent,
			workflows: sent,
			schedules: sent,
			hooks:     sent,
		},
		tmux:  tmux,
		popup: NewPopupWindow(tmux, "test-session"),
		frame: layout.New(tmux, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{workflows: []panel.Workflow{{
			ID:      "w1",
			Name:    "api",
			Harness: "claude",
			Cwd:     "/work/api",
			Flags:   "--model opus",
		}}},
	}
}

// newFormColumnModel is the agents list on a window wide enough to hold a form beside it.
func newFormColumnModel(t *testing.T) model {
	t.Helper()

	tmux := &recordingTmux{width: 200, height: 40}
	m := infoModel(tmux)
	m.width, m.height = 200, 40
	m.api = api{
		agents:    emptyLists{},
		workflows: emptyLists{},
		schedules: emptyLists{},
		hooks:     &fakeHooks{},
	}

	return m
}

func openWorkflowForm(t *testing.T, m model) model {
	t.Helper()

	opened, _ := m.newWorkflowForm()

	return requireModel(t, opened)
}

// findLastFilled is the bottom row of a view that says anything.
func findLastFilled(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}

	return ""
}

func (m model) openRename(agent panel.Agent) model {
	openRename(&m, agent)

	return m
}

func (m model) openRenameForm(agent panel.Agent, name *string) model {
	openRenameForm(&m, agent, name)

	return m
}

func (m model) confirmArchiveAgents(agents []panel.Agent) (tea.Model, tea.Cmd) {
	cmd := confirmArchiveAgents(&m, m.listing.liveAgents(), agents)

	return m, cmd
}

func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	cmd := newWorkflowForm(&m)

	return m, cmd
}

func (m model) editWorkflowForm(workflow panel.Workflow) (tea.Model, tea.Cmd) {
	cmd := editWorkflowForm(&m, m.shared(), workflow)

	return m, cmd
}

func (m model) openWorkflowRename(workflow panel.Workflow, values *workflowFormValues) model {
	openWorkflowRename(&m, workflow, values)

	return m
}

func (m model) openWorkflowConfigForm(values *workflowFormValues, target *panel.Workflow) model {
	openWorkflowConfigForm(&m, values, target)

	return m
}
