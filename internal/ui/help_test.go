package ui_test

import (
	"slices"
	"strings"
	"testing"

	"weave/proto/gen/agent/v1/agentv1connect"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestHelp covers the legend under the list and the key that expands it.
func TestHelp(t *testing.T) {
	t.Run("the legend opens on one line and the help key brings the rest out", func(t *testing.T) {
		harness := openHelp(t, panelHarnessParams{}, slices.Repeat([]string{"}"}, 4)...)
		collapsed := harness.readFrame()

		legend := readLegend(collapsed)
		assert.Contains(t, legend, "open", "it keeps what the row does first")
		assert.Contains(t, legend, "show help")
		assert.NotContains(t, legend, "archive", "and drops what the row also does")

		harness.press("?")

		expanded := harness.readFrame()
		legend = readLegend(expanded)
		assert.Contains(t, legend, "archive", "the expanded legend names every binding")
		assert.Contains(t, legend, "hide help")
		assert.Less(t, findRule(expanded), findRule(collapsed), "the rows the legend takes are the list's")

		harness.press("?")
		assert.Contains(t, readLegend(harness.readFrame()), "show help", "the key toggles rather than only expanding")
	})

	t.Run("a sidebar wide enough for two columns draws the same legend in fewer lines", func(t *testing.T) {
		narrow := openHelp(t, panelHarnessParams{}, append(slices.Repeat([]string{"}"}, 4), "?")...)
		wide := openHelp(t, panelHarnessParams{}, append(slices.Repeat([]string{"}"}, 11), "?")...)

		narrowLegend, wideLegend := readLegend(narrow.readFrame()), readLegend(wide.readFrame())
		assert.Less(t, len(splitLines(wideLegend)), len(splitLines(narrowLegend)))
		for _, word := range strings.Fields(narrowLegend) {
			assert.Contains(t, wideLegend, word, "and drops nothing")
		}
	})

	t.Run("a legend too wide for the sidebar keeps its short form and says there is help to show", func(t *testing.T) {
		harness := openHelp(t, panelHarnessParams{}, "?")

		legend := readLegend(harness.readFrame())
		assert.Contains(t, legend, "show help")
		assert.NotContains(t, legend, "hide help", "rather than claiming to have hidden any")
		assert.NotContains(t, legend, "move")
	})

	t.Run("an expanded legend gives way before it takes the list's last row", func(t *testing.T) {
		harness := openHelp(t, panelHarnessParams{}, append(slices.Repeat([]string{"}"}, 4), "?")...)

		for height := 8; height <= defaultHarnessHeight; height++ {
			resize(harness, defaultHarnessWidth, height)

			assert.Equal(t, "  api-refactor", readHoveredRow(harness.readFrame()), "the list keeps its row at height %d", height)
		}

		assert.Contains(t, readLegend(harness.readFrame()), "hide help", "a tall pane draws the legend whole")
	})
}

// TestHelpFitsEveryScreen covers each legend drawn whole at the narrowest sidebar and never wider than the pane.
func TestHelpFitsEveryScreen(t *testing.T) {
	t.Parallel()

	for _, screen := range buildListScreens() {
		if screen.title == "Search" {
			continue
		}

		t.Run(screen.name+" is not drawn as an ellipsis at the narrowest sidebar", func(t *testing.T) {
			t.Parallel()

			harness := openHelp(t, panelHarnessParams{}, append(screen.press, "?")...)

			legend := strings.TrimSpace(readLegend(harness.readFrame()))
			assert.NotEqual(t, "…", legend)
			assert.Contains(t, legend, "help")
		})
	}

	tests := []struct {
		name  string
		press []string
	}{
		{
			name:  "the agents tree on a paused row fits every width",
			press: []string{"j"},
		},
		{
			name:  "the record fits every width",
			press: []string{"ctrl+t"},
		},
		{
			name:  "the message region fits every width",
			press: []string{"enter", "ctrl+g"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, width := range []int{20, 30, 38, 41, 46, 60, 80, 120} {
				for _, expanded := range [][]string{nil, {"?"}} {
					harness := openHelp(t, panelHarnessParams{width: width}, slices.Concat(tt.press, expanded)...)

					for i, row := range splitLines(readLegend(harness.readFrame())) {
						assert.LessOrEqual(t, ansi.StringWidth(row), width, "legend row %d at width %d: %q", i, width, row)
					}
				}
			}
		})
	}
}

func openHelp(t *testing.T, params panelHarnessParams, press ...string) *panelHarness {
	t.Helper()

	dormant := newDormant("api-refactor")
	dormant.Id = "a2"

	harness := newPanelHarness(t, params)
	answerWorktreeReads(harness)
	harness.refuse(agentv1connect.AgentServiceResumeAgentProcedure, newTracedRefusal("no such agent"))
	harness.watch(buildSnapshot(newLiveWorkflow("w1", "backend"), newPausable("1970-01-01T00:00:00Z"), dormant))
	harness.press(press...)

	return harness
}
