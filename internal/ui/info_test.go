package ui

import (
	"strings"
	"testing"

	"weave/config"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	"github.com/stretchr/testify/assert"
)

// TestInfoStaysShut pins that only the log key and an action's editor open the pane below
// the stage.
func TestInfoStaysShut(t *testing.T) {
	tmux := &recordingTmux{}
	m := infoModel(tmux)

	m.stageSelection()

	assert.Equal(t, infoNothing, m.info.kind)
	assert.Equal(t, "%9", m.frame.GuestPane(), "the hovered agent has the stage to itself")
	assert.Empty(t, tmux.belowAnchors, "nothing is split under it")
	assert.NotContains(t, tmux.respawnPanes, "%2", "and nothing is paged into a pane below")
}

// TestWorkflowContextMarkdown covers the document the stage pages for a workflow, which is
// the context its agents inherit.
func TestWorkflowContextMarkdown(t *testing.T) {
	workflow := workflowstore.Workflow{
		ID:      "w1",
		Name:    "api",
		Context: "shared notes",
	}

	assert.Contains(t, workflowContextMarkdown(workflow), "# api")
	assert.Contains(t, workflowContextMarkdown(workflow), "shared notes")

	workflow.Context = ""

	assert.Contains(t, workflowContextMarkdown(workflow), "no shared context set")
}

// TestWorkflowFields covers what the details draw for a workflow: how many agents it holds,
// then the defaults future agents under it inherit.
func TestWorkflowFields(t *testing.T) {
	fields := workflowFields(workflowstore.Workflow{
		ID:            "w1",
		Name:          "api",
		Harness:       "codex",
		Cwd:           "/repo/api",
		Flags:         "--model opus",
		SetupAction:   "bootstrap",
		CleanupAction: "teardown",
	}, 3)

	assert.Equal(t, []recordField{
		{"agents", "3"},
		{"default agent", "codex"},
		{"directory", "/repo/api"},
		{"flags", "--model opus"},
		{"setup", "bootstrap"},
		{"cleanup", "teardown"},
		{"workflow_id", "w1"},
	}, fields)
}

// TestWorkflowFieldsWorktree pins that a worktree workflow's details draw the worktree
// settings the user chose rather than the setup and cleanup scripts weave generated.
func TestWorkflowFieldsWorktree(t *testing.T) {
	workflow := workflowstore.Workflow{
		ID:            "w1",
		Name:          "api",
		Harness:       "codex",
		Cwd:           "/repo/api",
		SetupAction:   "weave-worktree-setup-w1",
		CleanupAction: "weave-worktree-cleanup-w1",
		Worktree: workflowstore.WorktreeConfig{
			Enabled:      true,
			BaseBranch:   "main",
			CopyPaths:    []string{".env", ".envrc"},
			SymlinkPaths: []string{"node_modules"},
		},
	}

	assert.Equal(t, []recordField{
		{"agents", "3"},
		{"default agent", "codex"},
		{"directory", "/repo/api"},
		{"flags", "—"},
		{"worktree", "on"},
		{"base branch", "main"},
		{"copy", ".env, .envrc"},
		{"symlink", "node_modules"},
		{"workflow_id", "w1"},
	}, workflowFields(workflow, 3))

	workflow.Worktree.CopyPaths = nil
	workflow.Worktree.SymlinkPaths = nil
	workflow.Worktree.BaseBranch = ""

	fields := workflowFields(workflow, 3)

	assert.Contains(t, fields, recordField{"base branch", "—"})
	assert.NotContains(t, fields, recordField{"copy", ""})
	assert.NotContains(t, fields, recordField{"setup", "weave-worktree-setup-w1"})
}

type recordingTmux struct {
	path          string
	swaps         int
	respawns      []string
	width         int
	height        int
	sentKeys      []string
	windows       []string
	openWindowErr error
	detaches      int
	killed        []string
	belowAnchors  []string
	globals       map[string]string
	infoPane      string
	selected      []string
	respawnPanes  []string
	respawnDirs   []string
	deadPanes     map[string]bool
}

func (t *recordingTmux) ShowGlobal(string) string                { return "" }
func (t *recordingTmux) SetSessionOption(string, string, string) {}
func (t *recordingTmux) DetachSession(string)                    { t.detaches++ }
func (t *recordingTmux) BindKey(string, ...string)               {}
func (t *recordingTmux) GetClientPath(string) string             { return t.path }
func (t *recordingTmux) SendKeys(target string, keys ...string) error {
	t.sentKeys = append(t.sentKeys, strings.Join(append([]string{target}, keys...), " "))

	return nil
}
func (t *recordingTmux) IsPaneAlive(pane string) bool             { return !t.deadPanes[pane] }
func (t *recordingTmux) GetPaneWidth(string) int                  { return t.width }
func (t *recordingTmux) GetPaneHeight(string) int                 { return t.height }
func (t *recordingTmux) CapturePane(string) string                { return "" }
func (t *recordingTmux) SplitRight(string, string, string) string { return "%1" }
func (t *recordingTmux) SplitLeft(string, string, string) string  { return "%1" }

func (t *recordingTmux) SplitBelow(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return "%2"
}

func (t *recordingTmux) SplitAbove(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return "%2"
}

func (t *recordingTmux) SetInfoPane(pane string) {
	t.infoPane = pane
}

func (t *recordingTmux) SelectPane(pane string) {
	t.selected = append(t.selected, pane)
}

func (t *recordingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *recordingTmux) SwapPanes(string, string)        { t.swaps++ }
func (t *recordingTmux) GetSiblingPanes(string) []string { return nil }
func (t *recordingTmux) GetPaneOwner(string) string      { return "" }
func (t *recordingTmux) BreakPane(string)                {}
func (t *recordingTmux) KillPane(pane string)            { t.killed = append(t.killed, pane) }

func (t *recordingTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}

func (t *recordingTmux) OpenWindow(_, _, dir string, argv ...string) error {
	if t.openWindowErr != nil {
		return t.openWindowErr
	}

	t.windows = append(t.windows, strings.Join(append([]string{dir}, argv...), " "))

	return nil
}

func (t *recordingTmux) ResizePane(string, string) {}

func (t *recordingTmux) RespawnPane(pane, dir string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
	t.respawnDirs = append(t.respawnDirs, dir)
}

func infoModel(tmux *recordingTmux) model {
	frame := layout.New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	return model{
		tmux:          tmux,
		frame:         frame,
		keys:          config.DefaultKeybindings(),
		screen:        screenAgents,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "backend"}}, agents: []agentItem{
			{agent: agentstore.Agent{
				Name:       "auth",
				Pane:       "%9",
				Cwd:        "/repo",
				WorkflowID: "w1",
			}, alive: true},
		}}, tree: treeState{rowCursor: rowCursor{cursor: 1}},
	}
}
