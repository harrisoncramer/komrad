package ui

import (
	"context"
	"errors"
	"strings"
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
	"weave/internal/ui/layout"
	"weave/lib/xptr"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestFormsSubmitWhatIsOnScreen covers every form binding its fields to values the panel
// holds, since huh records a field into its own results only as the cursor leaves it.
func TestFormsSubmitWhatIsOnScreen(t *testing.T) {
	tests := []struct {
		name string

		open func(m model) model

		submit func(m model) (tea.Model, tea.Cmd)

		check func(t *testing.T, sent *formRecorder)
	}{
		{
			name: "the new-agent form",
			open: func(m model) model {
				workflow := m.listing.workflows[0]
				m.target.newAgentWorkflow = &workflow
				m.screen = screenForm
				prefill := agentFormValues{Harness: workflow.Harness, Dir: workflow.Cwd, Flags: workflow.Flags, SetupAction: inheritAction, CleanupAction: inheritAction}
				m.form, m.values.agent = newAgentForm(m.keys.Form, prefill, nil)
				m.values.agent.Prompt = "check CI"
				m.values.agent.Title = "ci"
				m.values.agent.Harness = "codex"

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitAgent() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdAgent)
				assert.Equal(t, "check CI", sent.createdAgent.GetPrompt())
				assert.Equal(t, "ci", sent.createdAgent.GetTitle())
				assert.Equal(t, "codex", sent.createdAgent.GetHarness(), "the workflow's harness is prefilled and can be overridden")
				assert.Equal(t, "/work/api", sent.createdAgent.GetDir(), "the workflow's directory is prefilled and kept")
				assert.Equal(t, "--model opus", sent.createdAgent.GetFlags())
				assert.Equal(t, "w1", sent.createdAgent.GetWorkflowId())
				assert.Nil(t, sent.createdAgent.SetupAction, "left on the default, the agent takes no override of its own")
				assert.Nil(t, sent.createdAgent.CleanupAction, "left on the default, the agent takes no override of its own")
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
				m.form, m.values.workflow, _ = workflowConfigForm("New workflow name", workflowFormValues{}, []actionChoice{
					{name: "make-worktree"},
					{name: "drop-worktree"},
				}, m.keys.Form)
				m.values.workflow.Name = "web"
				m.values.workflow.Context = "the frontend"
				m.values.workflow.Cwd = "/work/web"
				m.values.workflow.Flags = "--model sonnet"
				m.values.workflow.SetupType = setupTypeCustom
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
			name: "the new-workflow form with worktree enabled",
			open: func(m model) model {
				m.screen = screenWorkflowForm
				m.form, m.values.workflow, _ = workflowConfigForm("New workflow name", workflowFormValues{}, nil, m.keys.Form)
				m.values.workflow.Name = "web"
				m.values.workflow.Cwd = "/work/web"
				m.values.workflow.WorktreeBaseBranch = "develop"
				m.values.workflow.WorktreeCopyPaths = ".env\n\nconfig/local.json\n"
				m.values.workflow.WorktreeSymlinkPaths = "node_modules"
				m.values.workflow.WorktreeDiffExclude = "proto/gen\n*.pb.go"
				m.values.workflow.ArchiveOnPRClosed = true

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.submitWorkflowForm() },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdWorkflow)
				worktree := sent.createdWorkflow.GetWorktree()
				require.NotNil(t, worktree)
				assert.True(t, worktree.GetEnabled())
				assert.Equal(t, "develop", worktree.GetBaseBranch())
				assert.Equal(t, []string{".env", "config/local.json"}, worktree.GetCopyPaths(), "a blank line between two paths is dropped rather than sent as an entry")
				assert.Equal(t, []string{"node_modules"}, worktree.GetSymlinkPaths())
				assert.Equal(t, []string{"proto/gen", "*.pb.go"}, worktree.GetDiffExclude())
				assert.True(t, sent.createdWorkflow.GetArchiveOnPrClosed())
			},
		},
		{
			name: "the workflow edit form",
			open: func(m model) model {
				target := m.listing.workflows[0]
				m.target.editWorkflow = &target
				m.screen = screenWorkflowForm
				m.form, m.values.workflow, _ = workflowConfigForm("Workflow name", workflowFormValuesFrom(workflowstore.Config{
					Name:        target.Name,
					Context:     "shared notes",
					Cwd:         target.Cwd,
					Flags:       target.Flags,
					SetupAction: "make-worktree",
				}), []actionChoice{{name: "make-worktree"}}, m.keys.Form)
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
				target := m.listing.workflows[0]
				m.target.renameWorkflow = &target
				m.screen = screenWorkflowForm
				m.form, m.values.workflow = workflowForm("Rename \"api\" to", workflowFormValues{}, m.keys.Form)
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
					WorkflowID: m.listing.workflows[0].ID,
				}, m.listing.workflows, m.keys.Form)
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
			name: "the schedule edit form",
			open: func(m model) model {
				m.target.editSchedule = &schedulestore.Schedule{ID: "s1", Name: "sweeper"}
				m.screen = screenScheduleForm
				m.form, m.values.schedule = scheduleForm(schedulestore.Config{
					Name: "sweeper", Cron: "*/5 * * * *", Prompt: "sweep",
					Enabled: true, HideAgents: true, WorkflowID: "w1",
				}, m.listing.workflows, m.keys.Form)
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
				target := m.listing.workflows[0]
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
			name: "the agent archive prompt",
			open: func(m model) model {
				m.target.archiveAgents = []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}
				m.screen = screenAgentArchive
				m.form, m.values.archive = archiveAgentsForm(m.target.archiveAgents, m.keys.Form)
				*m.values.archive = archiveChoiceArchive
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateAgentArchive(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedAgents)
				assert.Equal(t, []string{"a1"}, sent.archivedAgents.GetIds())
				assert.Nil(t, sent.deletedAgents, "archiving alone keeps the record")
			},
		},
		{
			name: "the agent archive prompt answered archive and delete",
			open: func(m model) model {
				m.target.archiveAgents = []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}
				m.screen = screenAgentArchive
				m.form, m.values.archive = archiveAgentsForm(m.target.archiveAgents, m.keys.Form)
				*m.values.archive = archiveChoiceArchiveDelete
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateAgentArchive(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedAgents)
				assert.Equal(t, []string{"a1"}, sent.archivedAgents.GetIds())
				require.NotNil(t, sent.deletedAgents, "the delete follows the archive")
				assert.Equal(t, []string{"a1"}, sent.deletedAgents.GetIds())
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
		{
			name: "the workflow delete prompt",
			open: func(m model) model {
				workflow := archivedWorkflow("w1", "backend")
				m.target.deleteWorkflow = &workflow
				m.screen = screenArchiveDelete
				m.form, m.values.confirmed = deleteWorkflowForm(workflow, 0, m.keys.Form)
				*m.values.confirmed = true
				m.form.State = huh.StateCompleted

				return m
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateArchiveDelete(keyMsg("enter")) },
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedWorkflow)
				assert.Equal(t, "w1", sent.deletedWorkflow.GetId())
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

// TestNewAgentFormActionOverride covers the tri-state the action fields carry: left on
// the default the agent takes no override at all, and any other choice, including
// explicitly picking none, is sent as one.
func TestNewAgentFormActionOverride(t *testing.T) {
	tests := []struct {
		name          string
		setupAction   string
		wantSetup     *string
		cleanupAction string
		wantCleanup   *string
	}{
		{
			name:          "left on the default, neither field is sent",
			setupAction:   inheritAction,
			wantSetup:     nil,
			cleanupAction: inheritAction,
			wantCleanup:   nil,
		},
		{
			name:          "an explicit none is sent as an empty override, not nothing",
			setupAction:   "",
			wantSetup:     xptr.Ptr(""),
			cleanupAction: "",
			wantCleanup:   xptr.Ptr(""),
		},
		{
			name:          "a named choice is sent as itself",
			setupAction:   "install-deps",
			wantSetup:     xptr.Ptr("install-deps"),
			cleanupAction: "drop-worktree",
			wantCleanup:   xptr.Ptr("drop-worktree"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{}
			m := formModel(sent)
			m.listing.workflows[0].SetupAction = "make-worktree"
			m.listing.workflows[0].CleanupAction = "drop-worktree"
			workflow := m.listing.workflows[0]

			opened, _ := m.openNewAgent(&workflow)
			m = asModel(t, opened)
			m.values.agent.SetupAction = tt.setupAction
			m.values.agent.CleanupAction = tt.cleanupAction

			updated, cmd := m.submitAgent()
			submitted, ok := updated.(model)
			require.True(t, ok)
			settle(t, submitted, cmd)

			require.NotNil(t, sent.createdAgent)
			assert.Equal(t, tt.wantSetup, sent.createdAgent.SetupAction)
			assert.Equal(t, tt.wantCleanup, sent.createdAgent.CleanupAction)
		})
	}
}

// TestActionSelectShowsTheCurrentChoiceOnOpen covers an action field opening on a visible
// option, since huh.Select sizes its viewport when Options is called and not when Value is.
func TestActionSelectShowsTheCurrentChoiceOnOpen(t *testing.T) {
	available := []actionChoice{{name: "make-worktree"}, {name: "drop-worktree"}, {name: "install-deps"}}

	agentForm, _ := newAgentForm(config.DefaultKeybindings().Form,
		agentFormValues{SetupAction: inheritAction, CleanupAction: inheritAction},
		available,
	)
	agentForm.Init()
	agentView := agentForm.View()
	assert.Contains(t, agentView, "> Same as workflow", "the setup field opens with what it inherits marked as picked")
	assert.Contains(t, agentView, "> Same as workflow", "the cleanup field opens with what it inherits marked as picked")

	workflowForm, _, _ := workflowConfigForm("Workflow name", workflowFormValues{
		SetupType:     setupTypeCustom,
		SetupAction:   "make-worktree",
		CleanupAction: "drop-worktree",
	}, available, config.DefaultKeybindings().Form)
	workflowForm.Init()
	workflowView := workflowForm.View()
	assert.Contains(t, workflowView, "> make-worktree", "editing a workflow opens with its own stored setup action marked as the one picked, not none")
	assert.Contains(t, workflowView, "> drop-worktree", "editing a workflow opens with its own stored cleanup action marked as the one picked, not none")
}

// TestRefusedSubmitKeepsTheForm covers a refused write leaving the form up with what was
// filled in, since closing would cost every other field.
func TestRefusedSubmitKeepsTheForm(t *testing.T) {
	tests := []struct {
		name string

		open   func(t *testing.T, m model) model
		submit func(m model) (tea.Model, tea.Cmd)
		screen screen

		typed string
		held  func(m model) string
	}{
		{
			name: "the new-agent form, with no setup action to run",
			open: func(t *testing.T, m model) model {
				workflow := m.listing.workflows[0]
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

// TestNewAgentSubmitClosesImmediately covers the new-agent form closing on submit rather than
// holding for the write, since a setup action can run for minutes.
func TestNewAgentSubmitClosesImmediately(t *testing.T) {
	tests := []struct {
		name       string
		refuse     error
		wantOpen   bool
		wantDetail string
	}{
		{name: "a create that lands"},
		{
			name:     "a setup action that fails",
			refuse:   connect.NewError(connect.CodeFailedPrecondition, errors.New("the setup action failed: exit 7")),
			wantOpen: true,
		},
		{
			name:       "a create refused before setup ever ran",
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
			m.listing.workflows[0].SetupAction = "make-worktree"

			workflow := m.listing.workflows[0]
			opened, _ := m.openNewAgent(&workflow)
			m = asModel(t, opened)
			m.values.agent.Prompt = "check CI"

			updated, cmd := m.submitAgent()
			m = asModel(t, updated)

			assert.Equal(t, screenAgents, m.screen, "the form is gone the moment it was submitted")
			assert.False(t, m.pendingForm)
			assert.Equal(t, infoSetup, m.info.kind, "a pane opened for the setup action to stream into")
			assert.Equal(t, "%2", recorder.infoPane)

			got := settle(t, m, cmd)

			if tt.refuse != nil {
				if tt.wantOpen {
					assert.Nil(t, got.banner, "a setup failure is left to the setup log, not flashed again")
					assert.Equal(t, infoSetup, got.info.kind, "left up so what the setup action printed can still be read")
				} else {
					require.NotNil(t, got.banner, "a refused create is reported")
					assert.Equal(t, tt.wantDetail, got.banner.Detail)
					assert.Equal(t, infoNothing, got.info.kind, "nothing ran in it, so it closes with the refusal")
				}

				return
			}

			assert.Nil(t, got.banner)
			assert.Equal(t, infoNothing, got.info.kind, "closed once the create it was for has landed")
			require.NotNil(t, sent.createdAgent)
			assert.Equal(t, "%2", sent.createdAgent.GetSetupPane())
		})
	}
}

// TestSetupTypeRebuildsTheForm checks that switching setup type swaps the worktree fields
// for the action pickers by rebuilding the form, and hands back whatever was typed into
// the worktree fields if the setup type switches back.
func TestSetupTypeRebuildsTheForm(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)

	form, values, toggleIndex := m.workflowFormFor(workflowFormValues{})
	m.form = form
	m.values.workflow = values
	m.screen = screenWorkflowForm
	m.form.Init()
	for range toggleIndex {
		m.form.NextField()
	}

	worktreeView := m.form.View()
	assert.Equal(t, setupTypeWorktree, m.values.workflow.SetupType, "worktree is the default")
	assert.Contains(t, worktreeView, "Worktree base branch", "worktree is the default, so its fields are already on screen")
	assert.Contains(t, worktreeView, "Worktree copy paths")
	assert.Contains(t, worktreeView, "Worktree symlink paths")
	assert.NotContains(t, worktreeView, "Setup action")

	m.values.workflow.WorktreeCopyPaths = ".env"

	updated, _ := m.updateWorkflowForm(keyMsg("down"))
	m = asModel(t, updated)

	require.Equal(t, setupTypeCustom, m.values.workflow.SetupType, "the keypress switched it")
	customView := m.form.View()
	assert.NotContains(t, customView, "Worktree base branch", "switching to custom removes the worktree fields, not blanks them")
	assert.Contains(t, customView, "Setup action", "and adds the action pickers instead")
	assert.Contains(t, customView, "Cleanup action")

	updated, _ = m.updateWorkflowForm(keyMsg("up"))
	m = asModel(t, updated)

	assert.Equal(t, setupTypeWorktree, m.values.workflow.SetupType)
	assert.Equal(t, ".env", m.values.workflow.WorktreeCopyPaths, "switching back hands back what was typed before")
	assert.Contains(t, m.form.View(), "Worktree copy paths")
}

// TestSetupTypeSelectShowsBothOptionsOnOpen covers a select opening on the non-default answer,
// since huh scrolls to the bound value's index once it has both an accessor and options.
func TestSetupTypeSelectShowsBothOptionsOnOpen(t *testing.T) {
	form, _, toggleIndex := workflowConfigForm("Workflow name", workflowFormValues{SetupType: setupTypeCustom}, nil, config.DefaultKeybindings().Form)
	form.Init()
	for range toggleIndex {
		form.NextField()
	}

	view := form.View()
	assert.Contains(t, view, "Worktree: a git worktree per agent", "the worktree option is not hidden just because custom is selected")
	assert.Contains(t, view, "Custom: your own setup and cleanup actions")
	assert.Less(t,
		strings.Index(view, "Worktree: a git worktree per agent"),
		strings.Index(view, "Custom: your own setup and cleanup actions"),
		"worktree stays first regardless of which is selected",
	)
}

// TestArchivePromptChoices covers the three ways the prompt can be answered, where it opens
// on the reversible one so a submit alone never destroys anything.
func TestArchivePromptChoices(t *testing.T) {
	tests := []struct {
		name string

		down         int
		wantArchived []string
		wantDeleted  []string
	}{
		{
			name:         "submitting without moving archives and keeps the record",
			wantArchived: []string{"a1"},
		},
		{
			name:         "one row down archives and then destroys",
			down:         1,
			wantArchived: []string{"a1"},
			wantDeleted:  []string{"a1"},
		},
		{
			name: "the last row leaves them where they are",
			down: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{}

			opened, cmd := formModel(sent).archiveAgentsConfirm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}})
			m := delivered(t, asModel(t, opened), cmd)

			for range tt.down {
				m = deliver(t, m, keyMsg("down"))
			}

			got := deliver(t, m, keyMsg("ctrl+s"))

			require.Nil(t, got.banner)
			assert.Equal(t, screenAgents, got.screen, "every answer lands back on the list")
			assert.Equal(t, tt.wantArchived, sent.archivedAgents.GetIds())
			assert.Equal(t, tt.wantDeleted, sent.deletedAgents.GetIds())
		})
	}
}

