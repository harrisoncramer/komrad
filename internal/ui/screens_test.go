package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryScreenAnswersForItself walks the enum rather than a list of its own, so a screen
// added with an arm that answers for nothing fails here the way one added with no arm at all
// fails the exhaustive check.
func TestEveryScreenAnswersForItself(t *testing.T) {
	for s := screenAgents; s <= screenWorktrees; s++ {
		spec := s.spec()

		require.NotNil(t, spec.key, "screen %d takes no keys", s)
		require.NotNil(t, spec.view, "screen %d draws nothing", s)

		if spec.column != nil {
			column := spec.column()
			assert.Positive(t, column.floor, "screen %d draws a block at no width", s)
		}

		if spec.details != nil {
			assert.NotNil(t, spec.details.hovered, "screen %d draws details of nothing", s)
			assert.NotNil(t, spec.details.quit, "screen %d draws details with no way out", s)
		}

		if spec.filter != nil {
			assert.NotNil(t, spec.filter.hovered, "screen %d filters with no cursor to keep", s)
			assert.NotNil(t, spec.filter.place, "screen %d filters without placing its cursor again", s)
		}

		if spec.tabs != nil {
			assert.NotEmpty(t, spec.tabs.strip.options, "screen %d draws a strip with no tabs", s)
			assert.NotNil(t, spec.tabs.state, "screen %d draws a strip with nowhere to keep its choice", s)
			assert.NotNil(t, spec.tabs.fits, "screen %d draws a strip with no room rule", s)
		}
	}
}

// TestEveryListingScreenSurvivesARefresh drives the pass applyRefresh runs once a second against
// each screen the listing feeds.
func TestEveryListingScreenSurvivesARefresh(t *testing.T) {
	for s := screenAgents; s <= screenWorktrees; s++ {
		if !s.spec().fromListing {
			continue
		}

		tmux := &recordingTmux{width: 200, height: 40}
		m := infoModel(tmux)
		m.width, m.height = 200, 40
		m.frame.Attach("%0")
		m.enterScreen(s)

		m.applyRefresh(refreshResult{})

		assert.Equal(t, s, m.screen, "screen %d left the screen it was on", s)
		assert.NotEmpty(t, m.View(), "screen %d draws nothing after a pass", s)
	}
}

// TestFormIsDrawnOverTheScreenItWasOpenedFrom covers the form being chrome rather than a screen
// of its own.
func TestFormIsDrawnOverTheScreenItWasOpenedFrom(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())

	opened, _ := m.archiveScheduleConfirm()
	prompt, ok := opened.(model)
	require.True(t, ok)

	require.Equal(t, formScheduleArchive, prompt.form.kind)
	assert.Equal(t, screenSchedules, prompt.screen, "the schedules are still the screen underneath")

	closed, _ := prompt.closeForm()
	got, ok := closed.(model)
	require.True(t, ok)

	assert.False(t, got.form.isActive(), "closing takes the prompt down")
	assert.Equal(t, screenSchedules, got.screen, "and lands back where it was opened from")
}

// paneFloor is the narrowest pane this sweep asserts anything about, being a clipped name plus
// the marks drawn around it.
const paneFloor = 38

// TestNoScreenOutgrowsThePaneItWasGiven sweeps every screen at panes tall enough to hold their
// own chrome.
func TestNoScreenOutgrowsThePaneItWasGiven(t *testing.T) {
	widths := []int{paneFloor, 39, 40, 41, 42, 44, 46, 50, 60, 80, 120, 200}

	for s := screenAgents; s <= screenWorktrees; s++ {
		for _, width := range widths {
			for _, height := range []int{28, 60} {
				name := fmt.Sprintf("%d at %dx%d", s, width, height)
				t.Run(name, func(t *testing.T) {
					m := longNamedModel(t)
					m.screen = s
					m.width, m.height = width, height

					for _, expanded := range []bool{false, true} {
						shown := m
						shown.helpExpanded = expanded

						drawn := drawnLines(shown.View())
						assert.LessOrEqual(t, len(drawn), height, "taller than the pane")

						for i, row := range drawn {
							assert.LessOrEqual(t, lipgloss.Width(row), width,
								"row %d is wider than the pane: %q", i, row)
						}
					}
				})
			}
		}
	}
}

