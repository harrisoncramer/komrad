package ui_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/engine"
	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"
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

		frame := harness.Frame()
		assert.True(t, harness.Tmux().IsZoomed())
		assert.Contains(t, frame, "always run the linter")
		assert.Contains(t, paneltest.ListRows(frame), "backend")
		for _, line := range paneltest.SplitLines(harness.Rendered()) {
			assert.Equal(t, 200, ansi.StringWidth(line), "every row of the frame is squared off")
		}
	})

	t.Run("a running agent's own pane gets the window back", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		assert.False(t, harness.Tmux().IsZoomed())
		assert.Equal(t, "%9", harness.Tmux().GuestPane())
		assert.Equal(t, 197, measureListRule(harness.Frame()), "and the list goes back to the whole of the pane")
	})

	t.Run("a dormant agent says why it has no pane", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{Width: 200})
		answerWorktreeReads(harness)
		harness.Watch(paneltest.Snapshot(newLiveWorkflow("w1", "backend"), newDormant("auth")))

		harness.Press("j")
		harness.Advance(time.Second)

		assert.True(t, harness.Tmux().IsZoomed())
		assert.Contains(t, harness.Frame(), "dormant")
	})

	t.Run("leaving a running agent for a row with no pane takes the window", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		harness.Press("k")
		harness.Advance(time.Second)

		assert.True(t, harness.Tmux().IsZoomed(), "tmux is holding the zoom rather than the frame only believing so")
	})

	t.Run("a pane moved by hand gives up both halves until the keys come back", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")

		harness.Tmux().SetKeysElsewhere(true)
		harness.Send(tea.BlurMsg{})

		frame := harness.Frame()
		assert.NotContains(t, frame, "always run the linter", "the frame is told what tmux did rather than assuming")
		assert.Equal(t, 199, measureListRule(frame), "so the list is rendered in the whole of the pane")

		harness.Watch(paneltest.Snapshot(newNotedWorkflow(), newRunning()))

		assert.False(t, harness.Tmux().IsZoomed(), "a listing does not zoom the panel over the pane the keys are on")

		harness.Tmux().SetKeysElsewhere(false)
		harness.Send(tea.FocusMsg{})

		assert.Contains(t, harness.Frame(), "always run the linter", "and takes the window back once the keys return")
	})

	t.Run("an unfocused terminal keeps the window", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g")
		before := harness.Frame()

		harness.Send(tea.BlurMsg{})

		assert.Equal(t, before, harness.Frame(), "nothing is laid out again behind an unfocused terminal")
	})

	t.Run("a running agent's window is kept at the size of the stage", func(t *testing.T) {
		t.Parallel()

		harness := openStaged(t, "g", "g", "j")

		harness.Send(tea.WindowSizeMsg{Width: 200, Height: paneltest.DefaultHeight})

		sizes := harness.Tmux().WindowSizes()
		assert.Contains(t, sizes, "%9")
		assert.NotContains(t, sizes, "", "a dormant agent has no window to size")
	})

	t.Run("a remote engine's agent windows are left alone", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{Width: 200, Engine: engine.Remote{URL: "https://komrad.example.com"}})
		answerWorktreeReads(harness)
		harness.Watch(paneltest.Snapshot(newNotedWorkflow(), newRunning()))
		harness.Press("j")
		harness.Advance(time.Second)

		harness.Send(tea.WindowSizeMsg{Width: 200, Height: paneltest.DefaultHeight})

		assert.Empty(t, harness.Tmux().WindowSizes(), "no window on the panel's own server is touched")
	})

	t.Run("a remote engine's agent is attached over the engine's api", func(t *testing.T) {
		t.Parallel()

		harness := paneltest.Start(t, paneltest.Params{
			Width:           200,
			Engine:          engine.Remote{URL: "https://komrad.example.com"},
			TerminalCommand: []string{"komrad", "terminal"},
		})
		answerWorktreeReads(harness)
		harness.Watch(paneltest.Snapshot(newNotedWorkflow(), newRunning()))
		harness.Press("j")
		harness.Advance(time.Second)

		assert.Contains(t, harness.Tmux().Respawns(), "komrad terminal -- a1")
	})
}

