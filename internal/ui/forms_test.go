package ui

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"weave/config"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"
	hookv1 "weave/proto/gen/hook/v1"
	"weave/proto/gen/hook/v1/hookv1connect"
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
// holds.
func TestFormsSubmitWhatIsOnScreen(t *testing.T) {
	tests := []struct {
		name   string
		open   func(t *testing.T, m model) model
		submit func(m model) (tea.Model, tea.Cmd)
		check  func(t *testing.T, sent *formRecorder)
	}{
		{
			name: "the new-agent form",
			open: func(t *testing.T, m model) model {
				workflow := m.listing.workflows[0]

				values := agentPrefill(workflow)
				values.Prompt = "check CI"
				values.Title = "ci"
				values.Harness = "codex"

				opened, _ := m.openAgentForm(workflow, &values)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdAgent)
				assert.Equal(t, "check CI", sent.createdAgent.GetPrompt())
				assert.Equal(t, "ci", sent.createdAgent.GetTitle())
				assert.Equal(t, "codex", sent.createdAgent.GetHarness(), "the workflow's harness is prefilled and can be overridden")
				assert.Equal(t, "/work/api", sent.createdAgent.GetDir(), "the workflow's directory is prefilled and kept")
				assert.Empty(t, sent.createdAgent.GetFlags(), "the workflow's flags are inherited at launch rather than copied into the field")
				assert.Equal(t, "w1", sent.createdAgent.GetWorkflowId())
				assert.Nil(t, sent.createdAgent.ProvisionEnabled, "left on the default, the agent takes no override of its own")
			},
		},
		{
			name: "the child form",
			open: func(t *testing.T, m model) model {
				parent := agentstore.Agent{
					ID:         "a1",
					Name:       "author",
					WorkflowID: "w1",
					Cwd:        "/work/api",
					Harness:    "claude",
					Flags:      "--model opus",
				}

				values := childPrefill(parent)
				values.Prompt = "run the tests"

				opened, _ := m.openChildForm(parent, &values)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdAgent)
				assert.Equal(t, "run the tests", sent.createdAgent.GetPrompt())
				assert.Equal(t, "a1", sent.createdAgent.GetParentAgentId())
				assert.Equal(t, "claude", sent.createdAgent.GetHarness())
				assert.Empty(t, sent.createdAgent.GetFlags(), "the parent's flags are written for the CLI it runs")
			},
		},
		{
			name: "the rename form",
			open: func(t *testing.T, m model) model {
				name := "api-v2"

				opened, _ := m.openRenameForm(agentstore.Agent{ID: "a1", Name: "api-refactor"}, &name)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.renamedAgent)
				assert.Equal(t, "a1", sent.renamedAgent.GetId())
				assert.Equal(t, "api-v2", sent.renamedAgent.GetName())
			},
		},
		{
			name: "the new-workflow form",
			open: func(t *testing.T, m model) model {
				values := workflowFormValues{
					Name:        "web",
					Context:     "the frontend",
					Cwd:         "/work/web",
					Flags:       "--model sonnet",
					Provisioner: provisionerNone,
				}

				opened, _ := m.openWorkflowConfigForm(&values, nil)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdWorkflow)
				assert.Equal(t, "web", sent.createdWorkflow.GetName())
				assert.Equal(t, "the frontend", sent.createdWorkflow.GetContext())
				assert.Equal(t, "/work/web", sent.createdWorkflow.GetCwd())
				assert.Equal(t, "--model sonnet", sent.createdWorkflow.GetFlags())
				assert.False(t, sent.createdWorkflow.GetWorktree().GetEnabled(), "None provisions nothing")
			},
		},
		{
			name: "the new-workflow form with worktree enabled",
			open: func(t *testing.T, m model) model {
				values := workflowFormValues{
					Name:                 "web",
					Cwd:                  "/work/web",
					WorktreeBaseBranch:   "develop",
					WorktreeCopyPaths:    ".env\n\nconfig/local.json\n",
					WorktreeSymlinkPaths: "node_modules",
					WorktreeDiffExclude:  "proto/gen\n*.pb.go",
				}

				opened, _ := m.openWorkflowConfigForm(&values, nil)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.createdWorkflow)
				worktree := sent.createdWorkflow.GetWorktree()
				require.NotNil(t, worktree)
				assert.True(t, worktree.GetEnabled())
				assert.Equal(t, "develop", worktree.GetBaseBranch())
				assert.Equal(t, []string{".env", "config/local.json"}, worktree.GetCopyPaths(), "a blank line between two paths is dropped rather than sent as an entry")
				assert.Equal(t, []string{"node_modules"}, worktree.GetSymlinkPaths())
				assert.Equal(t, []string{"proto/gen", "*.pb.go"}, worktree.GetDiffExclude())
			},
		},
		{
			name: "the workflow edit form",
			open: func(t *testing.T, m model) model {
				target := m.listing.workflows[0]

				values := workflowFormValuesFrom(workflowstore.Config{
					Name:     target.Name,
					Context:  "shared notes",
					Cwd:      target.Cwd,
					Flags:    target.Flags,
					Worktree: workflowstore.WorktreeConfig{Enabled: true},
				})
				values.Cwd = "/work/api-v2"

				opened, _ := m.openWorkflowConfigForm(&values, &target)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.updatedWorkflow)
				assert.Equal(t, "w1", sent.updatedWorkflow.GetId())
				assert.Equal(t, "/work/api-v2", sent.updatedWorkflow.GetCwd(), "the field that was edited")
				assert.Equal(t, "api", sent.updatedWorkflow.GetName(), "and the ones that were not")
				assert.Equal(t, "shared notes", sent.updatedWorkflow.GetContext())
				assert.Equal(t, "--model opus", sent.updatedWorkflow.GetFlags())
				assert.True(t, sent.updatedWorkflow.GetWorktree().GetEnabled(), "the stored choice survives a round trip through the field")
			},
		},
		{
			name: "the workflow rename form",
			open: func(t *testing.T, m model) model {
				values := workflowFormValues{Name: "backend"}

				opened, _ := m.openWorkflowRename(m.listing.workflows[0], &values)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.updatedWorkflow)
				assert.Equal(t, "backend", sent.updatedWorkflow.GetName())
				assert.Equal(t, "/work/api", sent.updatedWorkflow.GetCwd(), "a rename carries the config it is not changing")
			},
		},
		{
			name: "the new-schedule form",
			open: func(t *testing.T, m model) model {
				values := schedulestore.Config{
					Enabled:    true,
					WorkflowID: m.listing.workflows[0].ID,
					Name:       "sweeper",
					Cron:       "*/5 * * * *",
					Prompt:     "sweep",
					HideAgents: true,
				}

				opened, _ := m.openScheduleForm(&values, nil)

				return asModel(t, opened)
			},
			submit: model.answerForm,
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
			open: func(t *testing.T, m model) model {
				target := schedulestore.Schedule{
					ID:         "s1",
					Name:       "sweeper",
					Cron:       "*/5 * * * *",
					Prompt:     "sweep",
					Enabled:    true,
					HideAgents: true,
					WorkflowID: "w1",
				}

				values := schedulePrefill(target)
				values.Prompt = "sweep harder"

				opened, _ := m.openScheduleForm(&values, &target)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.updatedSchedule)
				assert.Equal(t, "s1", sent.updatedSchedule.GetId())
				assert.Equal(t, "sweep harder", sent.updatedSchedule.GetPrompt(), "the field that was edited")
				assert.True(t, sent.updatedSchedule.GetHideAgents(), "and the flag it was opened with")
			},
		},
		{
			name: "the workflow archive prompt",
			open: func(t *testing.T, m model) model {
				opened, _ := m.archiveWorkflowConfirm(m.listing.workflows[0])

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedWorkflow)
				assert.Equal(t, "w1", sent.archivedWorkflow.GetId())
			},
		},
		{
			name: "the agent archive prompt",
			open: func(t *testing.T, m model) model {
				opened, _ := m.archiveAgentsConfirm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}})

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedAgents)
				assert.Equal(t, []string{"a1"}, sent.archivedAgents.GetIds())
				assert.Nil(t, sent.deletedAgents, "archiving alone keeps the record")
			},
		},
		{
			name: "the agent archive prompt answered archive and delete",
			open: func(t *testing.T, m model) model {
				opened, _ := m.archiveAgentsConfirm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}})
				m = asModel(t, opened)

				// The prompt opens on Archive, so the second answer is one step down the list.
				pump(t, m.form.form, keyMsg("down"))

				return m
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedAgents)
				assert.Equal(t, []string{"a1"}, sent.archivedAgents.GetIds())
				require.NotNil(t, sent.deletedAgents, "the delete follows the archive")
				assert.Equal(t, []string{"a1"}, sent.deletedAgents.GetIds())
			},
		},
		{
			name: "the schedule delete prompt",
			open: func(t *testing.T, m model) model {
				m.schedules.items = []scheduleItem{{schedule: schedulestore.Schedule{ID: "s1", Name: "sweeper"}}}

				opened, _ := m.archiveScheduleConfirm()

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.archivedSchedule)
				assert.Equal(t, "s1", sent.archivedSchedule.GetId())
			},
		},
		{
			name: "the archive delete prompt",
			open: func(t *testing.T, m model) model {
				m.archive.agents = []agentstore.Agent{{ID: "a1", Name: "api-refactor"}}

				opened, _ := m.deleteFromArchiveConfirm(m.archive.agents)

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedAgents)
				assert.Equal(t, []string{"a1"}, sent.deletedAgents.GetIds())
			},
		},
		{
			name: "the workflow delete prompt",
			open: func(t *testing.T, m model) model {
				opened, _ := m.deleteWorkflowConfirm(archivedWorkflow("w1", "backend"))

				return asModel(t, opened)
			},
			submit: model.answerForm,
			check: func(t *testing.T, sent *formRecorder) {
				require.NotNil(t, sent.deletedWorkflow)
				assert.Equal(t, "w1", sent.deletedWorkflow.GetId())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{}
			m := tt.open(t, formModel(sent))

			updated, cmd := tt.submit(m)
			submitted, ok := updated.(model)
			require.True(t, ok)

			got := settle(t, submitted, cmd)

			require.Empty(t, got.messages.items, "the service accepted the write")
			assert.False(t, got.form.isActive(), "a write the service took closes the form")
			tt.check(t, sent)
		})
	}
}

