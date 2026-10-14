package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"komrad/internal/engine"
	"komrad/internal/ui"
	"komrad/lib/xptr"
	"komrad/proto/gen/agent/v1/agentv1connect"
	watchv1 "komrad/proto/gen/watch/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestScreensStage covers what the panel shows beside the hovered row and how much of the window it holds.
func TestScreensStage(t *testing.T) {
	t.Parallel()

	t.Run("a row with no pane renders its notes beside the list and takes the window", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "always run the linter")
		assert.Contains(t, readListRows(frame), "backend")
		for _, line := range splitLines(harness.readRendered()) {
			assert.Equal(t, 200, ansi.StringWidth(line), "every row of the frame is squared off")
		}
	})

	t.Run("a running agent's own pane gets the window back", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		assert.False(t, harness.panel.IsZoomed())
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Equal(t, 197, measureListRule(harness.readFrame()), "and the list goes back to the whole of the pane")
	})

	t.Run("a dormant agent says why it has no pane", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 200})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newDormant("auth")))

		harness.press("j")
		harness.fireTimers()

		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, harness.readFrame(), "dormant")
	})

	t.Run("leaving a running agent for a row with no pane takes the window", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		harness.press("k")
		harness.fireTimers()

		assert.True(t, harness.panel.IsZoomed(), "tmux is holding the zoom rather than the frame only believing so")
	})

	t.Run("a pane moved by hand gives up both halves until the keys come back", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")

		harness.panel.SetKeysElsewhere(true)
		harness.send(tea.BlurMsg{})

		frame := harness.readFrame()
		assert.NotContains(t, frame, "always run the linter", "the frame is told what tmux did rather than assuming")
		assert.Equal(t, 199, measureListRule(frame), "so the list is rendered in the whole of the pane")

		harness.watch(buildSnapshot(newNotedWorkflow(), newRunning()))

		assert.False(t, harness.panel.IsZoomed(), "a listing does not zoom the panel over the pane the keys are on")

		harness.panel.SetKeysElsewhere(false)
		harness.send(tea.FocusMsg{})

		assert.Contains(t, harness.readFrame(), "always run the linter", "and takes the window back once the keys return")
	})

	t.Run("an unfocused terminal keeps the window", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")
		before := harness.readFrame()

		harness.send(tea.BlurMsg{})

		assert.Equal(t, before, harness.readFrame(), "nothing is laid out again behind an unfocused terminal")
	})

	t.Run("a running agent's window is kept at the size of the stage", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		sizes := harness.panel.ReadWindowSizes()
		assert.Contains(t, sizes, "%9")
		assert.NotContains(t, sizes, "", "a dormant agent has no window to size")
	})

	t.Run("a remote engine's agent windows are left alone", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{width: 200, engine: engine.Remote{Forward: []string{"ssh", "engine"}}})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newNotedWorkflow(), newRunning()))
		harness.press("j")
		harness.fireTimers()

		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		assert.Empty(t, harness.panel.ReadWindowSizes(), "no window on the panel's own server is touched")
	})

	t.Run("a remote engine's agent is attached over the engine's api", func(t *testing.T) {
		t.Parallel()

		harness := newPanelHarness(t, panelHarnessParams{
			width:           200,
			engine:          engine.Remote{Destination: "ubuntu@engine"},
			terminalCommand: []string{"komrad", "terminal", "--addr", "127.0.0.1:19001"},
		})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newNotedWorkflow(), newRunning()))
		harness.press("j")
		harness.fireTimers()

		assert.Contains(t, harness.panel.ReadRespawns(), "komrad terminal --addr 127.0.0.1:19001 -- a1")
	})
}

