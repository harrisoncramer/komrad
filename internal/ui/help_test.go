package ui_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"komrad/config"
	"komrad/proto/gen/agent/v1/agentv1connect"
	workflowv1 "komrad/proto/gen/workflow/v1"
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
		harness := openHelp(t, panelHarnessParams{sidebarWidth: 33}, append(slices.Repeat([]string{"{"}, 4), "?")...)

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
		if screen.screen == searchScreen {
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
			press: []string{"ctrl+q"},
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

type undrawn struct {
	key    string
	reason string
}

type driftScreen struct {
	name    string
	screen  string
	open    func(t *testing.T) *panelHarness
	excused []undrawn
}

// TestHelpKeyDrift covers every key a screen answers being named in that screen's legend.
func TestHelpKeyDrift(t *testing.T) {
	pressable := append(readConfiguredKeys(t, config.DefaultKeybindings()), "ctrl+a", "ctrl+q")

	for _, screen := range buildDriftScreens() {
		t.Run("the "+screen.name+" legend names every key it answers", func(t *testing.T) {
			harness := openDriftScreen(t, screen)
			drawn := harness.panel.ReadLegendKeys()

			for _, pressed := range pressable {
				isExcused := slices.ContainsFunc(screen.excused, func(excused undrawn) bool {
					return excused.key == pressed
				})
				if isExcused {
					continue
				}
				if !harness.panel.IsKeyTaken(pressed) {
					continue
				}

				assert.Contains(t, drawn, pressed, "the %s screen answers %q and its legend does not name it", screen.name, pressed)
			}
		})

		t.Run("every key the "+screen.name+" legend is excused from is still answered", func(t *testing.T) {
			harness := openDriftScreen(t, screen)

			for _, excused := range screen.excused {
				assert.True(t, harness.panel.IsKeyTaken(excused.key),
					"the %s screen no longer answers %q, so its excuse can go: %s", screen.name, excused.key, excused.reason)
			}
		})
	}
}

func buildDriftScreens() []driftScreen {
	quit := undrawn{
		key:    "ctrl+c",
		reason: "quit is not worth a row on any screen",
	}
	nearest := undrawn{
		key:    "V",
		reason: "select to nearest is named once the selection it needs is standing",
	}
	wheelReason := "the mouse wheel presses it, and paging is the only scroll the diff names"

	return []driftScreen{
		{
			name:   "agents",
			screen: agentsScreen,
			open:   openDriftAgents,
			excused: []undrawn{
				quit,
				{
					key:    "esc",
					reason: "on the root list this is the quit key by another name",
				},
				nearest,
			},
		},
		{
			name:   "schedules",
			screen: schedulesScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness := openDriftAgents(t)
				harness.press("tab", "s")

				return harness
			},
			excused: []undrawn{quit},
		},
		{
			name:   "archive",
			screen: archiveScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness := openArchive(t, buildOneArchivedAgent()...)
				harness.press("j")

				return harness
			},
			excused: []undrawn{
				quit,
				nearest,
			},
		},
		{
			name:   "search",
			screen: searchScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness := openSearch(t, openSearchParams{
					server: newSearchServer(newTurnHit("a1", "auth", "ledger")),
					query:  "ledger",
					rows:   buildDriftAgent(),
				})
				harness.press("enter")

				return harness
			},
			excused: []undrawn{quit},
		},
		{
			name:   "review",
			screen: "the review",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openReview(t, newReviewServer(t))
			},
			excused: []undrawn{
				quit,
				{
					key:    "ctrl+n",
					reason: wheelReason,
				},
				{
					key:    "ctrl+p",
					reason: wheelReason,
				},
			},
		},
		{
			name:   "worktrees",
			screen: worktreesScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openWorktrees(t)
			},
			excused: []undrawn{
				quit,
				nearest,
			},
		},
		{
			name:   "documents",
			screen: documentsScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness, _ := openDocuments(t)

				return harness
			},
			excused: []undrawn{quit},
		},
	}
}

func openDriftScreen(t *testing.T, screen driftScreen) *panelHarness {
	t.Helper()

	harness := screen.open(t)
	require.Equal(t, screen.screen, harness.readScreen(), "the %s screen is up", screen.name)

	return harness
}

func openDriftAgents(t *testing.T) *panelHarness {
	t.Helper()

	return openAgents(t, buildDriftAgent()...)
}

func buildDriftAgent() []proto.Message {
	agent := newAgent("a1", "auth", "w1")
	agent.PaneId = "%9"
	agent.Cwd = "/repo"
	agent.Alive = proto.Bool(true)
	agent.Documents = 1

	return []proto.Message{
		&workflowv1.Workflow{
			Id:       "w1",
			Name:     "backend",
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		},
		agent,
	}
}

func readConfiguredKeys(t *testing.T, keys config.Keybindings) []string {
	t.Helper()

	var configured []string
	groups := reflect.ValueOf(keys)
	for i := range groups.NumField() {
		group := groups.Field(i)
		for j := range group.NumField() {
			field, isKeyList := group.Field(j).Interface().([]string)
			require.True(t, isKeyList, "%s.%s is not a key list", groups.Type().Field(i).Name, group.Type().Field(j).Name)

			for _, key := range field {
				if !slices.Contains(configured, key) {
					configured = append(configured, key)
				}
			}
		}
	}

	return configured
}