// TestArchivePromptOpensOnArchive covers the prompt as first drawn, since huh scrolls its
// list to whatever value is bound and a submit would otherwise answer for nobody.
func TestArchivePromptOpensOnArchive(t *testing.T) {
	form, choice := archiveAgentsForm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}}, config.DefaultKeybindings().Form)
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	assert.Equal(t, archiveChoiceArchive, *choice, "the reversible answer is what a submit alone gives")

	var rows []string
	for _, line := range strings.Split(ansi.Strip(form.View()), "\n") {
		if row := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "┃")); row != "" {
			rows = append(rows, row)
		}
	}

	assert.Contains(t, rows, "> Archive", "and it is the row the cursor is on")
	assert.Contains(t, rows, "Archive and delete")
	assert.Contains(t, rows, "Cancel")
}

// TestArchivePromptKeepsTheRecordWhenTheArchiveIsRefused covers the delete only following an
// archive that landed, so a refused archive leaves the agent there rather than destroyed.
func TestArchivePromptKeepsTheRecordWhenTheArchiveIsRefused(t *testing.T) {
	sent := &formRecorder{refuse: connect.NewError(connect.CodeFailedPrecondition, errors.New("already archived"))}

	opened, _ := formModel(sent).archiveAgentsConfirm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}})
	m := asModel(t, opened)
	*m.values.archive = archiveChoiceArchiveDelete
	m.form.State = huh.StateCompleted

	submitted, cmd := m.updateAgentArchive(keyMsg("enter"))
	got := settle(t, asModel(t, submitted), cmd)

	require.NotNil(t, got.banner, "the refusal is reported")
	assert.Equal(t, `Could not archive and delete agent "api-refactor"`, got.banner.Summary)
	assert.Nil(t, sent.deletedAgents, "nothing was destroyed")
}

