package ui_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"komrad/config"
	"komrad/lib/xptr"
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
		assert.NotContains(t, legend, "rename", "and drops what the row also does")

		harness.press("?")

		expanded := harness.readFrame()
		legend = readLegend(expanded)
		assert.Contains(t, legend, "rename", "the expanded legend names every binding")
		assert.Contains(t, legend, "hide help")
		assert.Less(t, findRule(expanded), findRule(collapsed), "the rows the legend takes are the list's")

		harness.press("?")
		assert.Contains(t, readLegend(harness.readFrame()), "show help", "the key toggles rather than only expanding")
	})
}

// TestHelpKeyDrift covers that the screen's legend names every key the screen answers.
func TestHelpKeyDrift(t *testing.T) {
	pressable := readConfiguredKeys(t, config.DefaultKeybindings())

	for _, screen := range buildDriftScreens() {
		t.Run("the "+screen.name+" legend names every key it answers", func(t *testing.T) {
			harness := openDriftScreen(t, screen)
			rendered := harness.panel.ReadLegendKeys()

			for _, pressed := range pressable {
				isExcused := slices.ContainsFunc(screen.excused, func(excused excusedKey) bool {
					return excused.key == pressed
				})
				if isExcused {
					continue
				}
				if !harness.panel.IsKeyTaken(pressed) {
					continue
				}

				assert.Contains(t, rendered, pressed, "the %s screen answers %q and its legend does not name it", screen.name, pressed)
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

type excusedKey struct {
	key    string
	reason string
}

type driftScreen struct {
	name    string
	screen  string
	open    func(t *testing.T) *panelHarness
	excused []excusedKey
}

func buildDriftScreens() []driftScreen {
	quit := excusedKey{
		key:    "ctrl+c",
		reason: "quit is not worth a row on any screen",
	}
	nearest := excusedKey{
		key:    "V",
		reason: "select to nearest is named once the selection it needs is standing",
	}
	wheelReason := "the mouse wheel presses it, and paging is the only scroll the diff names"

	return []driftScreen{
		{
			name:   "agents",
			screen: agentsScreen,
			open:   openDriftAgents,
			excused: []excusedKey{
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
				harness.press("tab")

				return harness
			},
			excused: []excusedKey{quit},
		},
		{
			name:   "archived schedules",
			screen: schedulesScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openArchivedSchedules(t, newLiveWorkflow("w1", "backend"), newArchivedSchedule("s1", "monthly-gone"))
			},
			excused: []excusedKey{quit},
		},
		{
			name:   "archived agents",
			screen: agentsScreen,
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness := openArchivedAgents(t, buildOneArchivedAgent()...)
				harness.press("j")

				return harness
			},
			excused: []excusedKey{
				quit,
				nearest,
			},
		},
		{
			name:   "review",
			screen: "the review",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openReview(t, newReviewServer(t))
			},
			excused: []excusedKey{
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
			excused: []excusedKey{
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
			excused: []excusedKey{quit},
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
	agent.Alive = xptr.Ptr(true)
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
