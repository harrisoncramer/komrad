package ui

import (
	"context"
	"errors"
	"testing"

	"weave/config"
	actionv1 "weave/proto/gen/action/v1"
	"weave/proto/gen/action/v1/actionv1connect"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1 "weave/proto/gen/schedule/v1"
	"weave/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestFormsSubmitWhatIsOnScreen covers every form the panel submits, from the values
// it was opened with to the request that reaches the service.
//
// The cursor is never moved in any of these cases, which is the point. huh records a
// field into the form's own results only as the cursor leaves it, so a form read that
// way would send the fields below the cursor as blank, and the panel's submit key
// submits from wherever the cursor happens to be. Each form binds its fields to values
// the panel holds instead, so what is on screen is what is sent.
func TestFormsSubmitWhatIsOnScreen(t *testing.T) {
	tests := []struct {
		name string
		// open puts the form on screen with its fields carrying what a person would be
		// looking at, the prefill plus whatever they typed into it.
		open func(m model) model
		// submit is the path the panel takes when the form is submitted.
		submit func(m model) (tea.Model, tea.Cmd)
		// check is what the service should have been sent.
		check func(t *testing.T, sent *formRecorder)
	}{
		{
			name: "the new-agent form",
			open: func(m model) model {
				workflow := m.tree.workflows[0]
				m.target.newAgentWorkflow = &workflow
				m.screen = screenForm
				m.form, m.values.agent = newAgentForm(m.keys.Form, agentFormValues{Dir: workflow.Cwd, Flags: workflow.Flags})
				m.values.agent.Prompt = "check CI"
				m.values.agent.Title = "ci"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitAgent() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdAgent)
				assert.Equal(t, "check CI", sent.createdAgent.GetPrompt())
				assert.Equal(t, "ci", sent.createdAgent.GetTitle())
				assert.Equal(t, "/work/api", sent.createdAgent.GetDir(), "the workflow's directory is prefilled and kept")
				assert.Equal(t, "--model opus", sent.createdAgent.GetFlags())
				assert.Equal(t, "w1", sent.createdAgent.GetWorkflowId())
			},
		},
		{
			name: "the rename form",
			open: func(m model) model {
				m.target.renameAgent = agentstore.Agent{ID: "a1", Name: "api-refactor"}
				m.screen = screenRename
				m.form, m.values.rename = renameForm("api-refactor", "", m.keys.Form)
				*m.values.rename = "api-v2"
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateRename(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.renamedAgent)
				assert.Equal(t, "a1", sent.renamedAgent.GetId())
				assert.Equal(t, "api-v2", sent.renamedAgent.GetName())
			},
		},
		{
			name: "the new-workflow form",
			open: func(m model) model {
				m.screen = screenWorkflowForm
				m.form, m.values.workflow = workflowConfigForm("New workflow name", workflowstore.Config{}, []actionChoice{
					{name: "make-worktree", description: "Cut a worktree for the agent."},
					{name: "drop-worktree"},
				}, m.keys.Form)
				m.values.workflow.Name = "web"
				m.values.workflow.Context = "the frontend"
				m.values.workflow.Cwd = "/work/web"
				m.values.workflow.Flags = "--model sonnet"
				m.values.workflow.SetupAction = "make-worktree"
				m.values.workflow.CleanupAction = "drop-worktree"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdWorkflow)
				assert.Equal(t, "web", sent.createdWorkflow.GetName())
				assert.Equal(t, "the frontend", sent.createdWorkflow.GetContext())
				assert.Equal(t, "/work/web", sent.createdWorkflow.GetCwd())
				assert.Equal(t, "--model sonnet", sent.createdWorkflow.GetFlags())
				assert.Equal(t, "make-worktree", sent.createdWorkflow.GetSetupAction())
				assert.Equal(t, "drop-worktree", sent.createdWorkflow.GetCleanupAction())
			},
		},
		{
			// The edit form opens on what is stored, so a submit that changed one field
			// has to carry the rest as they were. The service replaces every field.
			name: "the workflow edit form",
			open: func(m model) model {
				target := m.tree.workflows[0]
				m.target.editWorkflow = &target
				m.screen = screenWorkflowForm
				m.form, m.values.workflow = workflowConfigForm("Workflow name", workflowstore.Config{
					Name:        target.Name,
					Context:     "shared notes",
					Cwd:         target.Cwd,
					Flags:       target.Flags,
					SetupAction: "make-worktree",
				}, []actionChoice{{name: "make-worktree"}}, m.keys.Form)
				m.values.workflow.Cwd = "/work/api-v2"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.updatedWorkflow)
				assert.Equal(t, "w1", sent.updatedWorkflow.GetId())
				assert.Equal(t, "/work/api-v2", sent.updatedWorkflow.GetCwd(), "the field that was edited")
				assert.Equal(t, "api", sent.updatedWorkflow.GetName(), "and the ones that were not")
				assert.Equal(t, "shared notes", sent.updatedWorkflow.GetContext())
				assert.Equal(t, "--model opus", sent.updatedWorkflow.GetFlags())
				assert.Equal(t, "make-worktree", sent.updatedWorkflow.GetSetupAction(), "the stored choice survives a round trip through the field")
			},
		},
		{
			name: "the workflow rename form",
			open: func(m model) model {
				target := m.tree.workflows[0]
				m.target.renameWorkflow = &target
				m.screen = screenWorkflowForm
				m.form, m.values.workflow = workflowForm("Rename \"api\" to", workflowstore.Config{}, m.keys.Form)
				m.values.workflow.Name = "backend"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.updatedWorkflow)
				assert.Equal(t, "backend", sent.updatedWorkflow.GetName())
				assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd(), "a rename carries the config it is not changing")
			},
		},
		{
			name: "the new-schedule form",
			open: func(m model) model {
				m.screen = screenScheduleForm
				m.form, m.values.schedule = scheduleForm(schedulestore.Config{
					Enabled:    true,
					WorkflowID: m.tree.workflows[0].ID,
				}, m.tree.workflows, m.keys.Form)
				m.values.schedule.Name = "sweeper"
				m.values.schedule.Cron = "*/5 * * * *"
				m.values.schedule.Prompt = "sweep"
				m.values.schedule.HideAgents = true

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitScheduleForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdSchedule)
				assert.Equal(t, "sweeper", sent.createdSchedule.GetName())
				assert.Equal(t, "*/5 * * * *", sent.createdSchedule.GetCron())
				assert.Equal(t, "sweep", sent.createdSchedule.GetPrompt())
				assert.True(t, sent.createdSchedule.GetEnabled(), "armed is prefilled on and never touched")
				assert.True(t, sent.createdSchedule.GetHideAgents())
			},
		},
		{
			// The toggle sits at the bottom of the form, furthest from where a submit is
			// likely to happen, so this is the case that would silently unhide a
			// schedule's runs if the values were read off the cursor's trail.
			name: "the schedule edit form",
			open: func(m model) model {
				m.target.editSchedule = &schedulestore.Schedule{ID: "s1", Name: "sweeper"}
				m.screen = screenScheduleForm
				m.form, m.values.schedule = scheduleForm(schedulestore.Config{
					Name: "sweeper", Cron: "*/5 * * * *", Prompt: "sweep",
					Enabled: true, HideAgents: true, WorkflowID: "w1",
				}, m.tree.workflows, m.keys.Form)
				m.values.schedule.Prompt = "sweep harder"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitScheduleForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.updatedSchedule)
				assert.Equal(t, "s1", sent.updatedSchedule.GetId())
				assert.Equal(t, "sweep harder", sent.updatedSchedule.GetPrompt(), "the field that was edited")
				assert.True(t, sent.updatedSchedule.GetHideAgents(), "and the flag it was opened with")
			},
		},
		{
			name: "the workflow archive prompt",
			open: func(m model) model {
				target := m.tree.workflows[0]
				m.target.archiveWorkflow = &target
				m.screen = screenWorkflowArchive
				m.form, m.values.confirmed = archiveWorkflowForm(target.Name, m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateWorkflowArchive(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedWorkflow)
				assert.Equal(t, "w1", sent.archivedWorkflow.GetId())
			},
		},
		{
			name: "the schedule delete prompt",
			open: func(m model) model {
				m.target.archiveSchedule = &schedulestore.Schedule{ID: "s1", Name: "sweeper"}
				m.screen = screenScheduleArchive
				m.form, m.values.confirmed = archiveScheduleForm("sweeper", m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateScheduleArchive(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedSchedule)
				assert.Equal(t, "s1", sent.archivedSchedule.GetId())
			},
		},
		{
			name: "the archive delete prompt",
			open: func(m model) model {
				m.archive.agents = []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}
				m.target.deleteAgents = m.archive.agents
				m.screen = screenArchiveDelete
				m.form, m.values.confirmed = deleteAgentsForm(m.archive.agents, m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateArchiveDelete(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedAgents)
				assert.Equal(t, []string{"a1"}, sent.deletedAgents.GetIds())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{}
			m := tt.open(formModel(sent))
			m.form.Init()

			updated, cmd := tt.submit(m)
			submitted, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, submitted, cmd)

			require.Nil(t, got.banner, "the service accepted the write")
			assert.False(t, got.screen.isForm(), "a write the service took closes the form")
			tt.check(t, sent)
		})
	}
}

