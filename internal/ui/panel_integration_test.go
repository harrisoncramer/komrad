//go:build integration

package ui_test

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"komrad/internal/komradtest"
	"komrad/internal/ui"
	"komrad/lib/xslice"
	agentv1 "komrad/proto/gen/agent/v1"
	schedulev1 "komrad/proto/gen/schedule/v1"
	viewv1 "komrad/proto/gen/view/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"charm.land/lipgloss/v2"
	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const panelSession = "shown"

type servedPanel struct {
	instance  *komradtest.Instance
	harness   *panelHarness
	agentID   string
	agentPane string
	navPane   string
}

// TestIntegrationPanel covers the panel driven against a real service and a real tmux server.
func TestIntegrationPanel(t *testing.T) {
	t.Run("the archive key takes an agent all the way to the database", func(t *testing.T) {
		instance, agentID := startServedAgent(t)
		harness := newPanelHarness(t, panelHarnessParams{remote: newPanelRemote(instance)})
		relist(t, instance, harness)
		require.Equal(t, []string{"backend", "  login"}, readListRows(harness.readFrame()))

		harness.press("z")
		require.Contains(t, harness.readFrame(), "C-s submit", "the key opens the confirm prompt rather than archiving outright")
		harness.press("ctrl+s")

		listed, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{Archived: true}))
		require.NoError(t, err)
		require.Len(t, listed.Msg.GetAgents(), 1)
		assert.Equal(t, agentID, listed.Msg.GetAgents()[0].GetAgent().GetId())
		assert.Empty(t, instance.GetOwnedPane(agentID), "archiving from the panel stops what was running")

		relist(t, instance, harness)
		assert.Equal(t, []string{"backend"}, readListRows(harness.readFrame()), "and the row goes off the list, leaving the folder it was in")
	})

	t.Run("a staged agent's pane dying leaves the panel a nav pane and a stage pane", func(t *testing.T) {
		served := openServedPanel(t, 0)
		stagePane := served.harness.panel.ReadStagePane()

		served.instance.Tmux.Run("kill-pane", "-t", served.agentPane)
		relist(t, served.instance, served.harness)

		assert.False(t, served.instance.Tmux.Client().IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
		assert.Equal(t,
			[]string{served.navPane, served.harness.panel.ReadStagePane()},
			readPanelPanes(served.instance),
			"the now-dormant agent's record is in the stage")
	})

	t.Run("archiving a workflow sends its panes back and leaves the divider where it was dragged", func(t *testing.T) {
		served := openServedPanel(t, 0)
		stagePane := served.harness.panel.ReadStagePane()
		configured := readPaneWidth(t, served.instance, served.agentPane)
		served.instance.Tmux.Run("resize-pane", "-t", served.navPane, "-x", "60")
		dragged := readPaneWidth(t, served.instance, served.agentPane)
		require.NotEqual(t, configured, dragged, "the drag moved the divider off the configured width")

		served.harness.press("k", "z")
		require.Contains(t, served.harness.readFrame(), "Archive workflow")
		served.harness.press("ctrl+s")
		relist(t, served.instance, served.harness)

		assert.Empty(t, served.harness.panel.ReadGuestPane(), "the archived agent's pane is not left standing in the panel")
		require.True(t, served.instance.Tmux.Client().IsPaneAlive(stagePane), "the panel's own stage pane outlives the archive")
		assert.Equal(t, stagePane, served.harness.panel.ReadStagePane(), "so the panel is not left splitting another")
		assert.Equal(t, dragged, readPaneWidth(t, served.instance, stagePane))
	})

	t.Run("leaving a running agent for its folder zooms the panel over the window", func(t *testing.T) {
		served := openServedPanel(t, 0)
		readZoomedFlag := func() string {
			return served.instance.Tmux.Run("display-message", "-p", "-t", served.navPane, "#{window_zoomed_flag}")
		}
		require.Equal(t, "0", readZoomedFlag(), "the agent's own pane has the stage to itself")

		served.harness.press("k")
		served.harness.fireTimers()

		assert.Equal(t, "1", readZoomedFlag())
	})

	t.Run("the panel draws at the size of the pane it has just been given", func(t *testing.T) {
		served := openServedPanel(t, 341)
		share := readPaneWidth(t, served.instance, served.navPane)
		require.Less(t, share, 341, "the panel starts on its share of the window")

		held := served.harness.readRawFrame()
		served.harness.press("k")
		unsettled := served.harness.fireTimersUnsettled()

		require.Equal(t, 341, readPaneWidth(t, served.instance, served.navPane), "the folder row takes the window")
		assert.Equal(t, held, served.harness.readRawFrame(), "the panel keeps its last frame until it is told the size")

		for _, cmd := range unsettled {
			served.harness.settle(cmd)
		}
		assert.Equal(t, 341, lipgloss.Width(served.harness.readDrawn()), "and once told, the frame covers it")

		served.harness.press("j")
		served.harness.fireTimers()

		require.Equal(t, share, readPaneWidth(t, served.instance, served.navPane), "the agent's own pane takes it back")
		assert.LessOrEqual(t, lipgloss.Width(served.harness.readDrawn()), share, "so nothing drawn runs past the pane and wraps")
	})
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
	harness := newPanelHarness(t, panelHarnessParams{
		width:    client.GetPaneWidth(navPane),
		height:   client.GetPaneHeight(navPane),
		settings: ui.PanelSettings{Pane: navPane},
		tmux:     client,
		remote:   newPanelRemote(instance),
	})
	relist(t, instance, harness)
	require.Equal(t, []string{"backend", "  login"}, readListRows(harness.readFrame()))

	harness.fireTimers()
	agentPane := instance.GetOwnedPane(agentID)
	require.Equal(t, agentPane, harness.panel.ReadGuestPane(), "the agent's own pane is swapped into the stage")
	require.Contains(t, readPanelPanes(instance), agentPane)

	return &servedPanel{
		instance:  instance,
		harness:   harness,
		agentID:   agentID,
		agentPane: agentPane,
		navPane:   navPane,
	}
}

func newPanelRemote(instance *komradtest.Instance) *panelRemote {
	return &panelRemote{
		agents:    instance.Agents,
		workflows: instance.Workflows,
		schedules: instance.Schedules,
		hooks:     instance.Hooks,
		worktrees: instance.Worktrees,
	}
}

func relist(t *testing.T, instance *komradtest.Instance, harness *panelHarness) {
	t.Helper()

	agents, err := instance.Agents.ListAgents(t.Context(), connect.NewRequest(&agentv1.ListAgentsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)
	workflows, err := instance.Workflows.ListWorkflows(t.Context(), connect.NewRequest(&workflowv1.ListWorkflowsRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)
	schedules, err := instance.Schedules.ListSchedules(t.Context(), connect.NewRequest(&schedulev1.ListSchedulesRequest{View: viewv1.View_VIEW_FULL}))
	require.NoError(t, err)

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
	)

	harness.watch(buildSnapshot(rows...))
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