// TestNewAgentFormWorktreeOverride covers the one thing an agent can say for itself: left on
// the default it takes no override at all, and choosing None is sent as one.
func TestNewAgentFormWorktreeOverride(t *testing.T) {
	tests := []struct {
		name     string
		chosen   string
		wantSent *bool
	}{
		{
			name:     "left on the default, nothing is sent",
			chosen:   inheritWorktree,
			wantSent: nil,
		},
		{
			name:     "choosing none is sent as an override, not as nothing",
			chosen:   withoutWorktree,
			wantSent: xptr.Ptr(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{}
			m := formModel(sent)
			m.listing.workflows[0].Worktree = workflowstore.WorktreeConfig{Enabled: true}
			workflow := m.listing.workflows[0]

			values := agentPrefill(workflow)

			opened, _ := m.openAgentForm(workflow, &values)
			m = asModel(t, opened)

			values.Worktree = tt.chosen

			updated, cmd := m.answerForm()
			submitted, ok := updated.(model)
			require.True(t, ok)
			settle(t, submitted, cmd)

			require.NotNil(t, sent.createdAgent)
			assert.Equal(t, tt.wantSent, sent.createdAgent.ProvisionEnabled)
		})
	}
}

// TestWorktreeSelectShowsTheCurrentChoiceOnOpen covers the field opening on a visible option.
func TestWorktreeSelectShowsTheCurrentChoiceOnOpen(t *testing.T) {
	agentForm, _ := newAgentForm(config.FixedFormKeys(),
		&agentFormValues{Worktree: inheritWorktree},
		true,
	)
	agentForm.Init()

	assert.Contains(t, agentForm.View(), "> Same as workflow", "the field opens with what it inherits marked as picked")
}

