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
	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// screensModel is a panel standing on the last row of a list longer than any pane, on every
// screen that draws one.
func screensModel() model {
	const total = 40

	m := model{
		keys:          config.DefaultKeybindings(),
		width:         80,
		detailsHeight: config.DefaultDetailsHeight,
		frame:         layout.New(&recordingTmux{}, config.DefaultLayout()),
		listing:       listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}},
	}

	for i := range total {
		name := fmt.Sprintf("agent%d", i)
		m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{Name: name, WorkflowID: "w1"}})
		m.archive.agents = append(m.archive.agents, agentstore.Agent{Name: name, WorkflowID: "w1"})
		m.schedules.items = append(m.schedules.items, scheduleItem{schedule: schedulestore.Schedule{Name: name}})
	}

	m.tree.cursor = total
	m.archive.cursor = total
	m.schedules.cursor = total - 1

	return m
}

// TestScreensFitThePane pins the header, the list, and the legend fitting the pane together.
func TestScreensFitThePane(t *testing.T) {
	// Taller than the tallest legend on purpose.
	const height = 29

	m := screensModel()
	m.height = height

	tests := []struct {
		name      string
		screen    screen
		wantTitle string
	}{
		{
			name:      "the agents tree",
			screen:    screenAgents,
			wantTitle: "Agents and Workflows",
		},
		{
			name:      "the archive",
			screen:    screenArchive,
			wantTitle: "Archive",
		},
		{
			name:      "the schedules",
			screen:    screenSchedules,
			wantTitle: "Schedules",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown := m
			shown.screen = tt.screen
			shown.helpExpanded = true
			view := shown.View()

			assert.NotEqual(t, "\n", view[len(view)-1:], "the view ends without a trailing newline")
			assert.LessOrEqual(t, len(lines(view)), height)
			assert.Contains(t, view, "agent39", "the hovered row is on screen")
			assert.Contains(t, view, "hide help", "the legend is on screen")
			assert.Contains(t, view, tt.wantTitle, "the title is on screen")
		})
	}
}

// TestListLines pins that the chrome comes off the pane, and that a pane the chrome fills leaves
// the list nothing rather than a line the pane does not have.
func TestListLines(t *testing.T) {
	const chrome = 6

	header := "Archive\n\n"
	help := "\nrestore\ndelete\nback\n"

	tests := []struct {
		name   string
		height int
		want   int
	}{
		{
			name:   "an unmeasured pane is unbounded",
			height: 0,
			want:   paint.Unbounded,
		},
		{
			name:   "a pane shorter than its chrome keeps no rows",
			height: chrome - 1,
			want:   0,
		},
		{
			name:   "a pane the chrome fills keeps no rows",
			height: chrome,
			want:   0,
		},
		{
			name:   "a pane one line taller than its chrome keeps that line",
			height: chrome + 1,
			want:   1,
		},
		{
			name:   "the pane less the chrome around the list",
			height: 20,
			want:   20 - chrome,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{height: tt.height}

			assert.Equal(t, tt.want, m.listLines(header, help))
		})
	}
}

// TestScreensFitATightPane pins that a list handed no room is dropped rather than drawn past the
// bottom of the pane, from the height each screen first fits at upwards.
func TestScreensFitATightPane(t *testing.T) {
	const tall = 40

	base := screensModel()

	// fitsFrom is where the details record stops spilling rather than where the chrome fills the
	// pane, since the record is drawn from a six-line pane onwards whether the screen has left it
	// room or not, and the heights under it still overdraw by its rows.
	tests := []struct {
		name     string
		screen   screen
		expanded bool
		fitsFrom int
	}{
		{name: "the agents tree", screen: screenAgents, fitsFrom: 9},
		{name: "the agents tree with the help expanded", screen: screenAgents, expanded: true, fitsFrom: 27},
		{name: "the archive", screen: screenArchive, fitsFrom: 9},
		{name: "the archive with the help expanded", screen: screenArchive, expanded: true, fitsFrom: 15},
		{name: "the schedules", screen: screenSchedules, fitsFrom: 9},
		{name: "the schedules with the help expanded", screen: screenSchedules, expanded: true, fitsFrom: 17},
		{name: "the mailbox", screen: screenMail, fitsFrom: 6},
		{name: "the mailbox with the help expanded", screen: screenMail, expanded: true, fitsFrom: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for height := tt.fitsFrom; height <= tall; height++ {
				shown := base
				shown.height = height
				shown.screen = tt.screen
				shown.helpExpanded = tt.expanded

				assert.Len(t, lines(shown.View()), height, "a pane %d lines tall", height)
			}
		})
	}
}

func lines(rendered string) []string {
	trimmed := strings.TrimSuffix(rendered, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}
