//go:build integration

package ui_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/internal/komradtest"
	"komrad/internal/ui"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	taskv1 "komrad/proto/gen/task/v1"
	viewv1 "komrad/proto/gen/view/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"charm.land/lipgloss/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestIntegrationPanel covers the panel driven against a real service and a real tmux server.
func TestIntegrationPanel(t *testing.T) {
	t.Run("the archive key takes a task's agents all the way to the database", func(t *testing.T) {
		instance, agentID := startServedAgent(t)
		harness := paneltest.Start(t, paneltest.Params{Remote: newPanelRemote(instance)})
		relist(t, instance, harness)
		require.Equal(t, []string{"backend", "└─ login"}, paneltest.ListRows(harness.Frame()))

		require.Equal(t, "└─ login", paneltest.HoveredRow(harness.Frame()))
		harness.Press("space", "a", "enter")

		listed, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{Archived: true}))
		require.NoError(t, err)
		require.Len(t, listed.Msg.GetAgents(), 1)
		assert.Equal(t, agentID, listed.Msg.GetAgents()[0].GetAgent().GetId())
		assert.Empty(t, instance.GetOwnedPane(agentID), "archiving from the panel stops what was running")

		relist(t, instance, harness)
		assert.Equal(t, []string{"backend"}, paneltest.ListRows(harness.Frame()), "and the row goes off the list, leaving the folder it was in")
	})

	t.Run("a staged agent's pane dying leaves the panel a nav pane and a stage pane", func(t *testing.T) {
		served := openServedPanel(t, 0)
		stagePane := served.harness.Tmux().StagePane()

		served.instance.Tmux.Run("kill-pane", "-t", served.agentPane)
		relist(t, served.instance, served.harness)

		assert.False(t, served.instance.Tmux.Client().IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
		assert.Equal(t,
			[]string{served.navPane, served.harness.Tmux().StagePane()},
			readPanelPanes(served.instance),
			"the now-dormant agent's record is in the stage")
	})

	t.Run("archiving a workflow sends its panes back and leaves the divider where it was dragged", func(t *testing.T) {
		served := openServedPanel(t, 0)
		stagePane := served.harness.Tmux().StagePane()
		configured := readPaneWidth(t, served.instance, served.agentPane)
		served.instance.Tmux.Run("resize-pane", "-t", served.navPane, "-x", "60")
		dragged := readPaneWidth(t, served.instance, served.agentPane)
		require.NotEqual(t, configured, dragged, "the drag moved the divider off the configured width")

		served.harness.Press("k", "space", "a")
		require.Contains(t, served.harness.Frame(), "backend › archive")
		served.harness.Press("a")
		relist(t, served.instance, served.harness)

		assert.Empty(t, served.harness.Tmux().GuestPane(), "the archived agent's pane is not left standing in the panel")
		require.True(t, served.instance.Tmux.Client().IsPaneAlive(stagePane), "the panel's own stage pane outlives the archive")
		assert.Equal(t, stagePane, served.harness.Tmux().StagePane(), "so the panel is not left splitting another")
		assert.Equal(t, dragged, readPaneWidth(t, served.instance, stagePane))
	})

	t.Run("leaving a running agent for its folder zooms the panel over the window", func(t *testing.T) {
		served := openServedPanel(t, 0)
		readZoomedFlag := func() string {
			return served.instance.Tmux.Run("display-message", "-p", "-t", served.navPane, "#{window_zoomed_flag}")
		}
		require.Equal(t, "0", readZoomedFlag(), "the agent's own pane has the stage to itself")

		served.harness.Press("k")
		served.harness.Advance(time.Second)

		assert.Equal(t, "1", readZoomedFlag())
	})

	t.Run("the panel renders at the size of the pane it has just been given", func(t *testing.T) {
		served := openServedPanel(t, 341)
		share := readPaneWidth(t, served.instance, served.navPane)
		require.Less(t, share, 341, "the panel starts on its share of the window")

		served.harness.Press("k")
		served.harness.Advance(time.Second)

		require.Equal(t, 341, readPaneWidth(t, served.instance, served.navPane), "the folder row takes the window")
		assert.Equal(t, 341, lipgloss.Width(served.harness.Rendered()), "the frame covers it")

		served.harness.Press("j")
		served.harness.Advance(time.Second)

		require.Equal(t, share, readPaneWidth(t, served.instance, served.navPane), "the agent's own pane takes it back")
		assert.LessOrEqual(t, lipgloss.Width(served.harness.Rendered()), share, "so nothing rendered runs past the pane and wraps")
	})
}