// TestALongNameIsCutWithAnEllipsis covers the names drawn beside a mark, where a cut made further
// down takes the mark with it and says nothing.
func TestALongNameIsCutWithAnEllipsis(t *testing.T) {
	long := strings.Repeat("x", 120)

	t.Run("a workflow in the tree", func(t *testing.T) {
		m := crowdedModel(t)
		m.width, m.height = 80, 40
		m.listing.workflows[0].Name = long
		m.tree.showHidden = false
		m.listing.agents[0].agent.Hidden = true

		header := rowWith(t, m.agentsListView(), long[:10])
		assert.Contains(t, header, "…", "the name says it was cut")
		assert.Contains(t, header, "+1", "the tally beside it is still drawn")
	})

	t.Run("a workflow in the archive", func(t *testing.T) {
		m := crowdedModel(t)
		m.width, m.height = 80, 40
		m.listing.archivedWorkflows = []panel.Workflow{{ID: "w1", Name: long}}

		assert.Contains(t, rowWith(t, m.archiveListView(), long[:10]), "…", "the name says it was cut")
	})

	t.Run("a schedule", func(t *testing.T) {
		m := crowdedModel(t)
		m.width, m.height = 80, 40
		m.listing.schedules[0].schedule.Name = long
		m.listing.schedules[0].schedule.Cron = "0 9 * * 1"

		row := rowWith(t, m.schedulesListView(), long[:10])
		assert.Contains(t, row, "…", "the name says it was cut")
		assert.Contains(t, row, "0 9 * * 1", "the cron beside it is still drawn")
	})
}

// rowWith is the drawn row carrying a piece of text, failing the test when no row does.
func rowWith(t *testing.T, view, want string) string {
	t.Helper()

	for _, row := range drawnLines(view) {
		if strings.Contains(row, want) {
			return row
		}
	}

	t.Fatalf("no row carries %q", want)

	return ""
}

// longNamedModel is the crowded panel with a name on every listing longer than any pane.
func longNamedModel(t *testing.T) model {
	t.Helper()

	long := strings.Repeat("x", 120)

	m := crowdedModel(t)
	m.listing.workflows[0].Name = long
	m.listing.archivedWorkflows = m.listing.workflows
	m.listing.schedules[0].schedule.Name = long
	m.listing.agents = append([]agentItem{{
		agent: panel.Agent{
			ID:         "zz",
			Name:       long,
			WorkflowID: "w1",
		},
	}}, m.listing.agents...)

	return m
}

// crowdedModel is a panel with more on every screen than any pane can hold, which is the shape
// that finds a view drawing past what it was given.
func crowdedModel(t *testing.T) model {
	t.Helper()

	m := model{
		keys:   config.DefaultKeybindings(),
		frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
		width:  80,
		height: 24,
		listing: listing{
			workflows: []panel.Workflow{{ID: "w1", Name: "a workflow with a long enough name"}},
		},
	}

	for i := range 40 {
		name := fmt.Sprintf("an-agent-with-a-long-name-%d", i)
		m.listing.agents = append(m.listing.agents, agentItem{
			agent: panel.Agent{
				ID:         fmt.Sprintf("a%d", i),
				Name:       name,
				WorkflowID: "w1",
			},
		})
		m.listing.archivedAgents = append(m.listing.archivedAgents, panel.Agent{
			ID:         fmt.Sprintf("r%d", i),
			Name:       name,
			WorkflowID: "w1",
		})
		m.listing.schedules = append(m.listing.schedules, scheduleItem{
			schedule: panel.Schedule{ID: fmt.Sprintf("s%d", i), Name: name},
		})
		m.search.turns = append(m.search.turns, searchHit{
			agentID: fmt.Sprintf("a%d", i),
			name:    name,
			snippet: strings.Repeat("a snippet long enough to want clipping ", 2),
		})
	}

	return m
}