// TestRefusedSubmitKeepsTheForm covers a refused write leaving the form up with what was
// filled in.
func TestRefusedSubmitKeepsTheForm(t *testing.T) {
	tests := []struct {
		name   string
		open   func(t *testing.T, m model) (model, func() string)
		submit func(m model) (tea.Model, tea.Cmd)
		form   formKind
		typed  string
	}{
		{
			name: "the new-agent form, with no provisioner to run",
			open: func(t *testing.T, m model) (model, func() string) {
				workflow := m.listing.workflows[0]

				values := agentPrefill(workflow)
				values.Prompt = "check CI"

				opened, _ := m.openAgentForm(workflow, &values)

				return asModel(t, opened), func() string { return values.Prompt }
			},
			submit: func(m model) (tea.Model, tea.Cmd) { return m.updateForm(keyMsg("ctrl+s")) },
			form:   formAgent,
			typed:  "check CI",
		},
		{
			name: "the rename form",
			open: func(t *testing.T, m model) (model, func() string) {
				name := "api-v2"

				opened, _ := m.openRenameForm(agentstore.Agent{ID: "a1", Name: "api-refactor"}, &name)
				m = asModel(t, opened)
				m.form.form.State = huh.StateCompleted

				return m, func() string { return name }
			},
			submit: model.answerForm,
			form:   formRename,
			typed:  "api-v2",
		},
		{
			name: "the new-workflow form",
			open: func(t *testing.T, m model) (model, func() string) {
				values := workflowFormValues{Name: "web"}

				opened, _ := m.openWorkflowConfigForm(&values, nil)
				m = asModel(t, opened)
				m.form.form.State = huh.StateCompleted

				return m, func() string { return values.Name }
			},
			submit: model.answerForm,
			form:   formWorkflow,
			typed:  "web",
		},
		{
			name: "the new-schedule form",
			open: func(t *testing.T, m model) (model, func() string) {
				values := schedulestore.Config{
					Enabled:    true,
					WorkflowID: "w1",
					Cron:       "*/5 * * * *",
				}

				opened, _ := m.openScheduleForm(&values, nil)

				return asModel(t, opened), func() string { return values.Cron }
			},
			submit: model.answerForm,
			form:   formSchedule,
			typed:  "*/5 * * * *",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sent := &formRecorder{refuse: connect.NewError(connect.CodeInvalidArgument, errors.New("not allowed"))}
			m, held := tt.open(t, formModel(sent))

			updated, cmd := tt.submit(m)
			got := settle(t, asModel(t, updated), cmd)

			require.NotEmpty(t, got.messages.items, "a refused write is reported")
			assert.Equal(t, "not allowed", newestMessage(got).detail)
			assert.Equal(t, "fix and submit again", newestMessage(got).hint, "esc backs out of the form rather than clearing the banner")
			assert.Equal(t, tt.form, got.form.kind, "the form is still up and owns the keys")
			assert.Equal(t, tt.typed, held(), "and still holds what was filled in")
			assert.NotEqual(t, huh.StateCompleted, got.form.form.State, "a completed form draws as nothing, so the form is built back")
			assert.NotEmpty(t, got.form.form.View())
			assert.False(t, got.held.active, "and takes keys again, since the write it was waiting on has landed")
		})
	}
}