const panelSession = "shown"

type servedPanel struct {
	instance  *komradtest.Instance
	harness   *paneltest.Panel
	agentID   string
	agentPane string
	navPane   string
}

func startServedAgent(t *testing.T) (*komradtest.Instance, string) {
	t.Helper()

	instance := komradtest.Start(t, komradtest.Params{})
	workflow, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agentID := created.Msg.GetAgent().GetId()
	require.NotEmpty(t, instance.GetOwnedPane(agentID))
	instance.WaitForHarness(agentID)

	return instance, agentID
}

func openServedPanel(t *testing.T, windowWidth int) *servedPanel {
	t.Helper()

	instance, agentID := startServedAgent(t)
	instance.Tmux.NewSession(panelSession)
	navPane := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")
	if windowWidth > 0 {
		instance.Tmux.Run("resize-window", "-t", panelSession, "-x", strconv.Itoa(windowWidth), "-y", "50")
	}

	client := instance.Tmux.Client()
	harness := paneltest.Start(t, paneltest.Params{
		Width:    client.GetPaneWidth(navPane),
		Height:   client.GetPaneHeight(navPane),
		Settings: ui.PanelSettings{Pane: navPane},
		Tmux:     client,
		Remote:   newPanelRemote(instance),
	})
	relist(t, instance, harness)
	require.Equal(t, []string{"backend", "└─ login"}, paneltest.ListRows(harness.Frame()))

	harness.Advance(time.Second)
	agentPane := instance.GetOwnedPane(agentID)
	require.Equal(t, agentPane, harness.Tmux().GuestPane(), "the agent's own pane is swapped into the stage")
	require.Contains(t, readPanelPanes(instance), agentPane)

	return &servedPanel{
		instance:  instance,
		harness:   harness,
		agentID:   agentID,
		agentPane: agentPane,
		navPane:   navPane,
	}
}

func newPanelRemote(instance *komradtest.Instance) *paneltest.Remote {
	return &paneltest.Remote{
		Agents:       instance.Agents,
		Workflows:    instance.Workflows,
		Schedules:    instance.Schedules,
		Hooks:        instance.Hooks,
		Worktrees:    instance.Worktrees,
		Forges:       instance.Forges,
		PullRequests: instance.PullRequests,
		Loops:        instance.Loops,
		Tasks:        instance.Tasks,
		Logs:         instance.Logs,
	}
}

func relist(t *testing.T, instance *komradtest.Instance, harness *paneltest.Panel) {
	t.Helper()

	agents, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)
	workflows, err := instance.Workflows.ListWorkflows(t.Context(), connect.NewRequest(&workflowv1.ListWorkflowsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)
	schedules, err := instance.Schedules.ListSchedules(t.Context(), connect.NewRequest(&schedulev1.ListSchedulesRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)

	var tasks []*taskv1.Task
	for _, workflow := range workflows.Msg.GetWorkflows() {
		listed, err := instance.Tasks.ListTasks(t.Context(), connect.NewRequest(&taskv1.ListTasksRequest{WorkflowId: workflow.GetId()}))
		require.NoError(t, err)
		tasks = append(tasks, listed.Msg.GetTasks()...)
	}

	rows := slices.Concat(
		xslice.Map(agents.Msg.GetAgents(), func(row *agentv1.AgentRow) proto.Message {
			return row.GetAgent()
		}),
		xslice.Map(workflows.Msg.GetWorkflows(), func(workflow *workflowv1.Workflow) proto.Message {
			return workflow
		}),
		xslice.Map(schedules.Msg.GetSchedules(), func(schedule *schedulev1.Schedule) proto.Message {
			return schedule
		}),
		xslice.Map(tasks, func(task *taskv1.Task) proto.Message {
			return task
		}),
	)

	harness.Watch(paneltest.Snapshot(rows...))
}

func readPanelPanes(instance *komradtest.Instance) []string {
	var panes []string
	for _, line := range strings.Split(instance.Tmux.Run("list-panes", "-s", "-t", panelSession, "-F", "#{pane_id}"), "\n") {
		if pane := strings.TrimSpace(line); pane != "" {
			panes = append(panes, pane)
		}
	}

	return panes
}

func readPaneWidth(t *testing.T, instance *komradtest.Instance, pane string) int {
	t.Helper()

	width, err := strconv.Atoi(instance.Tmux.Run("display-message", "-p", "-t", pane, "#{pane_width}"))
	require.NoError(t, err)

	return width
}
