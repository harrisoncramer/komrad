//go:build integration

package ui

import (
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const panelSession = "shown"

// TestPanelArchivesThroughTheService takes the archive key all the way to the database.
func TestPanelArchivesThroughTheService(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agent := created.Msg.GetAgent()
	require.NotEmpty(t, instance.GetOwnedPane(agent.GetId()))

	keys := config.DefaultKeybindings()
	tmux := &recordingTmux{}
	shown := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenAgents,
	}

	shown = settle(t, shown, shown.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(shown.agentRows()))

	shown.agents.cursor = 1

	updated, _ := shown.Update(keyMsg(keys.Agents.Archive[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	require.Equal(t, formAgentArchive, pressed.form.kind, "the key opens the confirm prompt rather than archiving outright")
	require.Equal(t, screenAgents, pressed.screen, "and it is drawn over the list it was opened from")

	pressed.form.form.State = huh.StateCompleted
	confirmed, cmd := pressed.form.update(pressed, keyMsg("enter"))
	shown, ok = confirmed.(model)
	require.True(t, ok)
	shown = settle(t, shown, cmd)

	listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Archived: true}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetAgents(), 1)
	assert.Equal(t, agent.GetId(), listed.Msg.GetAgents()[0].GetId())

	assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "archiving from the shown stops what was running")

	shown = settle(t, shown, shown.refreshCmd())
	assert.Equal(t, []string{"header:backend"}, describeRows(shown.agentRows()),
		"and the row goes off the shown's own list, leaving the folder it was in")
}