// TestRefusedSubmitKeepsTheForm covers what a refused write leaves on screen. The form
// stays up carrying what was filled in, since the field that caused the refusal is fixed
// from there and closing would cost every other field with it.
func TestRefusedSubmitKeepsTheForm(t *testing.T) {
	tests := []struct {
		name string
		// open puts the form on screen and fills it in. A case that sets the form completed
		// is one submitted from its last field, which is the path that rebuilds.
		open   func(t *testing.T, m model) model
		submit func(m model) (tea.Model, tea.Cmd)
		screen screen
		// typed is what was filled in, and held reads it back off the panel afterwards.
		typed string
		held  func(m model) string
	}{
		{
			name: "the new-agent form",
			open: func(t *testing.T, m model) model {
				workflow := m.tree.workflows[0]
				opened, _ := m.openNewAgent(&workflow)
				m = asModel(t, opened)
				m.values.agent.Prompt = "check CI"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateForm(keyMsg("ctrl+s")) },
			screen: screenForm,
			typed:  "check CI",
			held:   func(m model) string { return m.values.agent.Prompt },
		},
		{
			name: "the rename form",
			open: func(t *testing.T, m model) model {
				m.target.renameAgent = agentstore.Agent{ID: "a1", Name: "api-refactor"}
				m.screen = screenRename
				m.form, m.values.rename = renameForm("api-refactor", "", m.keys.Form)
				*m.values.rename = "api-v2"
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitRename() },
			screen: screenRename,
			typed:  "api-v2",
			held:   func(m model) string { return *m.values.rename },
		},
		{
			name: "the new-workflow form",
			open: func(t *testing.T, m model) model {
				opened, _ := m.newWorkflowForm()
				m = asModel(t, opened)
				m.values.workflow.Name = "web"
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
			screen: screenWorkflowForm,
			typed:  "web",
			held:   func(m model) string { return m.values.workflow.Name },
		},
		{
			name: "the new-schedule form",
			open: func(t *testing.T, m model) model {
				opened, _ := m.newScheduleForm()
				m = asModel(t, opened)
				m.values.schedule.Cron = "*/5 * * * *"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitScheduleForm() },
			screen: screenScheduleForm,
			typed:  "*/5 * * * *",
			held:   func(m model) string { return m.values.schedule.Cron },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{refuse: connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed"))}
			m := tt.open(t, formModel(sent))
			m.form.Init()

			updated, cmd := tt.submit(m)
			got := settle(t, asModel(t, updated), cmd)

			require.NotNil(t, got.banner, "a refused write is reported")
			assert.Equal(t, "not allowed", got.banner.Detail)
			assert.Equal(t, "fix and submit again", got.banner.Hint, "esc backs out of the form rather than clearing the banner")
			assert.Equal(t, tt.screen, got.screen, "the form still owns the screen")
			assert.Equal(t, tt.typed, tt.held(got), "and still holds what was filled in")
			assert.NotEqual(t, huh.StateCompleted, got.form.State, "a completed form draws as nothing, so the form is built back")
			assert.NotEmpty(t, got.form.View())
			assert.False(t, got.pendingForm, "and takes keys again, since the write it was waiting on has landed")
		})
	}
}

// TestFormResubmitsAfterARefusal covers the point of keeping the form. The fields it kept
// are what the second attempt sends, so a refusal costs the keystroke that caused it
// rather than the whole form.
//
// Both submits go through Update, which is where the panel drops the keys bound for a form
// that is waiting. Calling the form handler directly walks around that.
func TestFormResubmitsAfterARefusal(t *testing.T) {
	sent := &formRecorder{refuse: connect.NewError(connect.CodeInvalidArgument, errors.New("bad flags"))}

	m := formModel(sent)
	workflow := m.tree.workflows[0]
	opened, _ := m.openNewAgent(&workflow)
	m = asModel(t, opened)
	m.values.agent.Prompt = "check CI"
	m.values.agent.Flags = "--modle opus"

	attempted, cmd := m.Update(keyMsg("ctrl+s"))
	refused := settle(t, asModel(t, attempted), cmd)
	require.Nil(t, sent.createdAgent, "the service refused the first attempt")
	require.False(t, refused.pendingForm, "the refusal ended the wait, so the form takes keys again")

	sent.refuse = nil
	refused.values.agent.Flags = "--model opus"

	updated, cmd := refused.Update(keyMsg("ctrl+s"))
	got := settle(t, asModel(t, updated), cmd)

	require.NotNil(t, sent.createdAgent)
	assert.Equal(t, "check CI", sent.createdAgent.GetPrompt(), "the prompt survived the refusal")
	assert.Equal(t, "--model opus", sent.createdAgent.GetFlags(), "and the field that was fixed is what went")
	assert.Nil(t, got.banner)
	assert.Equal(t, screenAgents, got.screen, "the accepted write closes the form")
}

// asModel unwraps what an update handed back, which is the panel's model in every case.
func asModel(t *testing.T, updated tea.Model) model {
	t.Helper()

	m, ok := updated.(model)
	require.True(t, ok)

	return m
}

// TestFormsSubmitNothingWithoutValues covers a submit reaching a panel holding no
// bound values, which is a form that was never opened. Every submit path is a method
// anything could call, so none of them may write on an empty form.
func TestFormsSubmitNothingWithoutValues(t *testing.T) {
	tests := []struct {
		name   string
		submit func(m model) (tea.Model, tea.Cmd)
	}{
		{
			name:   "the new-agent form",
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitAgent() },
		},
		{
			name:   "the workflow form",
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
		},
		{
			name:   "the schedule form",
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitScheduleForm() },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{}

			m := formModel(sent)
			updated, cmd := tt.submit(m)
			submitted, ok := updated.(model)
			require.True(t, ok)
			settle(t, submitted, cmd)

			assert.Nil(t, sent.createdAgent)
			assert.Nil(t, sent.createdWorkflow)
			assert.Nil(t, sent.updatedWorkflow)
			assert.Nil(t, sent.createdSchedule)
			assert.Nil(t, sent.updatedSchedule)
		})
	}
}

