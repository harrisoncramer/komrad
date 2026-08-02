package ui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"weave/internal/appconfig"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// panelSession is the tmux session the panel's own window is opened in, separate from the
// manager session the agents run in, the way the popup runs it.
const panelSession = "panel"

// TestPanelArchivesThroughTheService covers a keypress reaching the database. Every other
// test here answers the panel from a fake client, which says what the panel asked for and
// nothing about whether asking for it does anything, so the archive key is exercised once
// against the real service with a real agent running in a real pane behind it.
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

	keys := appconfig.DefaultKeybindings()
	tmux := &recordingTmux{}
	panel := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			actions:   instance.Actions,
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		stage:  NewStage(tmux, appconfig.DefaultLayout()),
		keys:   keys,
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	// The cursor sits on the agent rather than the folder it is nested under, since the
	// same key on the header archives the whole workflow.
	panel.tree.cursor = 1

	updated, cmd := panel.Update(keyMsg(keys.Agents.Archive[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	panel = settle(t, pressed, cmd)

	listed, err := instance.Agents.ListAgents(ctx, connect.NewRequest(&agentv1.ListAgentsRequest{Archived: true}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetAgents(), 1)
	assert.Equal(t, agent.GetId(), listed.Msg.GetAgents()[0].GetId())

	assert.Empty(t, instance.GetOwnedPane(agent.GetId()), "archiving from the panel stops what was running")

	panel = settle(t, panel, panel.refreshCmd())
	assert.Equal(t, []string{"header:backend"}, describeRows(panel.agentRows()),
		"and the row goes off the panel's own list, leaving the folder it was in")
}

// TestStagedAgentArchivedElsewhere covers the panel losing the agent it is showing to
// somebody else, the MCP server or the CLI or a second panel. Staging swaps the agent's pane
// into the panel's own window, so whoever ends that agent ends it there, and the stage's own
// pane is away in the agent's window where nothing on screen can reach it.
func TestStagedAgentArchivedElsewhere(t *testing.T) {
	staged := newStagedPanel(t)
	stagePane := staged.panel.stage.pane

	_, err := staged.instance.Agents.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
		Ids: []string{staged.agentID},
	}))
	require.NoError(t, err)

	staged.panel = settle(t, staged.panel, staged.panel.refreshCmd())

	require.False(t, staged.panel.tmux.IsPaneAlive(staged.agentPane), "the archive stopped what was running")
	assert.False(t, staged.panel.tmux.IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
	assert.Equal(t, []string{staged.navPane, staged.panel.stage.pane}, panesInPanel(t, staged.instance),
		"the panel is a nav pane and a stage pane again")

	// Recovering the layout is only half of it. A stage that still believes it holds the slot
	// swaps the next agent into the window it lost, so the next agent is staged too.
	second, err := staged.instance.Agents.CreateAgent(t.Context(), connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "rename the config keys",
		Title:      "config",
		WorkflowId: staged.workflowID,
	}))
	require.NoError(t, err)

	secondPane := staged.instance.GetOwnedPane(second.Msg.GetAgent().GetId())
	require.NotEmpty(t, secondPane)

	staged.panel = settle(t, staged.panel, staged.panel.refreshCmd())
	staged.panel.tree.cursor = 1
	staged.panel.stageSelection()

	assert.Contains(t, panesInPanel(t, staged.instance), secondPane,
		"the next agent shows beside the panel rather than being swapped off screen")
}

