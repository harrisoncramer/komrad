package ui

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/gittest"
	"weave/internal/panes"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"

	"github.com/stretchr/testify/assert"
)

// TestInfoStaysShut pins that only the log key and a script's editor open the pane below
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

const panelViewer = "/dev/ttys004"

type recordingTmux struct {
	resizes     []string
	path        string
	swaps       int
	windowSizes map[string]string
	sessionOpts map[string]string
	respawns    []string
	width       int
	height      int
	// paneWidths is one pane's own width, falling back to width where a pane has no entry.
	paneWidths    map[string]int
	windowWidth   int
	sentKeys      []string
	detaches      int
	killed        []string
	belowAnchors  []string
	globals       map[string]string
	infoPane      string
	selected      []string
	respawnPanes  []string
	respawnDirs   []string
	deadPanes     map[string]bool
	sessionClient string
	zoomed        bool
	// keysElsewhere is the keyboard sitting in a pane other than the one being asked about.
	keysElsewhere bool
}

func (t *recordingTmux) ShowGlobal(name string) string  { return t.globals[name] }
func (t *recordingTmux) GetSessionClient(string) string { return t.sessionClient }
func (t *recordingTmux) SetSessionOption(_, name, value string) {
	if t.sessionOpts == nil {
		t.sessionOpts = map[string]string{}
	}

	t.sessionOpts[name] = value
}
func (t *recordingTmux) DetachSession(string)          { t.detaches++ }
func (t *recordingTmux) BindKey(string, ...string)     {}
func (t *recordingTmux) BindRootKey(string, ...string) {}
func (t *recordingTmux) UnbindRootKey(string)          {}
func (t *recordingTmux) UnbindKey(string)              {}
func (t *recordingTmux) GetHostPath() string           { return t.path }
func (t *recordingTmux) GetDefaultShell() string       { return "/bin/zsh" }
func (t *recordingTmux) SendKeys(target string, keys ...string) error {
	t.sentKeys = append(t.sentKeys, strings.Join(append([]string{target}, keys...), " "))

	return nil
}

// MovePanes replays a change's pane moves onto the fake, so a test asserting on swaps and
// zooms sees what the frame asked for whether or not they were collected first.
func (t *recordingTmux) MovePanes(moves []panes.Move) {
	for _, move := range moves {
		switch move.Kind {
		case panes.KindZoom:
			t.SetPaneZoomed(move.Pane, move.Zoomed)
		case panes.KindSwap:
			t.SwapPanes(move.Pane, move.Other)
		case panes.KindRespawn:
			t.RespawnPane(move.Pane, move.Dir, move.Argv...)
		case panes.KindResize:
			t.ResizePane(move.Pane, strconv.Itoa(move.Width))
		}
	}
}

func (t *recordingTmux) IsPaneAlive(pane string) bool    { return !t.deadPanes[pane] }
func (t *recordingTmux) SetPaneZoomed(_ string, on bool) { t.zoomed = on }

func (t *recordingTmux) GetWindowWidth(string) int { return t.windowWidth }

func (t *recordingTmux) GetPaneWidth(pane string) int {
	if width, ok := t.paneWidths[pane]; ok {
		return width
	}

	return t.width
}

// GetPaneFocus answers the way tmux was measured to: a pane the keys have moved off is neither
// the active one nor zoomed any more.
func (t *recordingTmux) GetPaneFocus(string) (focused, zoomed bool) {
	if t.keysElsewhere {
		return false, false
	}

	return true, t.zoomed
}

func (t *recordingTmux) GetPaneHeight(string) int                 { return t.height }
func (t *recordingTmux) SplitRight(string, string, string) string { return t.split("%1") }
func (t *recordingTmux) SplitLeft(string, string, string) string  { return t.split("%1") }

func (t *recordingTmux) SplitBelow(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return t.split("%2")
}

func (t *recordingTmux) SplitAbove(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return t.split("%2")
}

// split answers with the pane a split left, dropping the zoom the way tmux does.
func (t *recordingTmux) split(pane string) string {
	t.zoomed = false

	return pane
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

func (t *recordingTmux) GetSiblingPanes(string) []string { return nil }
func (t *recordingTmux) GetPaneOwner(string) string      { return "" }

// SwapPanes, BreakPane, KillPane and ResizePane each drop the zoom, the way tmux does.
func (t *recordingTmux) SwapPanes(string, string) {
	t.swaps++
	t.zoomed = false
}

func (t *recordingTmux) BreakPane(string) { t.zoomed = false }

func (t *recordingTmux) KillPane(pane string) {
	t.killed = append(t.killed, pane)
	t.zoomed = false
}

func (t *recordingTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}

// ResizeWindow records the size an agent's own window was put at, keyed by pane.
func (t *recordingTmux) ResizeWindow(pane string, width, height int) {
	if t.windowSizes == nil {
		t.windowSizes = map[string]string{}
	}

	t.windowSizes[pane] = fmt.Sprintf("%dx%d", width, height)
}

func (t *recordingTmux) IsPaneRunningCLI(pane string) bool { return pane != "" }

func (t *recordingTmux) ResizePane(pane, width string) {
	t.zoomed = false
	t.resizes = append(t.resizes, pane+" "+width)
}

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
		git:           gittest.NoRepo{},
		frame:         frame,
		shell:         tmux.GetDefaultShell(),
		keys:          config.DefaultKeybindings(),
		screen:        screenAgents,
		detailsHeight: config.DefaultDetailsHeight,
		listing: listing{workflows: []panel.Workflow{{
			ID:       "w1",
			Name:     "backend",
			Worktree: panel.WorktreeConfig{Enabled: true},
		}}, agents: []panel.AgentRow{
			{Agent: panel.Agent{
				Name:       "auth",
				Pane:       "%9",
				Cwd:        "/repo",
				WorkflowID: "w1",
			}, Alive: true},
		}}, agents: agentsScreen{rowCursor: rowCursor{cursor: 1}},
	}
}