// TestConfirmingArchiveTracksTheWriteAsPending covers the write being issued on a value
// receiver, so the model it mutates has to be the one the confirm form returns.
func TestConfirmingArchiveTracksTheWriteAsPending(t *testing.T) {
	sent := &formRecorder{}

	opened, _ := formModel(sent).archiveAgentsConfirm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}})
	m := asModel(t, opened)
	*m.values.archive = archiveChoiceArchive
	m.form.State = huh.StateCompleted

	submitted, cmd := m.updateAgentArchive(keyMsg("enter"))
	got := asModel(t, submitted)

	assert.Greater(t, got.pending, 0, "the in-flight write is tracked on the model that comes back")
	assert.Nil(t, got.refreshCmd(), "a refresh mid-write is held off rather than racing it")
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

// TestFormsSubmitNothingWithoutValues covers a submit reaching a panel with no bound values,
// since every submit path is a method anything could call.
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

// TestFormResultsAreNotWhatIsSubmitted pins why every form binds its values, since a field
// only reaches huh's own results as the cursor leaves it.
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

func formModel(sent *formRecorder) model {
	tmux := &recordingTmux{}

	return model{
		api:     api{agents: sent, workflows: sent, schedules: sent, actions: sent},
		tmux:    tmux,
		popup:   NewPopupWindow(tmux, "test-session"),
		frame:   layout.New(tmux, config.DefaultLayout()),
		keys:    config.DefaultKeybindings(),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "api", Harness: "claude", Cwd: "/work/api", Flags: "--model opus"}}},
	}
}

