package ui_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"weave/internal/engine"
	"weave/proto/gen/agent/v1/agentv1connect"
	watchv1 "weave/proto/gen/watch/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const crowdedCount = 40

var listScreens = []struct {
	name  string
	press []string
	title string
}{
	{
		name:  "the agents tree",
		title: "Agents & Workflows",
	},
	{
		name:  "the archive",
		press: []string{"Z"},
		title: "Archive",
	},
	{
		name:  "the schedules",
		press: []string{"S"},
		title: "Schedules",
	},
	{
		name:  "the search",
		press: []string{"s"},
		title: "Search",
	},
	{
		name:  "the worktrees",
		press: []string{"t"},
		title: "Worktrees",
	},
}

// TestScreensStage covers what the panel shows beside the hovered row and how much of the window it holds.
func TestScreensStage(t *testing.T) {
	t.Parallel()

	t.Run("a row with no pane draws its notes beside the list and takes the window", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")

		frame := harness.readFrame()
		assert.True(t, harness.panel.IsZoomed())
		assert.Contains(t, frame, "always run the linter")
		assert.Contains(t, readListRows(frame), "backend")
		for _, line := range splitLines(frame) {
			assert.Equal(t, 200, ansi.StringWidth(line), "every row of the frame is squared off")
		}
	})

	t.Run("a running agent's own pane gets the window back", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		assert.False(t, harness.panel.IsZoomed())
		assert.Equal(t, "%9", harness.panel.ReadGuestPane())
		assert.Equal(t, 200, measureListRule(harness.readFrame()), "and the list goes back to the whole of the pane")
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

	t.Run("the frame is as tall as the pane", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		assert.Len(t, splitLines(harness.readFrame()), defaultHarnessHeight)
	})

	t.Run("a pane moved by hand gives up both halves until the next listing", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")

		harness.panel.MoveKeysElsewhere(true)
		harness.send(tea.BlurMsg{})

		frame := harness.readFrame()
		assert.NotContains(t, frame, "always run the linter", "the frame is told what tmux did rather than assuming")
		assert.Equal(t, 200, measureListRule(frame), "so the list is drawn in the whole of the pane")

		harness.panel.MoveKeysElsewhere(false)
		harness.watch(buildSnapshot(newNotedWorkflow(), newRunning()))

		assert.Contains(t, harness.readFrame(), "always run the linter", "and takes the window back on the next listing")
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

		harness := newPanelHarness(t, panelHarnessParams{width: 200, engine: engine.Remote{Exec: []string{"ssh", "engine"}}})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newNotedWorkflow(), newRunning()))
		harness.press("j")
		harness.fireTimers()

		harness.send(tea.WindowSizeMsg{Width: 200, Height: defaultHarnessHeight})

		assert.Empty(t, harness.panel.ReadWindowSizes(), "no window on the panel's own server is touched")
	})
}

// TestScreensFit covers every list screen fitting its chrome and its hovered row into the pane.
func TestScreensFit(t *testing.T) {
	for _, tt := range listScreens[:3] {
		t.Run(tt.name+" fits its title, hovered row and whole legend", func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{width: 80, height: 29})

			harness.press(slices.Concat(tt.press, widenListPresses, []string{"G", "?"})...)

			frame := harness.readFrame()
			assert.LessOrEqual(t, len(splitLines(frame)), 29)
			assert.Contains(t, frame, tt.title)
			assert.Contains(t, frame, "hide help")
			assert.Contains(t, readHoveredLine(frame), "agent39", "the hovered row is on screen")
		})
	}

	prompts := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "a search on the agents tree",
			press: []string{"/", "d", "b", "enter"},
			want:  " / db",
		},
		{
			name:  "a filter on the agents tree",
			press: []string{"/", "tab", "d", "b", "enter"},
			want:  " ▽ db",
		},
		{
			name:  "a search on the worktrees",
			press: []string{"t", "/", "d", "b", "enter"},
			want:  " / db",
		},
		{
			name:  "the transcript search",
			press: []string{"s", "d", "b"},
			want:  " / db",
		},
	}

	for _, tt := range prompts {
		t.Run(tt.name+" sets its prompt under the title", func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{width: 80, height: 29})

			harness.press(tt.press...)

			assert.True(t, strings.HasPrefix(splitLines(harness.readFrame())[2], tt.want))
		})
	}

	records := []struct {
		name  string
		press []string
		fills int
	}{
		{
			name:  "the agents tree",
			fills: 12,
		},
		{
			name:  "the archive",
			press: []string{"Z"},
			fills: 12,
		},
		{
			name:  "the schedules",
			press: []string{"S"},
			fills: 10,
		},
	}

	for _, tt := range records {
		t.Run(tt.name+" gives the record's rows up to the list", func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{width: 80, height: 40})
			harness.press(append(tt.press, "G")...)

			resize(harness, 80, tt.fills)
			frame := harness.readFrame()
			assert.False(t, isRecordDrawn(frame), "the record gives up its rows")
			assert.Contains(t, readHoveredLine(frame), "agent39", "to the row the cursor stands on")

			resize(harness, 80, tt.fills+1)
			frame = harness.readFrame()
			assert.True(t, isRecordDrawn(frame), "the record is drawn once the list keeps a row beside it")
			assert.Contains(t, readHoveredLine(frame), "agent39")
		})
	}
}