// TestScreensDivider covers a divider the reader dragged staying where they put it.
func TestScreensDivider(t *testing.T) {
	t.Parallel()

	t.Run("a widened window puts the dragged width back", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)

		harness.panel.SetWindowWidth(300)
		harness.panel.SetPaneWidth("%0", 75)
		harness.panel.SetPaneWidth("%9", 224)
		harness.send(tea.WindowSizeMsg{Width: 75, Height: defaultHarnessHeight})

		assert.Contains(t, harness.panel.ReadResizes(), "%0 50")
	})

	t.Run("the dragged width survives the sidebar taking the window", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)
		harness.press("k")
		harness.fireTimers()

		harness.panel.SetPaneWidth("%0", 200)
		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		assert.Equal(t, 47, measureListRule(harness.readFrame()))
	})

	t.Run("the dragged width survives tmux dropping the zoom", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)
		harness.press("k")
		harness.fireTimers()
		harness.panel.SetPaneWidth("%0", 200)
		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		harness.panel.SetPaneWidth("%0", 50)
		harness.send(tea.WindowSizeMsg{Width: 50, Height: defaultHarnessHeight})
		harness.panel.SetPaneWidth("%0", 200)
		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		assert.Equal(t, 47, measureListRule(harness.readFrame()))
	})

	t.Run("a size tmux has already moved past is not rendered", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)
		harness.press("k")
		harness.fireTimers()
		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})
		before := harness.readRendered()

		harness.settle(harness.panel.Update(tea.WindowSizeMsg{Width: 100, Height: defaultHarnessHeight}))

		assert.Equal(t, before, harness.readRendered())
	})

	t.Run("a size read back before tmux moved the pane is not rendered", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)
		harness.press("k")
		harness.fireTimers()
		harness.panel.SetPaneWidth("%0", 200)
		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		harness.panel.SetPaneWidth("%0", 50)
		harness.press("j")
		harness.fireTimersUnsettled()
		before := harness.readRendered()

		harness.settle(harness.panel.Update(ui.NewSizeRead(200, defaultHarnessHeight)))

		assert.Equal(t, before, harness.readRendered())
	})
}

// TestScreensSidebarWidth covers how wide the list is rendered beside a document.
func TestScreensSidebarWidth(t *testing.T) {
	tests := []struct {
		name    string
		columns int
		want    int
	}{
		{
			name:    "a configured width narrower than a list of names is widened to one",
			columns: 8,
			want:    30,
		},
		{
			name:    "a configured width is capped at what leaves the document its floor",
			columns: 190,
			want:    156,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{
				width:        200,
				sidebarWidth: tt.columns,
			})
			harness.watch(buildSnapshot(newNotedWorkflow()))

			assert.Equal(t, tt.want, measureListRule(harness.readFrame()))
		})
	}

	t.Run("a divider dragged past the document's floor is rendered where it is", func(t *testing.T) {
		harness := openStaged(t, "g", "g", "j")
		harness.panel.SetPaneWidth("%0", 174)
		harness.panel.SetPaneWidth("%9", 25)
		harness.send(tea.WindowSizeMsg{Width: 174, Height: defaultHarnessHeight})

		harness.press("k")
		harness.fireTimers()
		harness.panel.SetPaneWidth("%0", 200)
		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		frame := harness.readFrame()
		assert.Equal(t, 171, measureListRule(frame), "the window less the stage, the divider and its gutters")
		assert.Contains(t, frame, "always run the linter", "and the document keeps what is left")
	})
}

// TestScreensFitATightPane covers a list screen in a pane only just tall enough for its chrome.
func TestScreensFitATightPane(t *testing.T) {
	t.Parallel()

	const fitsFrom, tall = 5, 40

	tests := []struct {
		name  string
		width int
		press []string
	}{
		{
			name:  "plain",
			width: 80,
		},
		{
			name:  "with the help expanded",
			width: 80,
			press: []string{"?"},
		},
		{
			name:  "with a message in the stack",
			width: 80,
			press: []string{"enter"},
		},
		{
			name:  "with the region holding a message",
			width: 80,
			press: []string{"enter", "ctrl+g"},
		},
		{
			name:  "with the record focused under the expanded legend",
			width: 30,
			press: []string{"pick:record", "?"},
		},
		{
			name:  "with the record focused under the expanded legend at the width its own legend folds",
			width: 35,
			press: []string{"pick:record", "?"},
		},
		{
			name:  "with a search standing under the expanded legend",
			width: 30,
			press: []string{"?", "/", "a", "enter"},
		},
		{
			name:  "with a search standing under the expanded legend at the width its legend folds",
			width: 35,
			press: []string{"?", "/", "a", "enter"},
		},
		{
			name:  "with a search being typed over the focused record",
			width: 30,
			press: []string{"pick:record", "?", "/", "a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, screen := range buildListScreens() {
				harness := openCrowded(t, panelHarnessParams{width: tt.width, height: tall})
				harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))
				harness.press(slices.Concat(screen.press, []string{"G"}, tt.press)...)

				for height := fitsFrom; height <= tall; height++ {
					resize(harness, tt.width, height)
					frame := harness.readFrame()

					assert.Len(t, splitLines(frame), height, "%s in a pane %d lines tall", screen.name, height)
					if height > fitsFrom {
						assert.NotEmpty(t, readHoveredRow(frame), "%s keeps the hovered row on screen in a pane %d lines tall", screen.name, height)
					}
				}
			}
		})
	}
}