type formRecorder struct {
	agentv1connect.AgentServiceClient
	workflowv1connect.WorkflowServiceClient
	schedulev1connect.ScheduleServiceClient
	actionv1connect.ActionServiceClient

	refuse error

	createdAgent     *agentv1.CreateAgentRequest
	adoptedAgent     *agentv1.AdoptRequest
	renamedAgent     *agentv1.UpdateAgentRequest
	deletedAgents    *agentv1.DeleteAgentsRequest
	archivedAgents   *agentv1.ArchiveAgentsRequest
	createdWorkflow  *workflowv1.CreateWorkflowRequest
	updatedWorkflow  *workflowv1.UpdateWorkflowRequest
	archivedWorkflow *workflowv1.ArchiveWorkflowRequest
	deletedWorkflow  *workflowv1.DeleteWorkflowRequest
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

// TestOnlyTheSubmitKeySubmitsAForm covers enter never submitting, since it would complete a
// form huh cannot resume and leave a refusal nowhere to come back to.
func TestOnlyTheSubmitKeySubmitsAForm(t *testing.T) {
	keys := config.DefaultKeybindings().Form

	agent, _ := newAgentForm(keys, agentFormValues{}, nil)
	agent.Init()
	for range 6 {
		agent.Update(huh.NextField())
	}

	pump(t, agent, keyMsg("enter"))
	assert.NotEqual(t, huh.StateCompleted, agent.State, "enter on the last field leaves the form alone")

	rename, _ := renameForm("api-refactor", "", keys)
	rename.Init()

	pump(t, rename, keyMsg("enter"))
	assert.NotEqual(t, huh.StateCompleted, rename.State, "enter leaves a one-field form alone too")

	pump(t, rename, keyMsg(keys.Submit[0]))
	assert.Equal(t, huh.StateCompleted, rename.State, "the configured key still submits it")
}

// TestFormTakesNoKeysWhileItsWriteIsInFlight covers the second ctrl+s landing before the
// first write has answered, which would launch the same agent twice.
func TestFormTakesNoKeysWhileItsWriteIsInFlight(t *testing.T) {
	sent := &formRecorder{}

	m := formModel(sent)
	workflow := m.listing.workflows[0]
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

func pump(t *testing.T, form *huh.Form, msg tea.Msg) {
	t.Helper()

	queue := []tea.Msg{msg}
	for step := 0; len(queue) > 0 && step < 10; step++ {
		_, cmd := form.Update(queue[0])
		queue = append(queue[1:], produced(cmd)...)
	}
}

// TestAdoptFormSendsWhatWasTyped covers the adopt form having no directory field, since the
// service is what decides where the agent runs.
func TestAdoptFormSendsWhatWasTyped(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)
	workflow := m.listing.workflows[0]

	opened, _ := m.openAdopt(&workflow)
	adopting := asModel(t, opened)
	require.Equal(t, screenAdopt, adopting.screen)
	assert.Equal(t, "claude", adopting.values.adopt.Harness, "the workflow's harness and flags are prefilled")
	assert.Equal(t, "--model opus", adopting.values.adopt.Flags)

	adopting.values.adopt.Ref = "https://github.com/chariot-giving/admin/pull/1069"
	adopting.values.adopt.Prompt = "pick it up"

	updated, cmd := adopting.submitAdopt()
	submitted, ok := updated.(model)
	require.True(t, ok)
	settle(t, submitted, cmd)

	require.NotNil(t, sent.adoptedAgent)
	assert.Equal(t, "https://github.com/chariot-giving/admin/pull/1069", sent.adoptedAgent.GetRef())
	assert.Equal(t, "pick it up", sent.adoptedAgent.GetPrompt())
	assert.Equal(t, "w1", sent.adoptedAgent.GetWorkflowId())
	assert.Nil(t, sent.createdAgent, "adoption does not go through create")
}

// TestAdoptKeyOpensTheForm covers the binding, which acts on a workflow header: adoption
// goes into a workflow, so the row it answers on is the workflow rather than an agent
// already inside it.
func TestAdoptKeyOpensTheForm(t *testing.T) {
	m := infoModel(&recordingTmux{})
	m.api = api{agents: &formRecorder{}}
	m.tree.cursor = 0

	entered, _ := m.updateAgents(keyMsg("a"))
	opened := asModel(t, entered)

	require.Equal(t, screenAdopt, opened.screen)
	require.NotNil(t, opened.values.adopt)
	assert.Equal(t, "w1", opened.target.newAgentWorkflow.ID, "it adopts into the workflow under the cursor")
}

// TestAdoptKeyDoesNothingOnAnAgentRow covers the other half of that: the key is the
// workflow's, so a row inside one is not what adoption is asked for.
func TestAdoptKeyDoesNothingOnAnAgentRow(t *testing.T) {
	m := infoModel(&recordingTmux{})
	m.api = api{agents: &formRecorder{}}

	entered, _ := m.updateAgents(keyMsg("a"))
	opened := asModel(t, entered)

	assert.Equal(t, screenAgents, opened.screen)
}
