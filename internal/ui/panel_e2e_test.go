package ui

import (
	"os"
	"path/filepath"
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
	"github.com/charmbracelet/huh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	keys := config.DefaultKeybindings()
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
	require.Equal(t, screenAgentArchive, pressed.screen, "the key opens the confirm prompt rather than archiving outright")

	*pressed.values.archive = archiveChoiceArchive
	pressed.form.State = huh.StateCompleted
	confirmed, cmd := pressed.updateAgentArchive(keyMsg("enter"))
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

// TestPanelOpensThePullRequestKey covers the pull request key from the press through to a
// process. The panel names an action nothing in its own list shows, so what proves the key is
// wired is the script running, on the agent the cursor was on, since the shipped one reads the
// pull request back out of the API by that id.
func TestPanelOpensThePullRequestKey(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	out := filepath.Join(t.TempDir(), "opened.txt")
	require.NoError(t, os.WriteFile(filepath.Join(instance.GetActionsDir, openPRAction), []byte(
		"#!/bin/sh\nprintf '%s' \"$WEAVE_AGENT_ID\" > "+out+"\n",
	), 0o755))

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
			actions:   instance.Actions,
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

	var written []byte
	require.Eventually(t, func() bool {
		read, err := os.ReadFile(out)
		written = read

		return err == nil && len(read) > 0
	}, 5*time.Second, 10*time.Millisecond, "the pull request key never reached a process")

	assert.Equal(t, created.Msg.GetAgent().GetId(), string(written), "the agent the script reads the pull request back for")
	assert.Contains(t, helpDescriptions(pressed.rowHelp()), "open pr (if recorded)", "the legend carries the key")
}

// TestPanelLeavesThePullRequestKeyAloneWithoutOne covers the agents that never recorded one.
// The panel holds the metadata it is drawing from, so it answers this itself rather than
// starting a script that has nothing to open.
func TestPanelLeavesThePullRequestKeyAloneWithoutOne(t *testing.T) {
	instance := weavetest.Start(t, weavetest.Params{})
	ctx := t.Context()

	out := filepath.Join(t.TempDir(), "opened.txt")
	require.NoError(t, os.WriteFile(filepath.Join(instance.GetActionsDir, openPRAction), []byte(
		"#!/bin/sh\nprintf 'ran' > "+out+"\n",
	), 0o755))

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
			actions:   instance.Actions,
		},
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   keys,
		screen: screenAgents,
	}

	panel = settle(t, panel, panel.refreshCmd())
	panel.tree.cursor = 1

	assert.Contains(t, helpDescriptions(panel.rowHelp()), "open pr (if recorded)", "the legend keeps one height, and says when the key answers")

	updated, cmd := panel.Update(keyMsg(keys.Agents.OpenPR[0]))
	pressed, ok := updated.(model)
	require.True(t, ok)
	settle(t, pressed, cmd)

	require.Never(t, func() bool {
		_, err := os.Stat(out)

		return err == nil
	}, time.Second, 10*time.Millisecond, "nothing is started for an agent with no pull request")
}

// TestStagedAgentArchivedElsewhere covers the panel losing the agent it is showing to
// somebody else, the MCP server or the CLI or a second panel. Staging swaps the agent's pane
// into the panel's own window, so whoever ends that agent ends it there, and the stage's own
// pane is away in the agent's window where nothing on screen can reach it.
func TestStagedAgentArchivedElsewhere(t *testing.T) {
	staged := newStagedPanel(t)
	stagePane := staged.panel.frame.Pane(layout.RegionStage)

	_, err := staged.instance.Agents.ArchiveAgents(t.Context(), connect.NewRequest(&agentv1.ArchiveAgentsRequest{
		Ids: []string{staged.agentID},
	}))
	require.NoError(t, err)

	staged.panel = settle(t, staged.panel, staged.panel.refreshCmd())

	require.False(t, staged.panel.tmux.IsPaneAlive(staged.agentPane), "the archive stopped what was running")
	assert.False(t, staged.panel.tmux.IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
	assert.Equal(t, []string{staged.navPane, staged.panel.frame.Pane(layout.RegionStage)}, panesInPanel(t, staged.instance),
		"the panel is a nav pane and a stage pane again")

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
	stagePane := staged.panel.frame.Pane(layout.RegionStage)

	staged.panel.tmux.KillPane(staged.agentPane)
	staged.panel = settle(t, staged.panel, staged.panel.refreshCmd())

	assert.False(t, staged.panel.tmux.IsPaneAlive(stagePane), "the pane left in the agent's window is not left running")
	assert.Equal(t,
		[]string{staged.navPane, staged.panel.frame.Pane(layout.RegionStage)},
		panesInPanel(t, staged.instance),
		"the panel is a nav pane and a stage pane again, the now-dormant agent's record in the stage")
}

type stagedPanel struct {
	instance   *weavetest.Instance
	panel      model
	workflowID string
	agentID    string
	navPane    string
	agentPane  string
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
			actions:   instance.Actions,
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
		instance:   instance,
		panel:      panel,
		workflowID: workflow.Msg.GetWorkflow().GetId(),
		agentID:    created.Msg.GetAgent().GetId(),
		navPane:    navPane,
		agentPane:  agentPane,
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
