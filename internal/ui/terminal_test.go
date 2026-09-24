package ui_test

import (
	"testing"

	"weave/internal/engine"
	"weave/internal/ui"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	infoPane       = "%2"
	terminalRefuse = "Could not open a te"
)

// TestTerminal covers the key that opens a shell in the pane under the stage.
func TestTerminal(t *testing.T) {
	t.Run("the shell opens in the hovered agent's directory and takes the keys", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", dir))
		require.Equal(t, 1, harness.panel.CountSwaps(), "the live agent is staged to begin with")

		harness.press("T")

		assert.Equal(t, 1, harness.panel.CountSwaps(), "the agent's own pane is left where it is")
		assert.Equal(t, []string{infoPane}, harness.panel.ReadRespawnPanes())
		assert.Equal(t, []string{"/bin/zsh"}, harness.panel.ReadRespawns(), "the shell is named so the pane cannot rerun what it held last")
		assert.Equal(t, []string{dir}, harness.panel.ReadRespawnDirs())
		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes())
		assert.Contains(t, readExpandedLegend(harness), "back to the terminal")
	})

	t.Run("the shell takes the pane the logs were in", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{settings: ui.PanelSettings{RunLogsDir: t.TempDir()}}, newShellAgent("a1", dir))
		harness.press("l")
		require.Len(t, harness.panel.ReadBelowAnchors(), 1)

		harness.press("T")

		assert.Len(t, harness.panel.ReadBelowAnchors(), 1, "the pane the logs were in is reused")
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadRespawnPanes())
		assert.Equal(t, dir, harness.panel.ReadRespawnDirs()[1])
	})

	t.Run("the key again hands the shell the keys rather than respawning it", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", t.TempDir()))

		harness.press("T", "T")

		assert.Len(t, harness.panel.ReadRespawns(), 1)
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the stage drawn again leaves the shell running", func(t *testing.T) {
		agent := newShellAgent("a1", t.TempDir())
		harness := openTerminal(t, panelHarnessParams{}, agent)
		harness.press("T")

		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), agent))
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), agent))

		assert.Len(t, harness.panel.ReadRespawns(), 1)
	})

	t.Run("the shell stays where it opened as the cursor moves", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", first), newShellAgent("a2", second))
		harness.press("T")

		harness.press("j")

		assert.Equal(t, []string{first}, readShellDirs(harness))
		assert.Contains(t, readExpandedLegend(harness), "terminal here", "the legend says the key moves the shell")
	})

	t.Run("the key over another agent moves the shell there", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", first), newShellAgent("a2", second))

		harness.press("T", "j", "T")

		assert.Equal(t, []string{first, second}, readShellDirs(harness))
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the key over a sibling in the same directory keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", dir), newShellAgent("a2", dir))
		harness.press("T", "j")
		require.Contains(t, readExpandedLegend(harness), "back to the terminal")

		harness.press("T")

		assert.Equal(t, []string{dir}, readShellDirs(harness), "one directory is one shell")
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the key over a workflow header keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", dir))

		harness.press("T", "k", "T")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("the key over an agent whose worktree has gone says why and keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", dir), newShellAgent("a2", "/no/such/worktree"))

		harness.press("T", "j", "T")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
		assert.Equal(t, []string{infoPane, infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("an agent whose worktree has gone opens no shell", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", "/no/such/worktree"))

		harness.press("T")

		assert.Empty(t, harness.panel.ReadRespawns())
		assert.Contains(t, harness.readFrame(), terminalRefuse)
	})

	t.Run("a workflow header opens no shell", func(t *testing.T) {
		harness := openTerminal(t, panelHarnessParams{}, newShellAgent("a1", t.TempDir()))

		harness.press("k", "T")

		assert.Empty(t, harness.panel.ReadRespawns())
	})

	t.Run("a remote engine's shell is opened over the transport", func(t *testing.T) {
		remote := panelHarnessParams{engine: engine.Remote{Destination: "ubuntu@engine"}}
		harness := openTerminal(t, remote, newShellAgent("a1", "/var/lib/weave/worktrees/01a0c466"))

		harness.press("T")

		assert.NotContains(t, harness.readFrame(), terminalRefuse, "nothing is stat'd on this machine")
		require.NotEmpty(t, harness.panel.ReadRespawns())
		last := len(harness.panel.ReadRespawns()) - 1
		assert.Equal(t, infoPane, harness.panel.ReadRespawnPanes()[last])
		assert.Equal(t, `ssh -t ubuntu@engine cd '/var/lib/weave/worktrees/01a0c466' && exec "${SHELL:-sh}"`, harness.panel.ReadRespawns()[last])
		assert.Empty(t, harness.panel.ReadRespawnDirs()[last], "the pane is not rooted at a directory this machine lacks")
		assert.Equal(t, []string{infoPane}, harness.panel.ReadSelectedPanes())
	})

	t.Run("a remote engine's agent with no directory opens no shell", func(t *testing.T) {
		remote := panelHarnessParams{engine: engine.Remote{Destination: "ubuntu@engine"}}
		harness := openTerminal(t, remote, newShellAgent("a1", ""))

		harness.press("T")

		assert.Empty(t, readShellDirs(harness))
		assert.Contains(t, harness.readFrame(), terminalRefuse)
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

func newShellAgent(id, dir string) *agentv1.Agent {
	agent := newAgentAt(id, "w1", 0)
	if id == "a1" {
		agent = newAgentAt(id, "w1", 1)
		agent.PaneId = "%9"
		agent.Alive = proto.Bool(true)
	}
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

func readExpandedLegend(harness *panelHarness) string {
	harness.press("?")
	defer harness.press("?")

	return readLegend(harness.readFrame())
}
