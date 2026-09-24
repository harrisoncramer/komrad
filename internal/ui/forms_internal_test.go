package ui

import (
	"context"
	"errors"
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

// TestNewAgentSubmitHoldsTheForm covers the new-agent form staying up until its create lands.
func TestNewAgentSubmitHoldsTheForm(t *testing.T) {
	tests := []struct {
		name       string
		refuse     error
		wantDetail string
	}{
		{name: "a create that lands"},
		{
			name:       "a setup that fails",
			refuse:     connect.NewError(connect.CodeFailedPrecondition, errors.New("the provisioner failed: exit 7")),
			wantDetail: "the provisioner failed: exit 7",
		},
		{
			name:       "a create refused before the provisioner ever ran",
			refuse:     connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed")),
			wantDetail: "not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &recordingTmux{}
			sent := &formRecorder{refuse: tt.refuse}
			m := formModel(sent)
			m.tmux = recorder
			m.frame = layout.New(recorder, config.DefaultLayout())
			m.frame.Attach("%0")
			m.listing.workflows[0].Worktree = panel.WorktreeConfig{Enabled: true}

			workflow := m.listing.workflows[0]

			values := agentPrefill(workflow)
			values.Prompt = "check CI"

			opened := m.openAgentForm(workflow, &values)
			m = asModel(t, opened)

			updated, cmd := m.answerForm()
			m = asModel(t, updated)

			assert.True(t, m.form.isActive(), "the form is still up while the create is in flight")
			assert.True(t, m.held.active)
			assert.False(t, m.held.since.IsZero(), "and the notice over it has something to count from")
			assert.Equal(t, infoNothing, m.info.kind, "no pane is opened for the provisioner")
			assert.Empty(t, recorder.infoPane)

			got := settle(t, m, cmd)

			if tt.refuse != nil {
				require.NotEmpty(t, got.messages.items, "a refused create is reported")
				assert.Equal(t, tt.wantDetail, newestMessage(got).detail)
				assert.Equal(t, formAgent, got.form.kind, "and the form it came from is still up")

				return
			}

			assert.Empty(t, got.messages.items)
			assert.False(t, got.form.isActive(), "the form goes once the create has landed")
			assert.False(t, got.held.active)
			assert.Equal(t, infoNothing, got.info.kind)
			require.NotNil(t, sent.createdAgent)
		})
	}
}

// TestConfirmingArchiveTracksTheWriteAsPending covers the confirmed archive counting as a write in flight.
func TestConfirmingArchiveTracksTheWriteAsPending(t *testing.T) {
	sent := &formRecorder{}

	opened, _ := formModel(sent).archiveAgentsConfirm([]panel.Agent{{ID: "a1", Name: "api-refactor"}})
	m := asModel(t, opened)
	m.form.form.State = huh.StateCompleted

	submitted, cmd := m.form.update(m, keyMsg("enter"))
	got := asModel(t, submitted)

	assert.Positive(t, got.pending, "the in-flight write is tracked on the model that comes back")
	require.NotNil(t, cmd)
}

func deliver(t *testing.T, m model, msg tea.Msg) model {
	t.Helper()

	updated, cmd := m.Update(msg)

	return delivered(t, asModel(t, updated), cmd)
}

func delivered(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()

	for _, msg := range produced(cmd) {
		m = deliver(t, m, msg)
	}

	return m
}

func asModel(t *testing.T, updated tea.Model) model {
	t.Helper()

	m, ok := updated.(model)
	require.True(t, ok)

	return m
}

func formModel(sent *formRecorder) model {
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

// TestFormTakesNoKeysWhileItsWriteIsInFlight covers a second submit while the first write is in flight.
func TestFormTakesNoKeysWhileItsWriteIsInFlight(t *testing.T) {
	sent := &formRecorder{}

	m := formModel(sent)
	workflow := m.listing.workflows[0]

	values := agentPrefill(workflow)
	values.Prompt = "check CI"

	opened := m.openAgentForm(workflow, &values)
	m = asModel(t, opened)
	m.held.active = true

	updated, cmd := m.Update(keyMsg("ctrl+s"))
	got := settle(t, asModel(t, updated), cmd)

	assert.Nil(t, sent.createdAgent, "the key never reached the submit path")
	assert.Equal(t, formAgent, got.form.kind, "the form is still up")
	assert.Equal(t, "check CI", values.Prompt, "and the form is as it was")
}

// TestEscDetachesFromAHeldForm covers esc handing the panel back during a create that still lands.
func TestEscDetachesFromAHeldForm(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)

	workflow := m.listing.workflows[0]

	values := agentPrefill(workflow)
	values.Prompt = "check CI"

	opened := m.openAgentForm(workflow, &values)
	m = asModel(t, opened)

	updated, cmd := m.answerForm()
	m = asModel(t, updated)
	require.True(t, m.held.active)

	detached, _ := m.Update(keyMsg("esc"))
	m = asModel(t, detached)

	assert.False(t, m.held.active, "the panel takes keys again")
	assert.False(t, m.form.isActive(), "and the form it was holding is gone")

	got := settle(t, m, cmd)

	assert.Empty(t, got.messages.items)
	require.NotNil(t, sent.createdAgent, "the create the form was submitted for still ran")
	_, held := got.listing.liveAgent("new")
	assert.False(t, held, "and it lands without taking the cursor to the agent it made")
}