// TestScreensDivider covers a divider the reader dragged staying where they put it.
func TestScreensDivider(t *testing.T) {
	t.Parallel()

	t.Run("a widened window puts the dragged width back", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)

		harness.Tmux().SetWindowWidth(300)
		harness.Tmux().SetPaneWidth("%0", 75)
		harness.Tmux().SetPaneWidth("%9", 224)
		harness.Send(tea.WindowSizeMsg{Width: 75, Height: paneltest.DefaultHeight})

		assert.Contains(t, harness.Tmux().Resizes(), "%0 50")
	})

	t.Run("the dragged width survives the sidebar taking the window", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)
		harness.Press("k")
		harness.Advance(time.Second)

		harness.Tmux().SetPaneWidth("%0", 200)
		harness.Send(tea.WindowSizeMsg{Width: 200, Height: paneltest.DefaultHeight})

		assert.Equal(t, 47, measureListRule(harness.Frame()))
	})

	t.Run("the dragged width survives tmux dropping the zoom", func(t *testing.T) {
		t.Parallel()

		harness := openDragged(t)
		harness.Press("k")
		harness.Advance(time.Second)
		harness.Tmux().SetPaneWidth("%0", 200)
		harness.Send(tea.WindowSizeMsg{Width: 200, Height: paneltest.DefaultHeight})

		harness.Tmux().SetPaneWidth("%0", 50)
		harness.Send(tea.WindowSizeMsg{Width: 50, Height: paneltest.DefaultHeight})
		harness.Tmux().SetPaneWidth("%0", 200)
		harness.Send(tea.WindowSizeMsg{Width: 200, Height: paneltest.DefaultHeight})

		assert.Equal(t, 47, measureListRule(harness.Frame()))
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
			harness := paneltest.Start(t, paneltest.Params{
				Width:        200,
				SidebarWidth: tt.columns,
			})
			harness.Watch(paneltest.Snapshot(newNotedWorkflow()))

			assert.Equal(t, tt.want, measureListRule(harness.Frame()))
		})
	}

	t.Run("a divider dragged past the document's floor is rendered where it is", func(t *testing.T) {
		harness := openStaged(t, "g", "g", "j")
		harness.Tmux().SetPaneWidth("%0", 174)
		harness.Tmux().SetPaneWidth("%9", 25)
		harness.Send(tea.WindowSizeMsg{Width: 174, Height: paneltest.DefaultHeight})

		harness.Press("k")
		harness.Advance(time.Second)
		harness.Tmux().SetPaneWidth("%0", 200)
		harness.Send(tea.WindowSizeMsg{Width: 200, Height: paneltest.DefaultHeight})

		frame := harness.Frame()
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
				harness := openCrowded(t, paneltest.Params{Width: tt.width, Height: tall})
				harness.Refuse(agentv1connect.AgentServiceResumeAgentProcedure, connect.NewError(connect.CodeNotFound, errors.New("no such agent")))
				harness.Press(slices.Concat(screen.press, []string{"G"}, tt.press)...)

				for height := fitsFrom; height <= tall; height++ {
					resize(harness, tt.width, height)
					frame := harness.Frame()

					assert.Len(t, paneltest.SplitLines(frame), height, "%s in a pane %d lines tall", screen.name, height)
					if height > fitsFrom {
						assert.NotEmpty(t, paneltest.HoveredRow(frame), "%s keeps the hovered row on screen in a pane %d lines tall", screen.name, height)
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
					harness := openLongNamed(t, paneltest.Params{Width: width, Height: height})
					harness.Press(tt.press...)

					for range 2 {
						frame := harness.Frame()
						assert.LessOrEqual(t, len(paneltest.SplitLines(frame)), height, "taller than a %dx%d pane", width, height)
						for i, line := range paneltest.SplitLines(frame) {
							assert.LessOrEqual(t, ansi.StringWidth(line), width, "row %d is wider than a %dx%d pane: %q", i, width, height, line)
						}

						harness.Press("?")
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
			harness := openCrowded(t, paneltest.Params{Width: 200})
			harness.Press(tt.press...)

			harness.Watch(buildCrowdedSnapshot(""))

			assert.Equal(t, tt.screen, paneltest.Screen(harness.Frame()), "the screen stays where it was")
		})
	}
}

const crowdedCount = 40

type listScreen struct {
	name   string
	press  []string
	screen screen.ID
}

func openStaged(t *testing.T, press ...string) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, paneltest.Params{Width: 200})
	answerWorktreeReads(harness)
	harness.Watch(paneltest.Snapshot(newNotedWorkflow(), newRunning()))
	harness.Press(press...)
	harness.Advance(time.Second)

	return harness
}

func openDragged(t *testing.T) *paneltest.Panel {
	t.Helper()

	harness := openStaged(t, "g", "g", "j")
	harness.Tmux().SetPaneWidth("%0", 50)
	harness.Tmux().SetPaneWidth("%9", 149)
	harness.Send(tea.WindowSizeMsg{Width: 50, Height: paneltest.DefaultHeight})

	return harness
}

func openCrowded(t *testing.T, params paneltest.Params) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	answerWorktreeReads(harness)
	harness.Watch(buildCrowdedSnapshot(""))

	return harness
}

func openLongNamed(t *testing.T, params paneltest.Params) *paneltest.Panel {
	t.Helper()

	harness := paneltest.Start(t, params)
	answerWorktreeReads(harness)
	harness.Watch(buildCrowdedSnapshot(strings.Repeat("x", 120)))

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

	return paneltest.Snapshot(rows...)
}

func newNotedWorkflow() *workflowv1.Workflow {
	return &workflowv1.Workflow{
		Id:      "w1",
		Name:    "backend",
		Context: "always run the linter",
	}
}

func resize(harness *paneltest.Panel, width, height int) {
	harness.Tmux().SetPaneSize(width, height)
	harness.Send(tea.WindowSizeMsg{Width: width, Height: height})
}

func measureListRule(frame string) int {
	for _, line := range paneltest.SplitLines(frame) {
		if strings.HasPrefix(line, "─") {
			return paneltest.RuleWidth(line)
		}
	}

	return 0
}

func findRowWith(t *testing.T, frame, want string) string {
	t.Helper()

	for _, line := range paneltest.Sidebar(frame) {
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
			screen: screen.Agents,
		},
		{
			name:   "the archived agents",
			press:  []string{"F", "tab", "esc"},
			screen: screen.Agents,
		},
		{
			name:   "the schedules",
			press:  []string{"tab"},
			screen: screen.Schedules,
		},
		{
			name:   "the worktrees",
			press:  []string{"shift+tab"},
			screen: screen.Worktrees,
		},
	}
}