// TestFormResultsAreNotWhatIsSubmitted pins the reason every form binds its values. A
// field only reaches huh's own results as the cursor leaves it, so a form read that way
// arrives empty however it was prefilled, and the panel's submit key submits from
// wherever the cursor is.
func TestFormResultsAreNotWhatIsSubmitted(t *testing.T) {
	form, values := scheduleForm(schedulestore.Config{
		Name: "sweeper", Cron: "*/5 * * * *", Prompt: "sweep",
		Enabled: true, HideAgents: true,
	}, nil, config.DefaultKeybindings().Form)
	form.Init()

	assert.Empty(t, form.GetString("name"), "huh has recorded nothing the cursor has not left")
	assert.False(t, form.GetBool("hide_agents"))

	require.NotNil(t, values)
	assert.Equal(t, "sweeper", values.Name, "the bound values are what a submit reads")
	assert.True(t, values.HideAgents)
}

// formModel builds a panel with one workflow to submit against, holding no open form.
// Each case opens the one it is about.
func formModel(sent *formRecorder) model {
	tmux := &recordingTmux{}

	return model{
		api:   api{agents: sent, workflows: sent, schedules: sent, actions: sent},
		tmux:  tmux,
		popup: NewPopupWindow(tmux, "test-session"),
		stage: NewStage(tmux, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		tree: treeState{
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "api", Cwd: "/work/api", Flags: "--model opus"}},
		},
	}
}

