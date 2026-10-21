package ui_test

import (
	"testing"

	"komrad/internal/engine"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestTerminal covers the subpage that opens a shell in the pane under the stage.
func TestTerminal(t *testing.T) {
	t.Run("from a task's row the shell opens in its directory and takes the keys", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(dir))

		harness.press("space", "t")

		assert.Equal(t, []string{"/bin/zsh"}, readShells(harness), "the shell is named so the pane cannot rerun what it held last")
		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("from a task's row the shell takes the stage's rows", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", t.TempDir()))

		harness.press("space", "t")

		assert.Contains(t, harness.panel.ReadKilledPanes(), stagePane)
		assert.Empty(t, harness.panel.ReadStagePane())
	})

	t.Run("picked again it hands the shell the keys rather than respawning it", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

		harness.press("space", "t", "space", "t")

		assert.Len(t, readShells(harness), 1)
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the stage rendered again leaves the shell running", func(t *testing.T) {
		agent := newStagedShellAgent(t.TempDir())
		harness := openTerminal(t, panelHarnessParams{}, agent)
		harness.press("space", "t")

		harness.watch(buildSnapshot(withTasks([]proto.Message{newLiveWorkflow("w1", "backend"), agent})...))
		harness.watch(buildSnapshot(withTasks([]proto.Message{newLiveWorkflow("w1", "backend"), agent})...))

		assert.Len(t, readShells(harness), 1)
	})

	t.Run("the shell keeps the keys as the listing changes", func(t *testing.T) {
		agent := newStagedShellAgent(t.TempDir())
		harness := openTerminal(t, panelHarnessParams{}, agent)
		harness.press("space", "t")
		selected := len(harness.panel.ReadSelectedPanes())
		harness.panel.SetActivePane(harness.panel.ReadStagePane())
		harness.send(tea.BlurMsg{})

		harness.watch(buildSnapshot(withTasks([]proto.Message{newLiveWorkflow("w1", "backend"), agent})...))

		assert.Empty(t, harness.panel.ReadSelectedPanes()[selected:])
		assert.False(t, harness.panel.IsZoomed())
	})

	t.Run("the shell stays where it opened as the cursor moves", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(first), newShellAgent("a2", second))
		harness.press("space", "t")

		harness.press("j")

		assert.Equal(t, []string{first}, readShellDirs(harness))
	})

	t.Run("picked on another agent it moves the shell there", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(first), newShellAgent("a2", second))

		harness.press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{first, second}, readShellDirs(harness))
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("picked on a sibling in the same directory it keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(dir), newShellAgent("a2", dir))

		harness.press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness), "one directory is one shell")
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("picked on a task whose worktree has gone it says why and keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(dir), newShellAgent("a2", "/no/such/worktree"))

		harness.press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a task whose worktree has gone opens no shell", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent("/no/such/worktree"))

		harness.press("space", "t")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Contains(t, harness.readFrame(), terminalRefuse)
	})

	t.Run("a workflow opens the shell in its orchestrator's directory", func(t *testing.T) {
		workflowDir, agentDir := t.TempDir(), t.TempDir()
		orchestrator := newOrchestrator()
		orchestrator.Cwd = workflowDir
		harness := openTerminal(t, panelHarnessParams{}, orchestrator, newShellAgent("a2", agentDir))

		harness.press("k", "space", "t", "j", "space", "t")

		assert.Equal(t, []string{workflowDir, agentDir}, readShellDirs(harness))
	})

	t.Run("a task with no checkout opens the shell in its workflow's directory", func(t *testing.T) {
		workflowDir := t.TempDir()
		workflow := newLiveWorkflow("w1", "backend")
		workflow.Cwd = workflowDir
		agent := newStagedShellAgent(t.TempDir())
		harness := newPanelHarness(t, panelHarnessParams{})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(workflow, agent, newTaskOf(agent)))
		harness.press("g", "g", "j", "space", "t")

		assert.Equal(t, []string{workflowDir}, readShellDirs(harness))
	})

	t.Run("an archived task opens the shell in its worktree rather than where its agent ran", func(t *testing.T) {
		dir := t.TempDir()
		agent := newShellAgent("a1", t.TempDir())
		agent.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
		task := newTaskOf(agent)
		task.Cwd, task.ArchivedAt = xptr.Ptr(dir), xptr.Ptr(archivedAt)
		harness := newPanelHarness(t, panelHarnessParams{})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), agent, task))
		harness.press("g", "g", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
	})

	t.Run("a remote engine with no way to reach it opens no shell", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{engine: engine.Remote{URL: "https://komrad.example.com"}}, newStagedShellAgent("/var/lib/komrad/worktrees/01a0c466"))

		harness.press("space", "t")

		assert.Empty(t, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
	})

	t.Run("a remote engine's shell is opened over the engine's api", func(t *testing.T) {
		remote := panelHarnessParams{
			engine:       engine.Remote{URL: "https://komrad.example.com"},
			shellCommand: []string{"komrad", "shell"},
		}
		harness := openTerminal(t, remote, newStagedShellAgent("/var/lib/komrad/worktrees/01a0c466"))

		harness.press("space", "t")

		assert.NotContains(t, harness.readFrame(), terminalRefuse, "nothing is stat'd on this machine")
		require.NotEmpty(t, harness.panel.ReadRespawns())
		last := len(harness.panel.ReadRespawns()) - 1
		assert.Equal(t, infoPane, harness.panel.ReadRespawnPanes()[last])
		assert.Equal(t, "komrad shell -- /var/lib/komrad/worktrees/01a0c466", harness.panel.ReadRespawns()[last])
		assert.Empty(t, harness.panel.ReadRespawnDirs()[last], "the pane is not rooted at a directory this machine lacks")
		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a task and workflow with no directory have it gray and open no shell", func(t *testing.T) {
		remote := panelHarnessParams{
			engine:       engine.Remote{URL: "https://komrad.example.com"},
			shellCommand: []string{"komrad", "shell"},
		}
		harness := openTerminal(t, remote, newStagedShellAgent(""))

		harness.press("space")
		require.Regexp(t, readOffEntry("terminal", "t"), harness.readRawFrame())
		harness.press("t")

		assert.Empty(t, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
	})

	t.Run("the shell stays open while the switcher steps through a view that cannot hold it", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

		harness.press("space", "t", "tab", "tab")

		assert.NotContains(t, harness.panel.ReadKilledPanes(), infoPane)
	})

	t.Run("the shell goes once the switcher closes on a view that cannot hold it", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

		harness.press("space", "t", "tab", "tab", "esc")

		assert.Contains(t, harness.panel.ReadKilledPanes(), infoPane)
	})
}