// TestScreensFitATightPane covers a list screen in a pane only just tall enough for its chrome.
func TestScreensFitATightPane(t *testing.T) {
	t.Parallel()

	const fitsFrom, tall = 5, 40

	states := []struct {
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
			press: []string{"?", "ctrl+t"},
		},
		{
			name:  "with the record focused under the expanded legend at the width its own legend folds",
			width: 35,
			press: []string{"?", "ctrl+t"},
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
			press: []string{"?", "ctrl+t", "/", "a"},
		},
	}

	for _, state := range states {
		for _, screen := range listScreens {
			if screen.title == "Search" && state.press != nil {
				continue
			}

			t.Run(state.name+" on "+screen.name, func(t *testing.T) {
				t.Parallel()

				harness := openCrowded(t, panelHarnessParams{width: state.width, height: tall})
				harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, fmt.Errorf("no such agent")))
				harness.press(append(append(screen.press, "G"), state.press...)...)

				for height := fitsFrom; height <= tall; height++ {
					resize(harness, state.width, height)
					frame := harness.readFrame()

					assert.Len(t, splitLines(frame), height, "a pane %d lines tall", height)
					if height > fitsFrom && screen.title != "Search" {
						assert.NotEmpty(t, readHoveredLine(frame), "the hovered row is on screen in a pane %d lines tall", height)
					}
				}
			})
		}
	}
}

// TestScreensOutgrowNoPane covers every screen drawing inside the pane it was given, whatever the names in it.
func TestScreensOutgrowNoPane(t *testing.T) {
	t.Parallel()

	widths := []int{38, 39, 40, 41, 42, 44, 46, 50, 60, 80, 120, 200}

	for _, screen := range listScreens {
		for _, width := range widths {
			for _, height := range []int{28, 60} {
				t.Run(fmt.Sprintf("%s at %dx%d", screen.name, width, height), func(t *testing.T) {
					t.Parallel()

					harness := openLongNamed(t, panelHarnessParams{width: width, height: height})
					harness.press(screen.press...)

					for range 2 {
						frame := harness.readFrame()
						assert.LessOrEqual(t, len(splitLines(frame)), height, "taller than the pane")
						for i, line := range splitLines(frame) {
							assert.LessOrEqual(t, ansi.StringWidth(line), width, "row %d is wider than the pane: %q", i, line)
						}

						harness.press("?")
					}
				})
			}
		}
	}
}

// TestScreensCutLongNames covers a name too long for its row being cut with an ellipsis that leaves what is beside it.
func TestScreensCutLongNames(t *testing.T) {
	long := strings.Repeat("x", 120)

	t.Run("a workflow in the tree keeps its hidden tally", func(t *testing.T) {
		harness := newPanelHarness(t, panelHarnessParams{width: 80})
		answerWorktreeReads(harness)
		harness.watch(buildSnapshot(newLiveWorkflow("w1", long), newHiddenAgent("a1", "sweep")))

		row := findRowWith(t, harness.readFrame(), long[:10])
		assert.Contains(t, row, "…", "the name says it was cut")
		assert.Contains(t, row, "+1", "the tally beside it is still drawn")
	})

	t.Run("a workflow in the archive", func(t *testing.T) {
		harness := openArchive(t, newArchivedWorkflow("w1", long))

		assert.Contains(t, findRowWith(t, harness.readFrame(), long[:10]), "…")
	})
}

// TestScreensSurviveAListing covers a listing landing on each screen the listing feeds.
func TestScreensSurviveAListing(t *testing.T) {
	for _, screen := range listScreens {
		t.Run(screen.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{width: 200})
			harness.press(screen.press...)

			harness.watch(buildCrowdedSnapshot(""))

			assert.Contains(t, splitLines(harness.readFrame())[0], screen.title, "the screen stays where it was")
		})
	}
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
		if i == crowdedCount-1 {
			name = "agent39"
		}
		if long != "" && i == 0 {
			name = long
		}

		agent := newAgent("a"+name, name, "w1")
		agent.CreatedAt = fmt.Sprintf("2026-09-20T12:%02d:00Z", crowdedCount-i)
		archived := newArchivedAgent("r"+name, name, "w1")
		archived.ArchivedAt = proto.String(fmt.Sprintf("2026-07-30T09:%02d:00Z", crowdedCount-i))
		schedule := newNamedSchedule("s"+name, name)
		schedule.Cron = ""
		worktree := &worktreev1.WatchedWorktree{
			Dir:      "/trees/" + name,
			RepoDir:  "/home/h/weave",
			Branch:   "agent/" + name,
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
			return ansi.StringWidth(line) - ansi.StringWidth(strings.TrimLeft(line, "─"))
		}
	}

	return 0
}

func readHoveredLine(frame string) string {
	for _, line := range readSidebar(frame) {
		if strings.HasPrefix(line, "›") {
			return line
		}
	}

	return ""
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
