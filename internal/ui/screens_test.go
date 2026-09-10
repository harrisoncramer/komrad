package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	schedulestore "weave/internal/store/schedule"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryScreenAnswersForItself walks the enum rather than a list of its own, so a screen
// added with an arm that answers for nothing fails here the way one added with no arm at all
// fails the exhaustive check.
func TestEveryScreenAnswersForItself(t *testing.T) {
	for s := screenAgents; s <= screenReview; s++ {
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
	}
}

// TestEveryListingScreenSurvivesARefresh drives the pass applyRefresh runs once a second against
// each screen the listing feeds.
func TestEveryListingScreenSurvivesARefresh(t *testing.T) {
	for s := screenAgents; s <= screenReview; s++ {
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

// TestNoScreenOutgrowsThePaneItWasGiven sweeps every screen at panes tall enough to hold their
// own chrome.
func TestNoScreenOutgrowsThePaneItWasGiven(t *testing.T) {
	sizes := []struct {
		width  int
		height int
	}{
		{width: 46, height: 28},
		{width: 80, height: 30},
		{width: 120, height: 40},
		{width: 200, height: 60},
	}

	for s := screenAgents; s <= screenMail; s++ {
		for _, size := range sizes {
			name := fmt.Sprintf("%d at %dx%d", s, size.width, size.height)
			t.Run(name, func(t *testing.T) {
				m := crowdedModel(t)
				m.screen = s
				m.width, m.height = size.width, size.height

				for _, expanded := range []bool{false, true} {
					shown := m
					shown.helpExpanded = expanded

					drawn := drawnLines(shown.View())
					assert.LessOrEqual(t, len(drawn), size.height, "taller than the pane")

					for i, row := range drawn {
						assert.LessOrEqual(t, lipgloss.Width(row), size.width,
							"row %d is wider than the pane: %q", i, row)
					}
				}
			})
		}
	}
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
			workflows: []workflowstore.Workflow{{ID: "w1", Name: "a workflow with a long enough name"}},
		},
	}

	for i := range 40 {
		name := fmt.Sprintf("an-agent-with-a-long-name-%d", i)
		m.listing.agents = append(m.listing.agents, agentItem{
			agent: agentstore.Agent{
				ID:         fmt.Sprintf("a%d", i),
				Name:       name,
				WorkflowID: "w1",
			},
		})
		m.archive.agents = append(m.archive.agents, agentstore.Agent{
			ID: fmt.Sprintf("r%d", i), Name: name, WorkflowID: "w1",
		})
		m.schedules.items = append(m.schedules.items, scheduleItem{
			schedule: schedulestore.Schedule{ID: fmt.Sprintf("s%d", i), Name: name},
		})
		m.mail.inbox = append(m.mail.inbox, &agentv1.Mail{
			Id:   fmt.Sprintf("m%d", i),
			Body: strings.Repeat("a message long enough to want wrapping ", 3),
		})
		m.search.turns = append(m.search.turns, searchHit{
			agentID: fmt.Sprintf("a%d", i),
			name:    name,
			snippet: strings.Repeat("a snippet long enough to want clipping ", 2),
		})
	}

	return m
}
