package ui_test

import (
	"testing"
	"time"

	"komrad/internal/engine"
	"komrad/internal/ui/paneltest"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TestTerminal covers the subpage that opens a shell in the pane under the stage.
func TestTerminal(t *testing.T) {
	t.Run("from a task's row the shell opens in its directory and takes the keys", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(dir))

		harness.Press("space", "t")

		assert.Equal(t, []string{"/bin/zsh"}, readShells(harness), "the shell is named so the pane cannot rerun what it held last")
		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Equal(t, []string{infoPane}, harness.Tmux().SelectedPanes())
	})

	t.Run("from a task's row the shell takes the stage's rows", func(t *testing.T) {
		harness := openTerminal(t, paneltest.Params{}, newShellAgent("a1", t.TempDir()))

		harness.Press("space", "t")

		assert.Contains(t, harness.Tmux().KilledPanes(), stagePane)
		assert.Empty(t, harness.Tmux().StagePane())
	})

	t.Run("picked again it hands the shell the keys rather than respawning it", func(t *testing.T) {
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(t.TempDir()))

		harness.Press("space", "t", "space", "t")

		assert.Len(t, readShells(harness), 1)
		assert.Equal(t, []string{infoPane, infoPane}, harness.Tmux().SelectedPanes())
	})

	t.Run("the stage rendered again leaves the shell running", func(t *testing.T) {
		agent := newStagedShellAgent(t.TempDir())
		harness := openTerminal(t, paneltest.Params{}, agent)
		harness.Press("space", "t")

		harness.Watch(paneltest.Snapshot(withTasks([]proto.Message{paneltest.Workflow("w1", "backend"), agent})...))
		harness.Watch(paneltest.Snapshot(withTasks([]proto.Message{paneltest.Workflow("w1", "backend"), agent})...))

		assert.Len(t, readShells(harness), 1)
	})

	t.Run("the shell keeps the keys as the listing changes", func(t *testing.T) {
		agent := newStagedShellAgent(t.TempDir())
		harness := openTerminal(t, paneltest.Params{}, agent)
		harness.Press("space", "t")
		selected := len(harness.Tmux().SelectedPanes())
		harness.Tmux().SetActivePane(harness.Tmux().StagePane())
		harness.Send(tea.BlurMsg{})

		harness.Watch(paneltest.Snapshot(withTasks([]proto.Message{paneltest.Workflow("w1", "backend"), agent})...))

		assert.Empty(t, harness.Tmux().SelectedPanes()[selected:])
		assert.False(t, harness.Tmux().IsZoomed())
	})

	t.Run("the shell stays where it opened as the cursor moves", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(first), newShellAgent("a2", second))
		harness.Press("space", "t")

		harness.Press("j")

		assert.Equal(t, []string{first}, readShellDirs(harness))
	})

	t.Run("picked on another agent it moves the shell there", func(t *testing.T) {
		first, second := t.TempDir(), t.TempDir()
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(first), newShellAgent("a2", second))

		harness.Press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{first, second}, readShellDirs(harness))
		assert.Equal(t, []string{infoPane, infoPane}, harness.Tmux().SelectedPanes())
	})

	t.Run("picked on a sibling in the same directory it keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(dir), newShellAgent("a2", dir))

		harness.Press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness), "one directory is one shell")
		assert.Equal(t, []string{infoPane, infoPane}, harness.Tmux().SelectedPanes())
	})

	t.Run("picked on a task whose worktree has gone it says why and keeps the shell", func(t *testing.T) {
		dir := t.TempDir()
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(dir), newShellAgent("a2", "/no/such/worktree"))

		harness.Press("space", "t", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
		assert.Contains(t, harness.Frame(), terminalRefuse)
		assert.Equal(t, []string{infoPane, infoPane}, harness.Tmux().SelectedPanes())
	})

	t.Run("a task whose worktree has gone opens no shell", func(t *testing.T) {
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent("/no/such/worktree"))

		harness.Press("space", "t")

		assert.Empty(t, harness.Tmux().Respawns())
		assert.Contains(t, harness.Frame(), terminalRefuse)
	})

	t.Run("a workflow opens the shell in its orchestrator's directory", func(t *testing.T) {
		workflowDir, agentDir := t.TempDir(), t.TempDir()
		orchestrator := paneltest.Orchestrator()
		orchestrator.Cwd = workflowDir
		harness := openTerminal(t, paneltest.Params{}, orchestrator, newShellAgent("a2", agentDir))

		harness.Press("k", "space", "t", "j", "space", "t")

		assert.Equal(t, []string{workflowDir, agentDir}, readShellDirs(harness))
	})

	t.Run("a task with no checkout opens the shell in its workflow's directory", func(t *testing.T) {
		workflowDir := t.TempDir()
		workflow := paneltest.Workflow("w1", "backend")
		workflow.Cwd = workflowDir
		agent := newStagedShellAgent(t.TempDir())
		harness := paneltest.Start(t, paneltest.Params{})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(workflow, agent, paneltest.TaskOf(agent)))
		harness.Press("g", "g", "j", "space", "t")

		assert.Equal(t, []string{workflowDir}, readShellDirs(harness))
	})

	t.Run("an archived task opens the shell in its worktree rather than where its agent ran", func(t *testing.T) {
		dir := t.TempDir()
		agent := newShellAgent("a1", t.TempDir())
		agent.State = agentv1.AgentState_AGENT_STATE_FAILED_TO_CREATE
		task := paneltest.TaskOf(agent)
		task.Cwd, task.ArchivedAt = xptr.Ptr(dir), archivedAt
		harness := paneltest.Start(t, paneltest.Params{})
		harness.AnswerWorktreeReads()
		harness.Watch(paneltest.Snapshot(paneltest.Workflow("w1", "backend"), agent, task))
		harness.Press("g", "g", "j", "space", "t")

		assert.Equal(t, []string{dir}, readShellDirs(harness))
	})

	t.Run("a remote engine with no way to reach it opens no shell", func(t *testing.T) {
		harness := openTerminal(t, paneltest.Params{Engine: engine.Remote{URL: "https://komrad.example.com"}}, newStagedShellAgent("/var/lib/komrad/worktrees/01a0c466"))

		harness.Press("space", "t")

		assert.Empty(t, readShellDirs(harness))
		assert.Contains(t, harness.Frame(), terminalRefuse)
	})

	t.Run("a remote engine's shell is opened over the engine's api", func(t *testing.T) {
		remote := paneltest.Params{
			Engine:       engine.Remote{URL: "https://komrad.example.com"},
			ShellCommand: []string{"komrad", "shell"},
		}
		harness := openTerminal(t, remote, newStagedShellAgent("/var/lib/komrad/worktrees/01a0c466"))

		harness.Press("space", "t")

		assert.NotContains(t, harness.Frame(), terminalRefuse, "nothing is stat'd on this machine")
		require.NotEmpty(t, harness.Tmux().Respawns())
		last := len(harness.Tmux().Respawns()) - 1
		assert.Equal(t, infoPane, harness.Tmux().RespawnPanes()[last])
		assert.Equal(t, "komrad shell -- /var/lib/komrad/worktrees/01a0c466", harness.Tmux().Respawns()[last])
		assert.Empty(t, harness.Tmux().RespawnDirs()[last], "the pane is not rooted at a directory this machine lacks")
		assert.Equal(t, []string{infoPane}, harness.Tmux().SelectedPanes())
	})

	t.Run("a task and workflow with no directory have it gray and open no shell", func(t *testing.T) {
		remote := paneltest.Params{
			Engine:       engine.Remote{URL: "https://komrad.example.com"},
			ShellCommand: []string{"komrad", "shell"},
		}
		harness := openTerminal(t, remote, newStagedShellAgent(""))

		harness.Press("space")
		require.Regexp(t, readOffEntry("terminal", "t"), harness.RawFrame())
		harness.Press("t")

		assert.Empty(t, readShellDirs(harness))
		assert.Contains(t, harness.Frame(), terminalRefuse)
	})

	t.Run("the shell stays open while the switcher steps through a view that cannot hold it", func(t *testing.T) {
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(t.TempDir()))

		harness.Press("space", "t", "tab", "tab")

		assert.NotContains(t, harness.Tmux().KilledPanes(), infoPane)
	})

	t.Run("the shell goes once the switcher closes on a view that cannot hold it", func(t *testing.T) {
		harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(t.TempDir()))

		harness.Press("space", "t", "tab", "tab", "esc")

		assert.Contains(t, harness.Tmux().KilledPanes(), infoPane)
	})
}