// TestNewAgentSubmitHoldsTheForm covers the new-agent form staying up with the submitting
// notice over it until the create lands.
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
			m.listing.workflows[0].Worktree = workflowstore.WorktreeConfig{Enabled: true}

			workflow := m.listing.workflows[0]

			values := agentPrefill(workflow)
			values.Prompt = "check CI"

			opened, _ := m.openAgentForm(workflow, &values)
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

// TestProvisionerRebuildsTheForm checks that switching the provisioner swaps the worktree
// fields for the hook pickers by rebuilding the form, and hands back whatever was typed into
// the worktree fields if the provisioner switches back.
func TestProvisionerRebuildsTheForm(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)

	values := workflowFormValues{}

	opened, _ := m.openWorkflowConfigForm(&values, nil)
	m = asModel(t, opened)

	_, order := workflowConfigForm(workflowConfigParams{
		NameTitle: "New workflow name",
		Values:    &values,
		Keys:      formKeys(),
	})
	for range slices.Index(order, provisionerKey) {
		m.form.form.NextField()
	}

	worktreeView := m.form.form.View()
	assert.Equal(t, provisionerWorktree, values.Provisioner, "worktree is the default")
	assert.Contains(t, worktreeView, "Worktree base branch", "worktree is the default, so its fields are already on screen")
	assert.Contains(t, worktreeView, "Worktree copy paths")
	assert.Contains(t, worktreeView, "Worktree symlink paths")

	values.WorktreeCopyPaths = ".env"

	updated, _ := m.form.update(m, keyMsg("up"))
	m = asModel(t, updated)

	require.Equal(t, provisionerNone, values.Provisioner, "the keypress switched it")
	noneView := m.form.form.View()
	assert.NotContains(t, noneView, "Worktree base branch", "None provisions nothing, so it asks nothing about a worktree")
	assert.Contains(t, noneView, "Pre-launch hook", "the hooks are asked for whichever provisioner is chosen")

	updated, _ = m.form.update(m, keyMsg("down"))
	m = asModel(t, updated)

	assert.Equal(t, provisionerWorktree, values.Provisioner)
	assert.Equal(t, ".env", values.WorktreeCopyPaths, "switching back hands back what was typed before")
	assert.Contains(t, m.form.form.View(), "Worktree copy paths")
}

// TestProvisionerSelectShowsEveryOptionOnOpen covers a select opening on the non-default
// answer.
func TestProvisionerSelectShowsEveryOptionOnOpen(t *testing.T) {
	form, order := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &workflowFormValues{Provisioner: provisionerNone},
		Keys:      config.FixedFormKeys(),
	})
	form.Init()
	for range slices.Index(order, provisionerKey) {
		form.NextField()
	}

	view := form.View()
	assert.Contains(t, view, "None: agents keep the directory they are given", "neither option is hidden by the other being picked")
	assert.Contains(t, view, "Worktree: a git worktree per agent")
	assert.Less(t,
		strings.Index(view, "None: agents keep the directory they are given"),
		strings.Index(view, "Worktree: a git worktree per agent"),
		"the options keep their order regardless of which is selected",
	)
}

// TestArchivePromptChoices covers the three ways the prompt can be answered, where it opens
// on the reversible one so a submit alone never destroys anything.
func TestArchivePromptChoices(t *testing.T) {
	tests := []struct {
		name         string
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

			require.Empty(t, got.messages.items)
			assert.Equal(t, screenAgents, got.screen, "every answer lands back on the list")
			assert.Equal(t, tt.wantArchived, sent.archivedAgents.GetIds())
			assert.Equal(t, tt.wantDeleted, sent.deletedAgents.GetIds())
		})
	}
}

// TestArchivePromptOpensOnArchive covers the prompt as first drawn.
func TestArchivePromptOpensOnArchive(t *testing.T) {
	form, choice := archiveAgentsForm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}}, 0, config.FixedFormKeys())
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

	// The prompt opens on Archive, so archive-and-delete is one step down the list.
	pump(t, m.form.form, keyMsg("down"))
	m.form.form.State = huh.StateCompleted

	submitted, cmd := m.form.update(m, keyMsg("enter"))
	got := settle(t, asModel(t, submitted), cmd)

	require.NotEmpty(t, got.messages.items, "the refusal is reported")
	assert.Equal(t, `Could not archive and delete agent "api-refactor"`, newestMessage(got).summary)
	assert.Nil(t, sent.deletedAgents, "nothing was destroyed")
}

