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

// TestScreensFitThePane pins the header, the list, and the legend fitting the pane together.
func TestScreensFitThePane(t *testing.T) {
	const (
		// Taller than the tallest legend on purpose.
		height = 29
		total  = 40
	)

	m := model{
		keys:    config.DefaultKeybindings(),
		height:  height,
		frame:   layout.New(&recordingTmux{}, config.DefaultLayout()),
		listing: listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}},
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

// TestScreensFitAPaneTheChromeFills pins that a list handed no room is dropped rather than drawn
// past the bottom of the pane, at the height where the chrome around it fills the pane and at
// every height above it.
func TestScreensFitAPaneTheChromeFills(t *testing.T) {
	const (
		total = 40
		tall  = 40
	)

	base := model{
		keys:          config.DefaultKeybindings(),
		width:         80,
		detailsHeight: config.DefaultDetailsHeight,
		frame:         layout.New(&recordingTmux{}, config.DefaultLayout()),
		listing:       listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}},
	}

	for i := range total {
		name := fmt.Sprintf("agent%d", i)
		base.listing.agents = append(base.listing.agents, agentItem{agent: agentstore.Agent{Name: name, WorkflowID: "w1"}})
		base.archive.agents = append(base.archive.agents, agentstore.Agent{Name: name, WorkflowID: "w1"})
		base.schedules.items = append(base.schedules.items, scheduleItem{schedule: schedulestore.Schedule{Name: name}})
	}

	base.tree.cursor = total
	base.archive.cursor = total
	base.schedules.cursor = total - 1

	tests := []struct {
		name     string
		screen   screen
		expanded bool
		filled   int
	}{
		{name: "the agents tree", screen: screenAgents, filled: 9},
		{name: "the agents tree with the help expanded", screen: screenAgents, expanded: true, filled: 27},
		{name: "the archive", screen: screenArchive, filled: 9},
		{name: "the archive with the help expanded", screen: screenArchive, expanded: true, filled: 15},
		{name: "the schedules", screen: screenSchedules, filled: 9},
		{name: "the schedules with the help expanded", screen: screenSchedules, expanded: true, filled: 17},
		{name: "the mailbox", screen: screenMail, filled: 6},
		{name: "the mailbox with the help expanded", screen: screenMail, expanded: true, filled: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for height := tt.filled; height <= tall; height++ {
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