// TestStagedAgentExitsOnItsOwn covers the same loss with nobody archiving anything. Agent
// panes are opened without remain-on-exit, so a CLI that finishes takes its pane down, and
// while that agent is staged the pane it takes down is in the panel's window.
func TestStagedAgentExitsOnItsOwn(t *testing.T) {
	staged := newStagedPanel(t)
	stagePane := staged.panel.stage.pane

	staged.panel.tmux.KillPane(staged.agentPane)
	staged.panel = settle(t, staged.panel, staged.panel.refreshCmd())

	assert.False(t, staged.panel.tmux.IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
	assert.Equal(t, []string{staged.navPane, staged.panel.stage.pane}, panesInPanel(t, staged.instance),
		"the panel is a nav pane and a stage pane again")
}

// TestTerminalKeyRunsTheShippedAction covers the terminal key through the whole path: the
// panel opens the pane under the stage, the service finds the action weave ships, and the
// script respawns that pane where the agent runs. Every part of that is outside the panel,
// so a fake would only assert the panel asked.
func TestTerminalKeyRunsTheShippedAction(t *testing.T) {
	staged := newStagedPanel(t)

	agent, err := staged.instance.Agents.GetAgent(t.Context(), connect.NewRequest(&agentv1.GetAgentRequest{
		Id: staged.agentID,
	}))
	require.NoError(t, err)
	cwd := agent.Msg.GetAgent().GetCwd()

	updated, cmd := staged.panel.Update(keyMsg(appconfig.DefaultKeybindings().Agents.Terminal[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)

	panel := settle(t, pressed, cmd)
	require.Nil(t, panel.banner)
	require.Equal(t, belowAction, panel.tree.below)

	below := panel.stage.secondary
	require.NotEmpty(t, below)
	assert.Contains(t, panesInPanel(t, staged.instance), below, "the pane sits in the panel's own window")

	// The action is fired and nothing waits for it, so the shell it respawns the pane with
	// arrives a moment after the key. tmux answers with the path as the kernel resolves it,
	// which on a mac is not the one the agent was created under.
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return staged.instance.Tmux.Run("display-message", "-p", "-t", below, "#{pane_current_path}") == resolved
	}, settleTimeout, 50*time.Millisecond, "the shipped action roots the pane where the agent runs")
}

// TestTerminalKeyAfterTheEditor covers the pane below being handed from one action to the
// next. tmux re-runs whatever a pane ran last when respawned with no command, so a shell
// asked for over an editor has to name the shell rather than leave it out.
func TestTerminalKeyAfterTheEditor(t *testing.T) {
	staged := newStagedPanel(t)
	keys := appconfig.DefaultKeybindings()

	panel := pressAndSettle(t, staged.panel, keys.Agents.Editor[0])
	below := panel.stage.secondary
	require.NotEmpty(t, below)

	// No EDITOR is configured here, so the shipped action falls back to vi, which is vim on
	// most machines and reports itself as whichever it is.
	require.Eventually(t, func() bool {
		return strings.HasPrefix(paneCommand(staged, below), "vi")
	}, settleTimeout, 50*time.Millisecond, "the editor claims the pane first")

	panel = pressAndSettle(t, panel, keys.Agents.Terminal[0])

	require.Equal(t, below, panel.stage.secondary, "the same pane is handed over rather than a second one opened")
	require.Eventually(t, func() bool {
		return !strings.HasPrefix(paneCommand(staged, below), "vi")
	}, settleTimeout, 50*time.Millisecond, "the terminal key leaves a shell, not the editor again")
}

// pressAndSettle sends one key to the panel and runs whatever it asked for to completion.
func pressAndSettle(t *testing.T, panel model, key string) model {
	t.Helper()

	updated, cmd := panel.Update(keyMsg(key))
	pressed, ok := updated.(model)
	require.True(t, ok)

	settled := settle(t, pressed, cmd)
	require.Nil(t, settled.banner)

	return settled
}

// paneCommand is what a pane is running now, which is how a test tells one action's work
// from the next one's in the same pane.
func paneCommand(staged *stagedPanel, pane string) string {
	return staged.instance.Tmux.Run("display-message", "-p", "-t", pane, "#{pane_current_command}")
}

// settleTimeout bounds waiting on an action that was fired and not waited for, which is
// generous because it is only reached when something is wrong.
const settleTimeout = 10 * time.Second

// stagedPanel is a panel over the real service with one agent staged. That is the layout
// every case here starts from, the agent's pane swapped into the slot beside the panel and
// the stage's own pane away in the agent's window.
type stagedPanel struct {
	instance   *weavetest.Instance
	panel      model
	workflowID string
	agentID    string
	navPane    string
	agentPane  string
}

// newStagedPanel stands the service up, launches one agent, and puts the panel's cursor on it.
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
	stage := NewStage(client, appconfig.DefaultLayout())
	stage.Init(navPane)

	panel := model{
		api: api{
			agents:    instance.Agents,
			workflows: instance.Workflows,
			schedules: instance.Schedules,
			actions:   instance.Actions,
		},
		tmux:   client,
		stage:  stage,
		keys:   appconfig.DefaultKeybindings(),
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	require.Equal(t, []string{"header:backend", "agent:login"}, describeRows(panel.agentRows()))

	// Row 0 is the folder the agent is nested under, so the agent itself is row 1.
	panel.tree.cursor = 1
	panel.stageSelection()
	require.Equal(t, agentPane, stage.agentPane, "the agent's own pane is swapped into the slot")
	require.Contains(t, panesInPanel(t, instance), agentPane)

	return &stagedPanel{
		instance:   instance,
		panel:      panel,
		workflowID: workflow.Msg.GetWorkflow().GetId(),
		agentID:    created.Msg.GetAgent().GetId(),
		navPane:    navPane,
		agentPane:  agentPane,
	}
}

// panesInPanel is every pane in the panel's own session, in the order tmux lays them out.
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
