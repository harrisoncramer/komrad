package ui

import (
	"fmt"
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	"weave/internal/view"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		listing:       listing{workflows: []panel.Workflow{{ID: "w1", Name: "everything"}}},
	}

	for i := range total {
		name := fmt.Sprintf("agent%d", i)
		m.listing.agents = append(m.listing.agents, panel.AgentRow{Agent: panel.Agent{
			ID:         name,
			Name:       name,
			WorkflowID: "w1",
		}})
		m.listing.archivedAgents = append(m.listing.archivedAgents, panel.AgentRow{Agent: panel.Agent{
			ID:         name,
			Name:       name,
			WorkflowID: "w1",
		}})
		m.listing.schedules = append(m.listing.schedules, scheduleItem{schedule: panel.Schedule{Name: name}})
	}

	m.agents.cursor = total
	m.archive.cursor = total
	m.schedules.cursor = total - 1
	for i := range total {
		name := fmt.Sprintf("agent%02d", i)
		m.worktrees.items = append(m.worktrees.items, worktreeItem{
			dir:      "/trees/" + name,
			repoDir:  "/home/h/weave",
			branch:   "agent/" + name,
			readable: true,
			agent:    &worktreeAgent{id: name, name: name},
		})
	}
	m.worktrees.cursor = total

	return m
}

// TestScreenTitlesOpenWithTheirIcon covers every listing's title drawn one column in behind
// the icon that says what the screen is, in the plain foreground rather than the accent.
func TestScreenTitlesOpenWithTheirIcon(t *testing.T) {
	withANSI(t)

	tests := []struct {
		name   string
		screen screen
		icon   view.Glyph
		title  string
	}{
		{
			name:   "the agents tree",
			screen: screenAgents,
			icon:   view.GlyphWorkflow,
			title:  "Agents & Workflows",
		},
		{
			name:   "the schedules",
			screen: screenSchedules,
			icon:   view.GlyphSchedule,
			title:  "Schedules",
		},
		{
			name:   "the archive",
			screen: screenArchive,
			icon:   view.GlyphArchive,
			title:  "Archive",
		},
		{
			name:   "the worktrees",
			screen: screenWorktrees,
			icon:   view.GlyphRepo,
			title:  "Worktrees",
		},
		{
			name:   "the search",
			screen: screenSearch,
			icon:   view.GlyphSearch,
			title:  "Search",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown := screensModel()
			shown.height = 29
			shown.screen = tt.screen

			first, _, _ := strings.Cut(shown.render(), "\n")
			require.True(t, strings.HasPrefix(first, " "+view.Spell(tt.icon)+"  "+tt.title), "the title opens the frame: %q", ansi.Strip(first))
			assert.NotContains(t, first, paint.Panel.Title(tt.title), "and is drawn plain")
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

func lines(rendered string) []string {
	trimmed := strings.TrimSuffix(rendered, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}