// TestStagedAgentExitsOnItsOwn covers a staged agent's pane dying while staging holds it in the
// panel's own window.
func TestStagedAgentExitsOnItsOwn(t *testing.T) {
	staged := newStagedPanel(t)
	stagePane := staged.shown.frame.Pane(layout.RegionStage)

	staged.shown.tmux.KillPane(staged.agentPane)
	staged.shown = settle(t, staged.shown, staged.shown.refreshCmd())

	assert.False(t, staged.shown.tmux.IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
	assert.Equal(t,
		[]string{staged.navPane, staged.shown.frame.Pane(layout.RegionStage)},
		panesInPanel(t, staged.instance),
		"the shown is a nav pane and a stage pane again, the now-dormant agent's record in the stage")
}

// TestArchivingAWorkflowSendsItsPanesBack covers the panel's own stage pane.
func TestArchivingAWorkflowSendsItsPanesBack(t *testing.T) {
	staged := newStagedPanel(t)
	shown := staged.shown
	stagePane := shown.frame.Pane(layout.RegionStage)

	width := func(pane string) string {
		return staged.instance.Tmux.Run("display-message", "-p", "-t", pane, "#{pane_width}")
	}
	// The divider dragged where the user wants it rather than where the config put it.
	configured := width(staged.agentPane)
	staged.instance.Tmux.Run("resize-pane", "-t", staged.navPane, "-x", "60")
	dragged := width(staged.agentPane)
	require.NotEqual(t, configured, dragged, "the drag moved the divider off the configured width")

	shown.agents.cursor = 0
	require.True(t, shown.agents.isHoveringWorkflow(shown.shared()), "the cursor is on the folder the agent is in")

	updated, _ := shown.Update(keyMsg(shown.keys.Workflows.Archive[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	require.Equal(t, formWorkflowArchive, pressed.form.kind)

	pressed.form.form.State = huh.StateCompleted
	confirmed, cmd := pressed.form.update(pressed, keyMsg("enter"))
	shown, ok = confirmed.(model)
	require.True(t, ok)
	shown = settle(t, shown, cmd)
	shown = settle(t, shown, shown.refreshCmd())

	assert.Empty(t, shown.frame.GuestPane(), "the archived agent's pane is not left standing in the shown")
	require.True(t, shown.tmux.IsPaneAlive(stagePane), "the shown's own stage pane outlives the archive")
	assert.Equal(t, stagePane, shown.frame.Pane(layout.RegionStage), "so the shown is not left splitting another")
	assert.Equal(t, dragged, width(stagePane), "and the divider stays where it was dragged")
}

type stagedPanel struct {
	instance  *weavetest.Instance
	shown     model
	navPane   string
	agentPane string
}

func newStagedPanel(t *testing.T) *stagedPanel {
	t.Helper()

	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agentPane := instance.GetOwnedPane(created.Msg.GetAgent().GetId())
	require.NotEmpty(t, agentPane)
	instance.WaitForHarness(created.Msg.GetAgent().GetId())

	instance.Tmux.NewSession(panelSession)
	navPane := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")

	client := instance.Tmux.Client()
	frame := layout.New(client, config.DefaultLayout())
	frame.Attach(navPane)

	shown := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   client,
		frame:  frame,
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}

	shown = settle(t, shown, shown.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(shown.agentRows()))

	shown.agents.cursor = 1
	shown.stageSelection()
	require.Equal(t, agentPane, frame.GuestPane(), "the agent's own pane is swapped into the slot")
	require.Contains(t, panesInPanel(t, instance), agentPane)

	return &stagedPanel{
		instance:  instance,
		shown:     shown,
		navPane:   navPane,
		agentPane: agentPane,
	}
}

func panesInPanel(t *testing.T, instance *weavetest.Instance) []string {
	t.Helper()

	var panes []string
	for _, line := range strings.Split(instance.Tmux.Run("list-panes", "-s", "-t", panelSession, "-F", "#{pane_id}"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			panes = append(panes, trimmed)
		}
	}

	return panes
}

// TestPanelKeepsTheWindowLeavingARunningAgent drives the panel against a real tmux server.
func TestPanelKeepsTheWindowLeavingARunningAgent(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	require.NotEmpty(t, instance.GetOwnedPane(created.Msg.GetAgent().GetId()))
	instance.WaitForHarness(created.Msg.GetAgent().GetId())

	instance.Tmux.NewSession(panelSession)
	sidebar := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")

	client := instance.Tmux.Client()
	shown := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   client,
		popup:  NewPopupWindow(client, panelSession),
		frame:  layout.New(client, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}
	shown.width, shown.height = 200, 40
	shown.frame.Attach(sidebar)

	shown = settle(t, shown, shown.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(shown.agentRows()))

	zoomed := func() string {
		return instance.Tmux.Run("display-message", "-p", "-t", sidebar, "#{window_zoomed_flag}")
	}

	shown.agents.cursor = 1
	shown.stageSelection()
	require.Equal(t, "0", zoomed(), "the agent's own pane has the stage to itself")

	shown.agents.cursor = 0
	shown.stageSelection()

	assert.True(t, shown.frame.IsSolo(), "the shown means to hold the window on its folder row")
	assert.Equal(t, "1", zoomed(), "and tmux is holding it, rather than the frame only believing so")
}

// TestPanelIsTheSizeOfThePaneItWasGiven covers the panel drawing at the size of the pane it has
// just been given rather than the one it had before the zoom.
func TestPanelIsTheSizeOfThePaneItWasGiven(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	require.NotEmpty(t, instance.GetOwnedPane(created.Msg.GetAgent().GetId()))
	instance.WaitForHarness(created.Msg.GetAgent().GetId())

	instance.Tmux.NewSession(panelSession)
	sidebar := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")
	instance.Tmux.Run("resize-window", "-t", panelSession, "-x", "341", "-y", "50")

	client := instance.Tmux.Client()
	shown := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   client,
		popup:  NewPopupWindow(client, panelSession),
		frame:  layout.New(client, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		drawn:  &lastFrame{},
	}
	shown.frame.Attach(sidebar)
	shown = settle(t, shown, shown.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(shown.agentRows()))

	// The one report the panel gets for free.
	shown.width, shown.height = client.GetPaneWidth(sidebar), client.GetPaneHeight(sidebar)
	share := shown.width
	require.Less(t, share, 341, "the shown starts on its share of the frame")

	// The panel moves its own pane, so the size that leaves it comes back as the resize it is,
	// which is what the renderer is told and so what the frame may be drawn at.
	sizes := make(chan tea.WindowSizeMsg, 4)
	shown.frame.OnResize(func(width, height int) {
		sizes <- tea.WindowSizeMsg{Width: width, Height: height}
	})

	paneWidth := func() int {
		width, _ := shown.frame.SidebarSize()

		return width
	}

	held := shown.View()

	shown.agents.cursor = 0
	shown.stageSelection()

	require.Equal(t, 341, paneWidth(), "the folder row takes the window")
	assert.True(t, shown.awaitingSize, "which the shown has not been told the size of yet")
	assert.Equal(t, held, shown.View(), "so it draws the frame it has rather than one at a width nobody has been given")

	shown = shown.resizeTo(reportedSize(t, sizes))

	assert.Equal(t, paneWidth(), shown.width, "and once the report lands it is the size of the pane it now has")
	assert.Equal(t, 341, lipgloss.Width(shown.View()), "so the frame covers it")

	shown.agents.cursor = 1
	shown.stageSelection()

	require.Equal(t, share, paneWidth(), "the agent's own pane takes it back")

	shown = shown.resizeTo(reportedSize(t, sizes))

	assert.Equal(t, paneWidth(), shown.width, "and the shown is the size of the pane it is left with")
	assert.LessOrEqual(t, lipgloss.Width(shown.View()), share, "so nothing it draws runs past the pane and wraps")
}

// reportedSize is the size the frame read back off its own pane, which the panel is handed as a
// resize rather than reading for itself.
func reportedSize(t *testing.T, sizes <-chan tea.WindowSizeMsg) tea.WindowSizeMsg {
	t.Helper()

	select {
	case size := <-sizes:
		return size
	case <-time.After(2 * time.Second):
		t.Fatal("the frame never reported the size its own pane landed at")

		return tea.WindowSizeMsg{}
	}
}