// TestConfirmingArchiveTracksTheWriteAsPending covers the write being issued on a value
// receiver, so the model it mutates has to be the one the confirm form returns.
func TestConfirmingArchiveTracksTheWriteAsPending(t *testing.T) {
	sent := &formRecorder{}

	opened, _ := formModel(sent).archiveAgentsConfirm([]agentstore.Agent{{ID: "a1", Name: "api-refactor"}})
	m := asModel(t, opened)
	m.form.form.State = huh.StateCompleted

	submitted, cmd := m.form.update(m, keyMsg("enter"))
	got := asModel(t, submitted)

	assert.Positive(t, got.pending, "the in-flight write is tracked on the model that comes back")
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

// TestFormResultsAreNotWhatIsSubmitted pins a field reaching huh's own results only as the
// cursor leaves it.
func TestFormResultsAreNotWhatIsSubmitted(t *testing.T) {
	values := schedulestore.Config{
		Name:       "sweeper",
		Cron:       "*/5 * * * *",
		Prompt:     "sweep",
		Enabled:    true,
		HideAgents: true,
	}

	form, _ := scheduleForm(&values, nil, config.FixedFormKeys())
	form.Init()

	assert.Empty(t, form.GetString("name"), "huh has recorded nothing the cursor has not left")
	assert.False(t, form.GetBool("hide_agents"))

	assert.Equal(t, "sweeper", values.Name, "the bound values are what a submit reads")
	assert.True(t, values.HideAgents)
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
		listing: listing{workflows: []workflowstore.Workflow{{
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
	storedWorkflow      *workflowv1.Workflow
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

func (f *formRecorder) GetWorkflow(_ context.Context, _ *connect.Request[workflowv1.GetWorkflowRequest]) (*connect.Response[workflowv1.GetWorkflowResponse], error) {
	if f.storedWorkflow == nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such workflow"))
	}

	return connect.NewResponse(&workflowv1.GetWorkflowResponse{Workflow: f.storedWorkflow}), nil
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

// TestOnlyTheSubmitKeySubmitsAForm covers enter never submitting.
func TestOnlyTheSubmitKeySubmitsAForm(t *testing.T) {
	keys := config.FixedFormKeys()

	agent, _ := newAgentForm(keys, &agentFormValues{}, true)
	agent.Init()
	for range 6 {
		agent.Update(huh.NextField())
	}

	pump(t, agent, keyMsg("enter"))
	assert.NotEqual(t, huh.StateCompleted, agent.State, "enter on the last field leaves the form alone")

	rename, _ := renameForm("api-refactor", new(string), keys)
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

	values := agentPrefill(workflow)
	values.Prompt = "check CI"

	opened, _ := m.openAgentForm(workflow, &values)
	m = asModel(t, opened)
	m.held.active = true

	updated, cmd := m.Update(keyMsg("ctrl+s"))
	got := settle(t, asModel(t, updated), cmd)

	assert.Nil(t, sent.createdAgent, "the key never reached the submit path")
	assert.Equal(t, formAgent, got.form.kind, "the form is still up")
	assert.Equal(t, "check CI", values.Prompt, "and the form is as it was")
}

func pump(t *testing.T, form *huh.Form, msg tea.Msg) {
	t.Helper()

	queue := []tea.Msg{msg}
	for step := 0; len(queue) > 0 && step < 10; step++ {
		_, cmd := form.Update(queue[0])
		queue = append(queue[1:], produced(cmd)...)
	}
}

// TestAdoptFormSendsWhatWasTyped covers the adopt form having no directory field.
func TestAdoptFormSendsWhatWasTyped(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)
	workflow := m.listing.workflows[0]

	values := adoptPrefill(workflow)
	assert.Equal(t, "claude", values.Harness, "the workflow's harness is prefilled")
	assert.Empty(t, values.Flags, "its flags are not, being written for the CLI it names")

	values.Ref = "https://github.com/acmecorp/admin/pull/1069"
	values.Prompt = "pick it up"

	opened, _ := m.openAdoptForm(workflow, &values)
	adopting := asModel(t, opened)
	require.Equal(t, formAdopt, adopting.form.kind)

	updated, cmd := adopting.answerForm()
	submitted, ok := updated.(model)
	require.True(t, ok)
	settle(t, submitted, cmd)

	require.NotNil(t, sent.adoptedAgent)
	assert.Equal(t, "https://github.com/acmecorp/admin/pull/1069", sent.adoptedAgent.GetRef())
	assert.Equal(t, "pick it up", sent.adoptedAgent.GetPrompt())
	assert.Equal(t, "w1", sent.adoptedAgent.GetWorkflowId())
	assert.Nil(t, sent.createdAgent, "adoption does not go through create")
}

// TestAdoptKeyOpensTheForm covers the binding, which acts on a workflow header: adoption
// goes into a workflow, so the row it answers on is the workflow rather than an agent
// already inside it.
func TestAdoptKeyOpensTheForm(t *testing.T) {
	sent := &formRecorder{}
	m := infoModel(&recordingTmux{})
	m.api = api{agents: sent}
	m.tree.cursor = 0

	entered, _ := m.updateAgents(keyMsg("a"))
	opened := asModel(t, entered)

	require.Equal(t, formAdopt, opened.form.kind)

	submitted, cmd := opened.answerForm()
	settle(t, asModel(t, submitted), cmd)

	require.NotNil(t, sent.adoptedAgent)
	assert.Equal(t, "w1", sent.adoptedAgent.GetWorkflowId(), "it adopts into the workflow under the cursor")
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

// TestWorkflowFormRoundTripsEveryHookMoment pins the form editing every moment there is.
func TestWorkflowFormRoundTripsEveryHookMoment(t *testing.T) {
	hooks := []workflowstore.Hook{
		{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
		{Event: workflowstore.HookPostLaunch, Script: "announce"},
		{Event: workflowstore.HookPostResume, Script: "announce-back"},
		{Event: workflowstore.HookPostRestore, Script: "unlock"},
		{Event: workflowstore.HookPreTeardown, Script: "take-the-stack-down"},
	}

	values := workflowFormValuesFrom(workflowstore.Config{Name: "backend", Hooks: hooks})

	assert.Equal(t, "install-deps", values.PreLaunchHook, "a moment's hook is what its select is sitting on")
	assert.Equal(t, hooks, values.toConfig().Hooks, "and every moment comes back as it went in")
}

// TestWorkflowFormKeepsTheFirstHookOnAMomentHoldingTwo pins what the form does with a workflow
// written when a moment could hold several.
func TestWorkflowFormKeepsTheFirstHookOnAMomentHoldingTwo(t *testing.T) {
	values := workflowFormValuesFrom(workflowstore.Config{
		Name: "backend",
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPreLaunch, Script: "copy-env"},
		},
	})

	assert.Equal(t, "install-deps", values.PreLaunchHook, "the one that ran first is the one on offer")
	assert.Equal(t, []workflowstore.Hook{
		{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
	}, values.toConfig().Hooks, "and saving the form writes the one moment holds")
}

// TestHookFieldsOfferTheHooksDirectory pins the picker being a select over the hooks directory
// rather than free text, and opening on what the workflow already names.
func TestHookFieldsOfferTheHooksDirectory(t *testing.T) {
	values := workflowFormValuesFrom(workflowstore.Config{
		Name:  "backend",
		Hooks: []workflowstore.Hook{{Event: workflowstore.HookPreLaunch, Script: "copy-env"}},
	})

	form, _ := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &values,
		Hooks:     []hookChoice{{name: "install-deps"}, {name: "copy-env"}},
		Keys:      config.FixedFormKeys(),
	})
	sizeForm(form, 40)

	view := form.View()
	assert.Contains(t, view, "Pre-launch hook")
	assert.Contains(t, view, "install-deps", "every hook in the directory is offered")
	assert.Contains(t, view, "copy-env")
	assert.Equal(t, "copy-env", values.PreLaunchHook, "and the one it already names is the one picked")
}

// TestHookFieldsDrawEveryOptionThatFits pins a moment already naming a hook still offering the
// options above it.
func TestHookFieldsDrawEveryOptionThatFits(t *testing.T) {
	values := workflowFormValuesFrom(workflowstore.Config{
		Name: "backend",
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "copy-env"},
			{Event: workflowstore.HookPostLaunch, Script: "copy-env"},
			{Event: workflowstore.HookPostResume, Script: "copy-env"},
			{Event: workflowstore.HookPostRestore, Script: "copy-env"},
			{Event: workflowstore.HookPreTeardown, Script: "copy-env"},
		},
	})

	form, _ := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &values,
		Hooks:     []hookChoice{{name: "install-deps"}, {name: "copy-env"}},
		Keys:      config.FixedFormKeys(),
	})
	sizeForm(form, 60)

	view := form.View()
	assert.Equal(t, len(workflowstore.HookEvents()), strings.Count(view, "install-deps"),
		"every moment offers the hook above the one it names")
	assert.NotContains(t, view, "↑/↓ for more", "and says nothing about scrolling for a list it draws whole")
}

