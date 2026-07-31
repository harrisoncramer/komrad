package ui

import (
	"context"
	"testing"

	"weave/internal/appconfig"
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
				m.form, m.values.agent = newAgentForm(m.keys.Form, workflow.Cwd, workflow.Flags)
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
				m.form, m.values.rename = renameForm("api-refactor", m.keys.Form)
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
				m.form, m.values.workflow = workflowConfigForm("New workflow name", workflowstore.Config{}, m.keys.Form)
				m.values.workflow.Name = "web"
				m.values.workflow.Context = "the frontend"
				m.values.workflow.Cwd = "/work/web"
				m.values.workflow.Flags = "--model sonnet"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdWorkflow)
				assert.Equal(t, "web", sent.createdWorkflow.GetName())
				assert.Equal(t, "the frontend", sent.createdWorkflow.GetContext())
				assert.Equal(t, "/work/web", sent.createdWorkflow.GetCwd())
				assert.Equal(t, "--model sonnet", sent.createdWorkflow.GetFlags())
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
					Name:    target.Name,
					Context: "shared notes",
					Cwd:     target.Cwd,
					Flags:   target.Flags,
				}, m.keys.Form)
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
			},
		},
		{
			name: "the workflow rename form",
			open: func(m model) model {
				target := m.tree.workflows[0]
				m.target.renameWorkflow = &target
				m.screen = screenWorkflowForm
				m.form, m.values.workflow = workflowForm("Rename \"api\" to", m.keys.Form)
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
			name: "the workflow delete prompt",
			open: func(m model) model {
				target := m.tree.workflows[0]
				m.target.deleteWorkflow = &target
				m.screen = screenWorkflowDelete
				m.form, m.values.confirmed = deleteWorkflowForm(target.Name, m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateWorkflowDelete(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedWorkflow)
				assert.Equal(t, "w1", sent.deletedWorkflow.GetId())
			},
		},
		{
			name: "the schedule delete prompt",
			open: func(m model) model {
				m.target.deleteSchedule = &schedulestore.Schedule{ID: "s1", Name: "sweeper"}
				m.screen = screenScheduleDelete
				m.form, m.values.confirmed = deleteScheduleForm("sweeper", m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateScheduleDelete(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedSchedule)
				assert.Equal(t, "s1", sent.deletedSchedule.GetId())
			},
		},
		{
			name: "the trash delete prompt",
			open: func(m model) model {
				m.trash.agents = []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}
				m.target.deleteAgent = &m.trash.agents[0]
				m.screen = screenTrashDelete
				m.form, m.values.confirmed = deleteAgentForm("api-refactor", m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateTrashDelete(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedAgent)
				assert.Equal(t, "a1", sent.deletedAgent.GetId())
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
			tt.check(t, sent)
		})
	}
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
	}, nil, appconfig.DefaultKeybindings().Form)
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
		api:   api{agents: sent, workflows: sent, schedules: sent},
		tmux:  tmux,
		popup: NewPopup(tmux, "test-session"),
		stage: NewStage(tmux, appconfig.DefaultSplit),
		keys:  appconfig.DefaultKeybindings(),
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

	createdAgent    *agentv1.CreateAgentRequest
	renamedAgent    *agentv1.UpdateAgentRequest
	deletedAgent    *agentv1.DeleteAgentRequest
	createdWorkflow *workflowv1.CreateWorkflowRequest
	updatedWorkflow *workflowv1.UpdateWorkflowRequest
	deletedWorkflow *workflowv1.DeleteWorkflowRequest
	createdSchedule *schedulev1.CreateScheduleRequest
	updatedSchedule *schedulev1.UpdateScheduleRequest
	deletedSchedule *schedulev1.DeleteScheduleRequest
}

func (f *formRecorder) ListAgents(context.Context, *connect.Request[agentv1.ListAgentsRequest]) (*connect.Response[agentv1.ListAgentsResponse], error) {
	return connect.NewResponse(&agentv1.ListAgentsResponse{}), nil
}

func (f *formRecorder) CreateAgent(_ context.Context, req *connect.Request[agentv1.CreateAgentRequest]) (*connect.Response[agentv1.CreateAgentResponse], error) {
	f.createdAgent = req.Msg

	return connect.NewResponse(&agentv1.CreateAgentResponse{Agent: &agentv1.Agent{Id: "new"}}), nil
}

func (f *formRecorder) UpdateAgent(_ context.Context, req *connect.Request[agentv1.UpdateAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	f.renamedAgent = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) DeleteAgent(_ context.Context, req *connect.Request[agentv1.DeleteAgentRequest]) (*connect.Response[emptypb.Empty], error) {
	f.deletedAgent = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ListWorkflows(context.Context, *connect.Request[workflowv1.ListWorkflowsRequest]) (*connect.Response[workflowv1.ListWorkflowsResponse], error) {
	return connect.NewResponse(&workflowv1.ListWorkflowsResponse{}), nil
}

func (f *formRecorder) CreateWorkflow(_ context.Context, req *connect.Request[workflowv1.CreateWorkflowRequest]) (*connect.Response[workflowv1.CreateWorkflowResponse], error) {
	f.createdWorkflow = req.Msg

	return connect.NewResponse(&workflowv1.CreateWorkflowResponse{Workflow: &workflowv1.Workflow{Id: "new"}}), nil
}

func (f *formRecorder) UpdateWorkflow(_ context.Context, req *connect.Request[workflowv1.UpdateWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	f.updatedWorkflow = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) DeleteWorkflow(_ context.Context, req *connect.Request[workflowv1.DeleteWorkflowRequest]) (*connect.Response[emptypb.Empty], error) {
	f.deletedWorkflow = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) ListSchedules(context.Context, *connect.Request[schedulev1.ListSchedulesRequest]) (*connect.Response[schedulev1.ListSchedulesResponse], error) {
	return connect.NewResponse(&schedulev1.ListSchedulesResponse{}), nil
}

func (f *formRecorder) CreateSchedule(_ context.Context, req *connect.Request[schedulev1.CreateScheduleRequest]) (*connect.Response[schedulev1.CreateScheduleResponse], error) {
	f.createdSchedule = req.Msg

	return connect.NewResponse(&schedulev1.CreateScheduleResponse{Schedule: &schedulev1.Schedule{Id: "new"}}), nil
}

func (f *formRecorder) UpdateSchedule(_ context.Context, req *connect.Request[schedulev1.UpdateScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	f.updatedSchedule = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (f *formRecorder) DeleteSchedule(_ context.Context, req *connect.Request[schedulev1.DeleteScheduleRequest]) (*connect.Response[emptypb.Empty], error) {
	f.deletedSchedule = req.Msg

	return connect.NewResponse(&emptypb.Empty{}), nil
}
