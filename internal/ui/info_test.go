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

// TestInfoStaysShut covers the pane below the stage, which nothing about the row under the
// cursor opens. Only the log key and an action's editor put anything down there.
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
// the context its agents inherit. Its fields are drawn in the details beside this.
func TestWorkflowContextMarkdown(t *testing.T) {
	workflow := workflowstore.Workflow{ID: "w1", Name: "api", Context: "shared notes"}

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
	}, fields)
}

// recordingTmux records what the panel puts on its frame, so a test can tell a swapped-in
// agent pane from a respawned pager preview. How the frame itself is laid out is the layout
// package's own to cover, so nothing about splits or sizes is kept here. Every other call is
// an inert stub.
type recordingTmux struct {
	// path is what GetClientPath reports, the directory the panel falls back to for
	// a form left blank.
	path     string
	swaps    int
	respawns []string
	// width is what GetPaneWidth reports for every pane, the columns markdown renders to.
	width int
	// windows are the windows opened for the user's own client, each recorded as its
	// directory followed by the argv it runs. openWindowErr makes the next one fail.
	windows       []string
	openWindowErr error
	// detaches counts the sessions the popup has been closed by detaching from.
	detaches int
	killed   []string
	// belowAnchors are the panes the info pane has been split beneath.
	belowAnchors []string
	globals      map[string]string
	// infoPane is the pane the panel said it opened its info region in, which is how an
	// action is told which pane to fill.
	infoPane string
	// respawnPanes are the panes each respawn went to, alongside the argv in respawns.
	respawnPanes []string
	// deadPanes are the panes IsPaneAlive answers no for, which is how a test stands a pane
	// up and then takes it away.
	deadPanes map[string]bool
}

func (t *recordingTmux) ShowGlobal(string) string                 { return "" }
func (t *recordingTmux) SetSessionOption(string, string, string)  {}
func (t *recordingTmux) DetachSession(string)                     { t.detaches++ }
func (t *recordingTmux) BindKey(string, ...string)                {}
func (t *recordingTmux) GetClientPath(string) string              { return t.path }
func (t *recordingTmux) SendKeys(string, ...string) error         { return nil }
func (t *recordingTmux) IsPaneAlive(pane string) bool             { return !t.deadPanes[pane] }
func (t *recordingTmux) GetPaneWidth(string) int                  { return t.width }
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

func (t *recordingTmux) RespawnPane(pane, _ string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
}

// infoModel builds an agents model holding one live agent, with the cursor on
// it. Row 0 is the workflow header the agent sits under, so the agent is row 1.
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
			{agent: agentstore.Agent{Name: "auth", Pane: "%9", Cwd: "/repo", WorkflowID: "w1"}, alive: true},
		}}, tree: treeState{rowCursor: rowCursor{cursor: 1}},
	}
}