// TestHookFieldsSayWhenTheListScrolls pins a hooks directory too long to draw whole telling a
// person the list carries on.
func TestHookFieldsSayWhenTheListScrolls(t *testing.T) {
	available := []hookChoice{
		{name: "copy-env"},
		{name: "install-deps"},
		{name: "seed-db"},
		{name: "warm-cache"},
		{name: "link-modules"},
	}

	values := workflowFormValues{Provisioner: provisionerNone}

	form, _ := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &values,
		Hooks:     available,
		Keys:      config.FixedFormKeys(),
	})
	sizeForm(form, 60)

	assert.Contains(t, form.View(), "↑/↓ for more")
}

// TestHookOptionsKeepAHookThatHasGone pins a hook the workflow names but the directory no
// longer holds still being offered.
func TestHookOptionsKeepAHookThatHasGone(t *testing.T) {
	options := hookOptions([]hookChoice{{name: "install-deps"}}, "copy-env")

	require.Len(t, options, 3)
	assert.Empty(t, options[0].Value, "None leads, so huh opens the list at the top")
	assert.Equal(t, "install-deps", options[1].Value)
	assert.Equal(t, "copy-env", options[2].Value, "the missing one keeps its name as the value so it round-trips")
	assert.Contains(t, options[2].Key, "missing")
}