// TestAnEarlierWriteLeavesALaterHoldAlone covers an escaped create landing while a later form is held.
func TestAnEarlierWriteLeavesALaterHoldAlone(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)

	workflow := m.listing.workflows[0]

	first := agentPrefill(workflow)
	first.Prompt = "the slow one"

	opened := m.openAgentForm(workflow, &first)
	m = asModel(t, opened)

	submitted, _ := m.answerForm()
	m = asModel(t, submitted)

	escaped := m.writes

	detached, _ := m.Update(keyMsg("esc"))
	m = asModel(t, detached)

	second := agentPrefill(workflow)
	second.Prompt = "the second one"

	opened = m.openAgentForm(workflow, &second)
	m = asModel(t, opened)

	submitted, _ = m.answerForm()
	m = asModel(t, submitted)

	require.True(t, m.held.active)
	require.NotEqual(t, escaped, m.held.write, "the two submits are told apart by their own numbers")

	landed, _ := m.applyWrite(writeResult{
		summary: "Could not start the agent",
		written: &written{row: agentRowKey("new")},
		write:   escaped,
	})
	got := asModel(t, landed)

	assert.True(t, got.held.active, "the escaped create landing leaves the second form held")
	assert.True(t, got.form.isActive())
	assert.Empty(t, got.landing.row, "and an escaped create takes nobody to the agent it made")
}

// TestSubmittingNoticeCountsUp covers the submitting notice counting once a write is slow.
func TestSubmittingNoticeCountsUp(t *testing.T) {
	since := time.Now()

	assert.Equal(t, "  submitting…", submittingNotice(time.Time{}, since))
	assert.Equal(t, "  submitting…", submittingNotice(since, since.Add(time.Second)))
	assert.Equal(t, "  submitting… 12s · esc to keep working", submittingNotice(since, since.Add(12*time.Second)))
	assert.Equal(t, "  submitting… 6m40s · esc to keep working", submittingNotice(since, since.Add(400*time.Second)))
}

// formColumnModel is the agents list on a window wide enough to hold a form beside it.
func formColumnModel(t *testing.T) model {
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

	return asModel(t, opened)
}

// TestAHeldFormCarriesItsOwnNotice covers the submitting notice being drawn under the form.
func TestAHeldFormCarriesItsOwnNotice(t *testing.T) {
	for _, height := range []int{24, 32, 40} {
		m := resizedTo(t, formColumnModel(t), 160, height)

		opened, cmd := m.newWorkflowForm()
		m = asModel(t, opened)
		m = delivered(t, m, cmd)

		m.pending, m.held = 1, heldWrite{active: true, since: time.Now()}
		m = delivered(t, m, formResized())

		lines := strings.Split(strings.TrimRight(ansi.Strip(m.render()), "\n"), "\n")

		assert.NotContains(t, lines[0], "submitting", "height=%d the panel keeps its top row", height)
		assert.Contains(t, lastFilled(lines), "submitting…", "height=%d the notice sits under the form", height)
		assert.LessOrEqual(t, paint.CountLines(m.render()), height, "height=%d", height)
	}
}

// lastFilled is the bottom row of a view that says anything.
func lastFilled(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}

	return ""
}

// TestFormStillsTheCursorOnTheListBesideIt covers the list cursor standing still while a form has the keys.
func TestFormStillsTheCursorOnTheListBesideIt(t *testing.T) {
	m := formColumnModel(t)
	m.rowCursor = newRowCursor()
	m.rowCursor.IsBlinked = true
	require.True(t, m.isListFocused(), "the list holds the keys to start with")
	require.Equal(t, blankCursor, m.rowCursorPrefix(), "so its cursor is mid-blink")

	m = openWorkflowForm(t, m)

	assert.False(t, m.isListFocused(), "the form holds them once it is up")
	assert.Equal(t, paint.Panel.Cursor().Render(rowCursorGlyph), m.rowCursorPrefix(), "and its cursor stands still")
}

func (m model) openAgentForm(workflow panel.Workflow, values *agentFormValues) model {
	openAgentForm(&m, workflow, values)

	return m
}

func (m model) openRename(agent panel.Agent) model {
	openRename(&m, agent)

	return m
}

func (m model) openRenameForm(agent panel.Agent, name *string) model {
	openRenameForm(&m, agent, name)

	return m
}

func (m model) archiveAgentsConfirm(agents []panel.Agent) (tea.Model, tea.Cmd) {
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