// TestInfo covers the pane below the stage staying shut until something asks for it.
func TestInfo(t *testing.T) {
	harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

	assert.Equal(t, "%9", harness.panel.ReadGuestPane(), "the hovered agent has the stage to itself")
	assert.Empty(t, harness.panel.ReadBelowAnchors(), "nothing is split under it")
	assert.NotContains(t, harness.panel.ReadRespawnPanes(), infoPane, "and nothing is paged into a pane below")
}

const (
	stagePane      = ui.StagePane
	infoPane       = ui.InfoPane
	terminalRefuse = "Could not open a te"
)

func openTerminal(t *testing.T, params panelHarnessParams, agents ...*agentv1.Agent) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	rows := []proto.Message{newLiveWorkflow("w1", "backend")}
	for _, agent := range agents {
		rows = append(rows, agent)
		if agent.GetRole() != agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR {
			task := newTaskOf(agent)
			task.Cwd = xptr.Ptr(agent.GetCwd())
			rows = append(rows, task)
		}
	}
	harness.watch(buildSnapshot(rows...))
	harness.press("g", "g", "j")

	return harness
}

func newStagedShellAgent(dir string) *agentv1.Agent {
	agent := newShellAgent("a1", dir)
	agent.CreatedAt = "2026-09-20T12:01:00Z"
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

func newShellAgent(id, dir string) *agentv1.Agent {
	agent := newAgentAt(id, "w1", 0)
	agent.Cwd = dir

	return agent
}

func readShellDirs(harness *panelHarness) []string {
	var dirs []string
	for i, pane := range harness.panel.ReadRespawnPanes() {
		if pane == infoPane {
			dirs = append(dirs, harness.panel.ReadRespawnDirs()[i])
		}
	}

	return dirs
}

func readShells(harness *panelHarness) []string {
	var shells []string
	for i, pane := range harness.panel.ReadRespawnPanes() {
		if pane == infoPane {
			shells = append(shells, harness.panel.ReadRespawns()[i])
		}
	}

	return shells
}