// TestScreensOutgrowNoPane covers every screen rendering inside the pane it was given, whatever the names in it.
func TestScreensOutgrowNoPane(t *testing.T) {
	t.Parallel()

	widths := []int{38, 39, 40, 41, 42, 44, 46, 50, 60, 80, 120, 200}

	tests := buildListScreens()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, width := range widths {
				for _, height := range []int{28, 60} {
					harness := openLongNamed(t, panelHarnessParams{width: width, height: height})
					harness.press(tt.press...)

					for range 2 {
						frame := harness.readFrame()
						assert.LessOrEqual(t, len(splitLines(frame)), height, "taller than a %dx%d pane", width, height)
						for i, line := range splitLines(frame) {
							assert.LessOrEqual(t, ansi.StringWidth(line), width, "row %d is wider than a %dx%d pane: %q", i, width, height, line)
						}

						harness.press("?")
					}
				}
			}
		})
	}
}

// TestScreensSurviveAListing covers a listing landing on each screen the listing feeds.
func TestScreensSurviveAListing(t *testing.T) {
	tests := buildListScreens()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{width: 200})
			harness.press(tt.press...)

			harness.watch(buildCrowdedSnapshot(""))

			assert.Equal(t, tt.screen, harness.readScreen(), "the screen stays where it was")
		})
	}
}

const crowdedCount = 40

type listScreen struct {
	name   string
	press  []string
	screen string
}

func openStaged(t *testing.T, press ...string) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, panelHarnessParams{width: 200})
	answerWorktreeReads(harness)
	harness.watch(buildSnapshot(newNotedWorkflow(), newRunning()))
	harness.press(press...)
	harness.fireTimers()

	return harness
}

func openDragged(t *testing.T) *panelHarness {
	t.Helper()

	harness := openStaged(t, "g", "g", "j")
	harness.panel.SetPaneWidth("%0", 50)
	harness.panel.SetPaneWidth("%9", 149)
	harness.send(tea.WindowSizeMsg{Width: 50, Height: defaultHarnessHeight})

	return harness
}

func openCrowded(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.watch(buildCrowdedSnapshot(""))

	return harness
}

func openLongNamed(t *testing.T, params panelHarnessParams) *panelHarness {
	t.Helper()

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.watch(buildCrowdedSnapshot(strings.Repeat("x", 120)))

	return harness
}

func buildCrowdedSnapshot(long string) *watchv1.WatchEvent {
	workflow := "everything"
	if long != "" {
		workflow = long
	}

	rows := []proto.Message{newLiveWorkflow("w1", workflow)}
	for i := range crowdedCount {
		name := fmt.Sprintf("agent%02d", i)
		if long != "" && i == 0 {
			name = long
		}

		agent := newAgent("a"+name, name, "w1")
		agent.CreatedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", crowdedCount-i)
		archived := newArchivedAgent("r"+name, name, "w1")
		archived.ArchivedAt = xptr.Ptr(fmt.Sprintf("2026-07-30T09:%02d:00Z", crowdedCount-i))
		schedule := newNamedSchedule("s"+name, name)
		schedule.Cron = ""
		worktree := &worktreev1.WatchedWorktree{
			Dir:      "/trees/" + name,
			RepoDir:  "/home/h/komrad",
			Branch:   "task/" + name,
			Readable: true,
		}

		rows = append(rows, agent, archived, schedule, worktree)
	}

	return buildSnapshot(rows...)
}

func newNotedWorkflow() *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:      "w1",
		Name:    "backend",
		Context: "always run the linter",
	}
}

func resize(harness *panelHarness, width, height int) {
	harness.panel.SetPaneSize(width, height)
	harness.send(tea.WindowSizeMsg{Width: width, Height: height})
}

func measureListRule(frame string) int {
	for _, line := range splitLines(frame) {
		if strings.HasPrefix(line, "─") {
			return ruleWidth(line)
		}
	}

	return 0
}

func findRowWith(t *testing.T, frame, want string) string {
	t.Helper()

	for _, line := range readSidebar(frame) {
		if strings.Contains(line, want) {
			return line
		}
	}

	require.Failf(t, "no row carries the text", "%q", want)

	return ""
}

func buildListScreens() []listScreen {
	return []listScreen{
		{
			name:   "the agents tree",
			screen: agentsScreen,
		},
		{
			name:   "the archived agents",
			press:  []string{"F", "tab", "esc"},
			screen: agentsScreen,
		},
		{
			name:   "the schedules",
			press:  []string{"tab"},
			screen: schedulesScreen,
		},
		{
			name:   "the worktrees",
			press:  []string{"shift+tab"},
			screen: worktreesScreen,
		},
	}
}
