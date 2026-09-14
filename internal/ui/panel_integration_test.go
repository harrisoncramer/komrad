//go:build integration

package ui

import (
	"strings"
	"testing"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/weavetest"
	"weave/lib/xptr"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const panelSession = "panel"

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
	panel := model{
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

	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	panel.tree.cursor = 1

	updated, _ := panel.Update(keyMsg(keys.Agents.Archive[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	require.Equal(t, formAgentArchive, pressed.form.kind, "the key opens the confirm prompt rather than archiving outright")
	require.Equal(t, screenAgents, pressed.screen, "and it is drawn over the list it was opened from")

	pressed.form.form.State = huh.StateCompleted
	confirmed, cmd := pressed.form.update(pressed, keyMsg("enter"))
	panel, ok = confirmed.(model)
	require.True(t, ok)
	panel = settle(t, panel, cmd)

	listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Archived: true}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetAgents(), 1)
	assert.Equal(t, agent.GetId(), listed.Msg.GetAgents()[0].GetId())

	assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "archiving from the panel stops what was running")

	panel = settle(t, panel, panel.refreshCmd())
	assert.Equal(t, []string{"header:backend"}, describeRows(panel.agentRows()),
		"and the row goes off the panel's own list, leaving the folder it was in")
}

// TestPanelOpensThePullRequestKey pins the pull request key by the address it hands the
// desktop for the agent the cursor was on.
func TestPanelOpensThePullRequestKey(t *testing.T) {
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

	_, err = instance.Agents.SetMetadata(ctx, connect.NewRequest(&agentv1.SetMetadataRequest{
		Id:    created.Msg.GetAgent().GetId(),
		Key:   "pr",
		Value: xptr.Ptr("https://example.test/1"),
	}))
	require.NoError(t, err)

	keys := config.DefaultKeybindings()
	tmux := &recordingTmux{}
	panel := model{
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

	panel = settle(t, panel, panel.refreshCmd())
	panel.tree.cursor = 1

	updated, cmd := panel.Update(keyMsg(keys.Agents.OpenPR[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	settle(t, pressed, cmd)

	assert.Equal(t, []string{"https://example.test/1"}, instance.Opened.Targets(),
		"the address recorded on the agent the cursor was on")
	assert.Contains(t, helpDescriptions(pressed.agentHelp().full), "open pr", "the legend carries the key")
}

// TestPanelLeavesThePullRequestKeyAloneWithoutOne pins that an agent with no recorded pull
// request asks for nothing.
func TestPanelLeavesThePullRequestKeyAloneWithoutOne(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "backend",
		Cwd:  t.TempDir(),
	}))
	require.NoError(t, err)

	_, err = instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "fix the flaky login test",
		Title:      "login",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	keys := config.DefaultKeybindings()
	tmux := &recordingTmux{}
	panel := model{
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

	panel = settle(t, panel, panel.refreshCmd())
	panel.tree.cursor = 1

	assert.Contains(t, helpDescriptions(panel.agentHelp().full), "open pr", "the legend keeps one height whether or not the key has an address to open")

	updated, cmd := panel.Update(keyMsg(keys.Agents.OpenPR[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	settle(t, pressed, cmd)

	assert.Empty(t, instance.Opened.Targets(), "nothing is opened for an agent with no pull request")
}

// TestStagedAgentExitsOnItsOwn covers a staged agent's pane dying while staging holds it in the
// panel's own window.
func TestStagedAgentExitsOnItsOwn(t *testing.T) {
	staged := newStagedPanel(t)
	stagePane := staged.panel.frame.Pane(layout.RegionStage)

	staged.panel.tmux.KillPane(staged.agentPane)
	staged.panel = settle(t, staged.panel, staged.panel.refreshCmd())

	assert.False(t, staged.panel.tmux.IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
	assert.Equal(t,
		[]string{staged.navPane, staged.panel.frame.Pane(layout.RegionStage)},
		panesInPanel(t, staged.instance),
		"the panel is a nav pane and a stage pane again, the now-dormant agent's record in the stage")
}

// TestArchivingAWorkflowSendsItsPanesBack covers the panel's own stage pane, which is parked in
// the staged agent's window and would be killed with it.
func TestArchivingAWorkflowSendsItsPanesBack(t *testing.T) {
	staged := newStagedPanel(t)
	panel := staged.panel
	stagePane := panel.frame.Pane(layout.RegionStage)

	width := func(pane string) string {
		return staged.instance.Tmux.Run("display-message", "-p", "-t", pane, "#{pane_width}")
	}
	// The divider dragged where the user wants it rather than where the config put it.
	configured := width(staged.agentPane)
	staged.instance.Tmux.Run("resize-pane", "-t", staged.navPane, "-x", "60")
	dragged := width(staged.agentPane)
	require.NotEqual(t, configured, dragged, "the drag moved the divider off the configured width")

	panel.tree.cursor = 0
	require.True(t, isHoveringWorkflow(panel), "the cursor is on the folder the agent is in")

	updated, _ := panel.Update(keyMsg(panel.keys.Workflows.Archive[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	require.Equal(t, formWorkflowArchive, pressed.form.kind)

	pressed.form.form.State = huh.StateCompleted
	confirmed, cmd := pressed.form.update(pressed, keyMsg("enter"))
	panel, ok = confirmed.(model)
	require.True(t, ok)
	panel = settle(t, panel, cmd)
	panel = settle(t, panel, panel.refreshCmd())

	assert.Empty(t, panel.frame.GuestPane(), "the archived agent's pane is not left standing in the panel")
	require.True(t, panel.tmux.IsPaneAlive(stagePane), "the panel's own stage pane outlives the archive")
	assert.Equal(t, stagePane, panel.frame.Pane(layout.RegionStage), "so the panel is not left splitting another")
	assert.Equal(t, dragged, width(stagePane), "and the divider stays where it was dragged")
}

type stagedPanel struct {
	instance  *weavetest.Instance
	panel     model
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

	instance.Tmux.NewSession(panelSession)
	navPane := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")

	client := instance.Tmux.Client()
	frame := layout.New(client, config.DefaultLayout())
	frame.Attach(navPane)

	panel := model{
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

	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	panel.tree.cursor = 1
	panel.stageSelection()
	require.Equal(t, agentPane, frame.GuestPane(), "the agent's own pane is swapped into the slot")
	require.Contains(t, panesInPanel(t, instance), agentPane)

	return &stagedPanel{
		instance:  instance,
		panel:     panel,
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

func helpDescriptions(bindings []key.Binding) []string {
	described := make([]string, 0, len(bindings))
	for _, b := range bindings {
		described = append(described, b.Help().Desc)
	}

	return described
}

// TestPanelClearsTheUnviewedMarkThroughTheService takes the read marker all the way to the
// database.
func TestPanelClearsTheUnviewedMarkThroughTheService(t *testing.T) {
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
	instance.Stores.SeedTurnEnded(t, agent.GetId())

	tmux := &recordingTmux{sessionClient: panelViewer}
	popup := NewPopupWindow(tmux, "test-session")

	panel := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   tmux,
		popup:  popup,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	panel.tree.cursor = 1

	hovered, ok := panel.selectedAgent()
	require.True(t, ok)
	require.True(t, hovered.unviewed, "the panel reads the mark off the listing")

	at := time.Now()
	panel, cmd := panel.dwellViewed(at)
	require.Nil(t, cmd)

	panel, cmd = panel.dwellViewed(at.Add(viewedDwell))
	require.NotNil(t, cmd)
	cmd()

	panel = settle(t, panel, panel.refreshCmd())
	after, ok := panel.selectedAgent()
	require.True(t, ok)
	assert.False(t, after.unviewed, "and the mark is gone once the service has it")
}

// TestPanelLeavesTheMarkStandingWhileItIsHidden covers the way weave is actually used: the panel
// keeps running with its client detached, so the tick keeps firing over a cursor that has been
// parked on one row for hours, and the row that finishes while nobody is watching is exactly
// the one the mark exists for.
func TestPanelLeavesTheMarkStandingWhileItIsHidden(t *testing.T) {
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

	tmux := &recordingTmux{sessionClient: panelViewer}
	popup := NewPopupWindow(tmux, "test-session")

	panel := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   tmux,
		popup:  popup,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	panel.tree.cursor = 1

	at := time.Now()
	panel, _ = panel.dwellViewed(at)

	tmux.sessionClient = ""

	// The agent finishes long after the cursor last moved, which is what makes a dwell measured
	// from the cursor's arrival say the row was read.
	instance.Stores.SeedTurnEnded(t, created.Msg.GetAgent().GetId())
	panel = settle(t, panel, panel.refreshCmd())

	hidden := at.Add(time.Hour)
	panel, cmd := panel.dwellViewed(hidden)
	require.Nil(t, cmd, "nobody was looking at it")

	tmux.sessionClient = panelViewer

	back := hidden.Add(viewedDwell / 2)
	panel, cmd = panel.dwellViewed(back)
	assert.Nil(t, cmd, "and coming back to the panel starts the look over rather than finishing one")

	_, cmd = panel.dwellViewed(back.Add(viewedDwell))
	assert.NotNil(t, cmd)
}

// TestPanelMakesEachTurnEarnItsOwnDwell covers a cursor left parked on one row while the agent
// under it keeps working: the first turn is read after a second, and the turns that end
// afterwards have to be looked at in their own right rather than being cleared on arrival by a
// dwell measured from a look that happened hours ago.
func TestPanelMakesEachTurnEarnItsOwnDwell(t *testing.T) {
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

	id := created.Msg.GetAgent().GetId()
	instance.Stores.SeedTurnEnded(t, id)

	tmux := &recordingTmux{sessionClient: panelViewer}
	popup := NewPopupWindow(tmux, "test-session")

	panel := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			hooks:     instance.Hooks,
		},
		tmux:   tmux,
		popup:  popup,
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	panel.tree.cursor = 1

	at := time.Now()
	panel, cmd := panel.dwellViewed(at)
	require.Nil(t, cmd)

	panel, cmd = panel.dwellViewed(at.Add(viewedDwell))
	require.NotNil(t, cmd, "the first turn is read once the cursor has sat on it")
	cmd()

	panel = settle(t, panel, panel.refreshCmd())

	// An hour of ticks over a row with nothing to read, which is a cursor somebody parked and
	// walked away from.
	later := at.Add(time.Hour)
	panel, cmd = panel.dwellViewed(later)
	require.Nil(t, cmd, "a row that has been read makes no calls")

	instance.Stores.SeedTurnEnded(t, id)
	panel = settle(t, panel, panel.refreshCmd())

	hovered, ok := panel.selectedAgent()
	require.True(t, ok)
	require.True(t, hovered.unviewed, "a second turn ending is unread again")

	panel, cmd = panel.dwellViewed(later.Add(time.Millisecond))
	assert.Nil(t, cmd, "and is not claimed on arrival by the hour the cursor spent sitting there")

	_, cmd = panel.dwellViewed(later.Add(viewedDwell * 2))
	assert.NotNil(t, cmd, "only by a look of its own")
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

	instance.Tmux.NewSession(panelSession)
	sidebar := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")

	client := instance.Tmux.Client()
	panel := model{
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
	panel.width, panel.height = 200, 40
	panel.frame.Attach(sidebar)

	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	zoomed := func() string {
		return instance.Tmux.Run("display-message", "-p", "-t", sidebar, "#{window_zoomed_flag}")
	}

	panel.tree.cursor = 1
	panel.stageSelection()
	require.Equal(t, "0", zoomed(), "the agent's own pane has the stage to itself")

	panel.tree.cursor = 0
	panel.stageSelection()

	assert.True(t, panel.frame.IsSolo(), "the panel means to hold the window on its folder row")
	assert.Equal(t, "1", zoomed(), "and tmux is holding it, rather than the frame only believing so")
}

// TestPanelIsTheSizeOfThePaneItWasGiven covers the panel drawing at the size of the pane it has
// just been given rather than the one it had before the zoom, which is what the flicker was.
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

	instance.Tmux.NewSession(panelSession)
	sidebar := instance.Tmux.Run("display-message", "-p", "-t", panelSession, "#{pane_id}")
	instance.Tmux.Run("resize-window", "-t", panelSession, "-x", "341", "-y", "50")

	client := instance.Tmux.Client()
	panel := model{
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
	panel.frame.Attach(sidebar)
	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	// The one report the panel gets for free, which is the terminal's own on the way in.
	panel.width, panel.height = client.GetPaneWidth(sidebar), client.GetPaneHeight(sidebar)
	share := panel.width
	require.Less(t, share, 341, "the panel starts on its share of the frame")

	// The panel moves its own pane, so the size that leaves it comes back as the resize it is,
	// which is what the renderer is told and so what the frame may be drawn at.
	sizes := make(chan tea.WindowSizeMsg, 4)
	panel.frame.OnResize(func(width, height int) {
		sizes <- tea.WindowSizeMsg{Width: width, Height: height}
	})

	paneWidth := func() int {
		width, _ := panel.frame.SidebarSize()

		return width
	}

	held := panel.View()

	panel.tree.cursor = 0
	panel.stageSelection()

	require.Equal(t, 341, paneWidth(), "the folder row takes the window")
	assert.True(t, panel.awaitingSize, "which the panel has not been told the size of yet")
	assert.Equal(t, held, panel.View(), "so it draws the frame it has rather than one at a width nobody has been given")

	panel = panel.resizeTo(reportedSize(t, sizes))

	assert.Equal(t, paneWidth(), panel.width, "and once the report lands it is the size of the pane it now has")
	assert.Equal(t, 341, lipgloss.Width(panel.View()), "so the frame covers it")

	panel.tree.cursor = 1
	panel.stageSelection()

	require.Equal(t, share, paneWidth(), "the agent's own pane takes it back")

	panel = panel.resizeTo(reportedSize(t, sizes))

	assert.Equal(t, paneWidth(), panel.width, "and the panel is the size of the pane it is left with")
	assert.LessOrEqual(t, lipgloss.Width(panel.View()), share, "so nothing it draws runs past the pane and wraps")
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
