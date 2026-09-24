package ui_test

import (
	"reflect"
	"slices"
	"testing"

	"weave/config"
	workflowv1 "weave/proto/gen/workflow/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type undrawn struct {
	key    string
	reason string
}

type driftScreen struct {
	name    string
	open    func(t *testing.T) *panelHarness
	excused []undrawn
}

// TestKeyDrift covers every key a screen answers being named in that screen's legend.
func TestKeyDrift(t *testing.T) {
	pressable := readConfiguredKeys(t, config.DefaultKeybindings())

	for _, screen := range buildDriftScreens() {
		t.Run("the "+screen.name+" legend names every key it answers", func(t *testing.T) {
			harness := screen.open(t)
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
			harness := screen.open(t)

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
	earlierRun := undrawn{
		key:    "L",
		reason: "the earlier run is named only while the pane is showing one",
	}
	wheel := "the mouse wheel presses it, and paging is the only scroll the diff names"

	return []driftScreen{
		{
			name: "agents",
			open: openDriftAgents,
			excused: []undrawn{
				quit,
				{
					key:    "esc",
					reason: "on the root list this is the quit key by another name",
				},
				nearest,
				earlierRun,
			},
		},
		{
			name: "schedules",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				harness := openDriftAgents(t)
				harness.press("S")

				return harness
			},
			excused: []undrawn{
				quit,
				earlierRun,
			},
		},
		{
			name: "archive",
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
			name: "search",
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
			name: "review",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openReview(t, newReviewServer(t))
			},
			excused: []undrawn{
				quit,
				{
					key:    "ctrl+n",
					reason: wheel,
				},
				{
					key:    "ctrl+p",
					reason: wheel,
				},
			},
		},
		{
			name: "worktrees",
			open: func(t *testing.T) *panelHarness {
				t.Helper()

				return openWorktrees(t)
			},
			excused: []undrawn{
				quit,
				nearest,
			},
		},
	}
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