// formRecorder accepts every write a form makes and keeps the request it was sent, so
// a case can assert on what reached the service rather than on how many calls it took.
// It answers as all three clients, since a form submit lists as well as writes. The
// embedded interfaces are nil, so a method nothing here implements panics rather than
// quietly returning a zero value.
type formRecorder struct {
	agentv1connect.AgentServiceClient
	workflowv1connect.WorkflowServiceClient
	schedulev1connect.ScheduleServiceClient
	actionv1connect.ActionServiceClient

	// refuse is what every write answers with while it is set, which is the refusal a
	// form has to survive.
	refuse error

	createdAgent     *agentv1.CreateAgentRequest
	renamedAgent     *agentv1.UpdateAgentRequest
	deletedAgents    *agentv1.DeleteAgentsRequest
	createdWorkflow  *workflowv1.CreateWorkflowRequest
	updatedWorkflow  *workflowv1.UpdateWorkflowRequest
	archivedWorkflow *workflowv1.ArchiveWorkflowRequest
	createdSchedule  *schedulev1.CreateScheduleRequest
	updatedSchedule  *schedulev1.UpdateScheduleRequest
	archivedSchedule *schedulev1.ArchiveScheduleRequest
}

func (f *formRecorder) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

func (f *formRecorder) ListActions(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[actionv1.ListActionsResponse], error) {
	return connect.NewResponse(&actionv1.ListActionsResponse{}), nil
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

// TestOnlyTheSubmitKeySubmitsAMultiFieldForm covers the keymap the fillable forms use.
// Enter from the last field would complete the form, which huh cannot resume, so a refusal
// would have nowhere to come back to. The one-field forms keep enter, since there is no
// cursor to lose in them.
func TestOnlyTheSubmitKeySubmitsAMultiFieldForm(t *testing.T) {
	keys := config.DefaultKeybindings().Form

	agent, _ := newAgentForm(keys, agentFormValues{})
	agent.Init()
	for range 3 {
		agent.Update(huh.NextField())
	}

	pump(t, agent, keyMsg("enter"))
	assert.NotEqual(t, huh.StateCompleted, agent.State, "enter on the last field leaves the form alone")

	rename, _ := renameForm("api-refactor", "", keys)
	rename.Init()

	pump(t, rename, keyMsg("enter"))
	assert.Equal(t, huh.StateCompleted, rename.State, "a one-field form still submits on enter")
}

// TestFormTakesNoKeysWhileItsWriteIsInFlight covers the second ctrl+s landing before the
// first write has answered, which would launch the same agent twice.
func TestFormTakesNoKeysWhileItsWriteIsInFlight(t *testing.T) {
	sent := &formRecorder{}

	m := formModel(sent)
	workflow := m.tree.workflows[0]
	opened, _ := m.openNewAgent(&workflow)
	m = asModel(t, opened)
	m.values.agent.Prompt = "check CI"
	m.pendingForm = true

	updated, cmd := m.Update(keyMsg("ctrl+s"))
	got := settle(t, asModel(t, updated), cmd)

	assert.Nil(t, sent.createdAgent, "the key never reached the submit path")
	assert.Equal(t, screenForm, got.screen)
	assert.Equal(t, "check CI", got.values.agent.Prompt, "and the form is as it was")
}

// pump applies a message to a form and feeds back whatever the form asks for, since moving
// between fields takes a couple of rounds. The step bound is a backstop against a form that
// keeps asking, such as a blinking cursor.
func pump(t *testing.T, form *huh.Form, msg tea.Msg) {
	t.Helper()

	queue := []tea.Msg{msg}
	for step := 0; len(queue) > 0 && step < 10; step++ {
		_, cmd := form.Update(queue[0])
		queue = append(queue[1:], produced(cmd)...)
	}
}