// TestInfo covers the pane below the stage staying shut until something asks for it.
func TestInfo(t *testing.T) {
	harness := openTerminal(t, paneltest.Params{}, newStagedShellAgent(t.TempDir()))

	assert.Equal(t, "%9", harness.Tmux().GuestPane(), "the hovered agent has the stage to itself")
	assert.Empty(t, harness.Tmux().BelowAnchors(), "nothing is split under it")
	assert.NotContains(t, harness.Tmux().RespawnPanes(), infoPane, "and nothing is paged into a pane below")
}

const (
	stagePane      = paneltest.StagePane
	infoPane       = paneltest.InfoPane
	terminalRefuse = "Could not open a te"
)

func openTerminal(t *testing.T, params paneltest.Params, agents ...*agentv1.Agent) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	harness.AnswerWorktreeReads()
	rows := []proto.Message{paneltest.Workflow("w1", "backend")}
	for _, agent := range agents {
		rows = append(rows, agent)
		if agent.GetRole() != agentv1.AgentRole_AGENT_ROLE_ORCHESTRATOR {
			task := paneltest.TaskOf(agent)
			task.Cwd = xptr.Ptr(agent.GetCwd())
			rows = append(rows, task)
		}
	}
	harness.Watch(paneltest.Snapshot(rows...))
	harness.Press("g", "g", "j")

	return harness
}

func newStagedShellAgent(dir string) *agentv1.Agent {
	agent := newShellAgent("a1", dir)
	agent.CreatedAt = timestamppb.New(time.Date(2026, 9, 20, 12, 1, 0, 0, time.UTC))
	agent.PaneId = "%9"
	agent.Alive = xptr.Ptr(true)

	return agent
}

func newShellAgent(id, dir string) *agentv1.Agent {
	agent := newAgentAt(id, "w1", 0)
	agent.Cwd = dir

	return agent
}

func readShellDirs(harness *paneltest.Panel) []string {
	var dirs []string
	for i, pane := range harness.Tmux().RespawnPanes() {
		if pane == infoPane {
			dirs = append(dirs, harness.Tmux().RespawnDirs()[i])
		}
	}

	return dirs
}

func readShells(harness *paneltest.Panel) []string {
	var shells []string
	for i, pane := range harness.Tmux().RespawnPanes() {
		if pane == infoPane {
			shells = append(shells, harness.Tmux().Respawns()[i])
		}
	}

	return shells
}