// TestHookFieldsSayWhereHooksGoWhenThereAreNone pins the empty case reading as an empty
// directory rather than a broken form.
func TestHookFieldsSayWhereHooksGoWhenThereAreNone(t *testing.T) {
	// None keeps the worktree fields off the screen and the hook fields on it.
	values := workflowFormValues{Provisioner: provisionerNone}

	form, _ := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &values,
		Keys:      config.FixedFormKeys(),
	})
	sizeForm(form, 40)

	assert.Contains(t, form.View(), "none found in your hooks directory")
}

// TestWorkflowFormScrollsToFitTheTerminal pins the form being given the room it has rather
// than drawing past the bottom of the screen.
func TestWorkflowFormScrollsToFitTheTerminal(t *testing.T) {
	values := workflowFormValues{Provisioner: provisionerWorktree}

	const terminal = 24

	form, _ := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &values,
		Hooks:     []hookChoice{{name: "install-deps"}},
		Keys:      config.FixedFormKeys(),
	})
	sizeForm(form, terminal)

	// huh draws its help line outside the height it is told about, which is the one row a form
	// drawn in the whole of a short pane runs over by.
	lines := strings.Count(form.View(), "\n") + 1
	assert.LessOrEqual(t, lines, terminal+1, "the form is drawn inside the terminal it was sized against")
	assert.Contains(t, form.View(), "Workflow name", "and opens at the top of itself")
}

// formColumnWidth is what these forms are measured in, which is about what the panel's own
// column comes to on a wide terminal.
const formColumnWidth = 80

// sizeForm starts a form and tells it how much room it has, which is how the panel sizes one.
func sizeForm(form *huh.Form, height int) {
	form.Init()
	form.Update(tea.WindowSizeMsg{Width: formColumnWidth, Height: height})
}

// TestHookFieldsSurviveBeingWalkedThrough pins the fields not rewriting themselves as the
// cursor passes over them.
func TestHookFieldsSurviveBeingWalkedThrough(t *testing.T) {
	values := workflowFormValuesFrom(workflowstore.Config{
		Name:     "backend",
		Worktree: workflowstore.WorktreeConfig{Enabled: true},
		Hooks: []workflowstore.Hook{
			{Event: workflowstore.HookPreLaunch, Script: "install-deps"},
			{Event: workflowstore.HookPostLaunch, Script: "announce"},
		},
	})
	before := values.toConfig().Hooks

	form, _ := workflowConfigForm(workflowConfigParams{
		NameTitle: "Workflow name",
		Values:    &values,
		Hooks:     []hookChoice{{name: "announce"}, {name: "copy-env"}, {name: "install-deps"}},
		Keys:      config.FixedFormKeys(),
	})
	sizeForm(form, 40)

	// Every field, so each hook select is focused and blurred in turn.
	for range 20 {
		form.Update(huh.NextField())
	}

	assert.Equal(t, before, values.toConfig().Hooks, "walking the form leaves every moment as it was")
}

// TestEscDetachesFromAHeldForm covers esc during a create: the panel comes back rather than
// holding every key until a provisioner that may run for minutes has finished, the write still
// lands the agent it was sent for, and the landing leaves the person where they went.
func TestEscDetachesFromAHeldForm(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)

	workflow := m.listing.workflows[0]

	values := agentPrefill(workflow)
	values.Prompt = "check CI"

	opened, _ := m.openAgentForm(workflow, &values)
	m = asModel(t, opened)

	updated, cmd := m.answerForm()
	m = asModel(t, updated)
	require.True(t, m.held.active)

	detached, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, detached)

	assert.False(t, m.held.active, "the panel takes keys again")
	assert.False(t, m.form.isActive(), "and the form it was holding is gone")

	m.tree.focus = agentRowKey("somewhere-else")

	got := settle(t, m, cmd)

	assert.Empty(t, got.messages.items)
	require.NotNil(t, sent.createdAgent, "the create the form was submitted for still ran")
	assert.Equal(t, agentRowKey("somewhere-else"), got.tree.focus, "and it lands without taking the cursor back")
}

// TestAnEarlierWriteLeavesALaterHoldAlone covers escaping a slow create and submitting another:
// the first one landing must not tear down the form the second is still being held for.
func TestAnEarlierWriteLeavesALaterHoldAlone(t *testing.T) {
	sent := &formRecorder{}
	m := formModel(sent)

	workflow := m.listing.workflows[0]

	first := agentPrefill(workflow)
	first.Prompt = "the slow one"

	opened, _ := m.openAgentForm(workflow, &first)
	m = asModel(t, opened)

	submitted, _ := m.answerForm()
	m = asModel(t, submitted)

	escaped := m.epoch

	detached, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = asModel(t, detached)

	second := agentPrefill(workflow)
	second.Prompt = "the second one"

	opened, _ = m.openAgentForm(workflow, &second)
	m = asModel(t, opened)

	submitted, _ = m.answerForm()
	m = asModel(t, submitted)

	require.True(t, m.held.active)
	require.NotEqual(t, escaped, m.held.epoch, "the two submits are told apart by their own epochs")

	landed, _ := m.applyWrite(writeResult{
		summary: "Could not start the agent",
		written: &written{row: agentRowKey("new")},
		epoch:   escaped,
	})
	got := asModel(t, landed)

	assert.True(t, got.held.active, "the escaped create landing leaves the second form held")
	assert.True(t, got.form.isActive())
	assert.Empty(t, got.tree.focus, "and an escaped create takes nobody to the agent it made")
}

