package ui_test

import (
	"testing"

	"komrad/internal/engine"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	infoPane       = "%2"
	terminalRefuse = "Could not open a te"
)

// TestTerminal covers the subpage that opens a shell in the pane under the stage.
func TestTerminal(t *testing.T) {
	t.Run("the shell opens in the agent's directory and takes the keys", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(dir))
		require.Equal(t, 1, harness.panel.CountSwaps(), "the live agent is staged to begin with")

		harness.press("space", "t")

		assert.Equal(t, 1, harness.panel.CountSwaps(), "the agent's own pane is left where it is")
		assert.Equal(t, []string{infoPane}, harness.panel.ReadRespawnPanes())
		assert.Equal(t, []string{"/bin/zsh"}, harness.panel.ReadRespawns(), "the shell is named so the pane cannot rerun what it held last")
		assert.Equal(t, []string{dir}, harness.panel.ReadRespawnDirs())
		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("picked again it hands the shell the keys rather than respawning it", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

		harness.press("space", "t", "space", "t")

		assert.Len(t, harness.panel.ReadRespawns(), 1)
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the stage drawn again leaves the shell running", func(t *testing.T) {
		agent := newStagedShellAgent(t.TempDir())
		harness := openTerminal(t, panelHarnessParams{}, agent)
		harness.press("space", "t")

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), agent))
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), agent))

		assert.Len(t, harness.panel.ReadRespawns(), 1)
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

	t.Run("picked on an agent whose worktree has gone it says why and keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(dir), newShellAgent("a2", "/no/such/worktree"))

		harness.press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("an agent whose worktree has gone opens no shell", func(t *testing.T) {
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

	t.Run("a remote engine with no way to reach it opens no shell", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{engine: engine.Remote{Destination: "ubuntu@engine"}}, newStagedShellAgent("/var/lib/komrad/worktrees/01a0c466"))

		harness.press("space", "t")

		assert.Empty(t, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
	})

	t.Run("a remote engine's shell is opened over the engine's api", func(t *testing.T) {
		remote := panelHarnessParams{
			engine:       engine.Remote{Destination: "ubuntu@engine"},
			shellCommand: []string{"komrad", "shell", "--addr", "127.0.0.1:19001"},
		}
		harness := openTerminal(t, remote, newStagedShellAgent("/var/lib/komrad/worktrees/01a0c466"))

		harness.press("space", "t")

		assert.NotContains(t, harness.readFrame(), terminalRefuse, "nothing is stat'd on this machine")
		require.NotEmpty(t, harness.panel.ReadRespawns())
		last := len(harness.panel.ReadRespawns()) - 1
		assert.Equal(t, infoPane, harness.panel.ReadRespawnPanes()[last])
		assert.Equal(t, "komrad shell --addr 127.0.0.1:19001 -- /var/lib/komrad/worktrees/01a0c466", harness.panel.ReadRespawns()[last])
		assert.Empty(t, harness.panel.ReadRespawnDirs()[last], "the pane is not rooted at a directory this machine lacks")
		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("an agent with no directory has it gray and opens no shell", func(t *testing.T) {
		remote := panelHarnessParams{
			engine:       engine.Remote{Destination: "ubuntu@engine"},
			shellCommand: []string{"komrad", "shell", "--addr", "127.0.0.1:19001"},
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

		harness.press("space", "t", "tab")

		assert.Empty(t, harness.panel.ReadKilledPanes())
	})

	t.Run("the shell goes once the switcher closes on a view that cannot hold it", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newStagedShellAgent(t.TempDir()))

		harness.press("space", "t", "tab", "esc")

		assert.Equal(t, []string{infoPane}, harness.panel.ReadKilledPanes())
	})
}

func openTerminal(t *testing.T, params panelHarnessParams, agents ...*agentv1.Agent) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	rows := []proto.Message{newLiveWorkflow("w1", "backend")}
	for _, agent := range agents {
		rows = append(rows, agent)
	}
	harness.watch(buildSnapshot(rows...))

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