// TestSubmittingNoticeCountsUp pins that a held form's notice stands still while the write is
// quick and starts counting once it is not, so a slow provisioner does not read as a wedge.
func TestSubmittingNoticeCountsUp(t *testing.T) {
	since := time.Now()

	assert.Equal(t, "  submitting…", submittingNotice(time.Time{}, since))
	assert.Equal(t, "  submitting…", submittingNotice(since, since.Add(time.Second)))
	assert.Equal(t, "  submitting… 12s · esc to keep working", submittingNotice(since, since.Add(12*time.Second)))
	assert.Equal(t, "  submitting… 6m40s · esc to keep working", submittingNotice(since, since.Add(400*time.Second)))
}

// TestRefreshDescriptionSaysWhatWillHappen covers the sentence somebody reads before pressing,
// which has to name what becomes of the branch rather than only what the operation is called.
func TestRefreshDescriptionSaysWhatWillHappen(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		plan     refreshPlan
		want     []string
		wantNone []string
	}{
		{
			name: "a branch with nothing of its own is just moved",
			plan: refreshPlan{
				baseRef: "origin/main",
			},
			want:     []string{"no commits of its own", "moved to origin/main"},
			wantNone: []string{"replayed"},
		},
		{
			name: "a branch the base already holds loses those commits",
			plan: refreshPlan{
				baseRef: "origin/main",
				landed:  3,
			},
			want: []string{"already holds every commit", "3 commits will leave the branch"},
		},
		{
			name: "an ordinary rebase says what it replays",
			plan: refreshPlan{
				baseRef: "origin/main",
				replay:  2,
			},
			want:     []string{"2 commits of its own will be replayed onto origin/main"},
			wantNone: []string{"leave the branch"},
		},
		{
			name: "a partly landed branch says both halves",
			plan: refreshPlan{
				baseRef: "origin/main",
				landed:  2,
				replay:  1,
			},
			want: []string{"1 commit of its own will be replayed", "Another 2 the base already holds"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			description := refreshDescription(tt.plan)
			for _, want := range tt.want {
				assert.Contains(t, description, want)
			}
			for _, none := range tt.wantNone {
				assert.NotContains(t, description, none)
			}
		})
	}
}

// TestRefreshDescriptionAlwaysSaysWhatItDoesToTheFiles pins the half a person is most likely to
// be wrong about.
func TestRefreshDescriptionAlwaysSaysWhatItDoesToTheFiles(t *testing.T) {
	t.Parallel()

	for _, plan := range []refreshPlan{
		{baseRef: "origin/main"},
		{baseRef: "origin/main", landed: 3},
		{baseRef: "origin/main", replay: 2},
	} {
		description := refreshDescription(plan)
		assert.Contains(t, description, "files will change under it")
		assert.Contains(t, description, "nothing untracked is removed")
		assert.Contains(t, description, "copy of the worktree is kept")
	}
}

// TestRefreshAffirmativeNamesWhatRuns covers the button naming what actually runs.
func TestRefreshAffirmativeNamesWhatRuns(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Rebase", refreshAffirmative(refreshPlan{replay: 2}))
	assert.Equal(t, "Move", refreshAffirmative(refreshPlan{landed: 3}))
	assert.Equal(t, "Move", refreshAffirmative(refreshPlan{}))
}

// TestRefreshWarning covers each thing the confirmation is there to say, and the ordinary case
// saying nothing, since a warning shown every time is one nobody reads.
func TestRefreshWarning(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		plan     refreshPlan
		want     []string
		wantNone bool
	}{
		{
			name:     "a rebase that would run cleanly warns about nothing",
			plan:     refreshPlan{replay: 2},
			wantNone: true,
		},
		{
			name:     "the landed commits are left to the description, which says what becomes of them",
			plan:     refreshPlan{landed: 2},
			wantNone: true,
		},
		{
			name: "merge commits, which a rebase throws away before replaying what they settled",
			plan: refreshPlan{merges: 2},
			want: []string{"throws away 2 merge commits"},
		},
		{
			name: "the paths the two sides both changed",
			plan: refreshPlan{conflictPaths: []string{"a.go", "b.go"}},
			want: []string{"Both sides changed a.go, b.go"},
		},
		{
			name: "more paths than are worth reading out",
			plan: refreshPlan{conflictPaths: []string{"a.go", "b.go", "c.go", "d.go", "e.go"}},
			want: []string{"a.go, b.go, c.go and 2 more"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			warning := refreshWarning(tt.plan)
			if tt.wantNone {
				assert.Empty(t, warning)

				return
			}

			for _, want := range tt.want {
				assert.Contains(t, warning, want)
			}
		})
	}
}
