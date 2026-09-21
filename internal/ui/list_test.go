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
		listing:       listing{workflows: []workflowstore.Workflow{{ID: "w1", Name: "everything"}}},
		search:        searchState{query: "agent39", turns: hits("agent39")},
	}

	for i := range total {
		name := fmt.Sprintf("agent%d", i)
		m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{
			ID:         name,
			Name:       name,
			WorkflowID: "w1",
		}})
		m.listing.archivedAgents = append(m.listing.archivedAgents, agentstore.Agent{
			ID:         name,
			Name:       name,
			WorkflowID: "w1",
		})
		m.listing.schedules = append(m.listing.schedules, scheduleItem{schedule: schedulestore.Schedule{Name: name}})
	}

	m.tree.cursor = total
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
			wantTitle: "Agents & Workflows",
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

			first, _, _ := strings.Cut(shown.View(), "\n")
			require.True(t, strings.HasPrefix(first, " "+view.Spell(tt.icon)+"  "+tt.title), "the title opens the frame: %q", ansi.Strip(first))
			assert.NotContains(t, first, paint.Panel.Title(tt.title), "and is drawn plain")
		})
	}
}

// TestSearchPromptSitsUnderTheTitle covers the prompt set in the same column as the title
// above it, whichever glyph opens it.
func TestSearchPromptSitsUnderTheTitle(t *testing.T) {
	tests := []struct {
		name   string
		screen screen
		mode   filterMode
		want   string
	}{
		{
			name:   "a search on the agents tree",
			screen: screenAgents,
			mode:   filterModeSearch,
			want:   " / db",
		},
		{
			name:   "a filter on the agents tree",
			screen: screenAgents,
			mode:   filterModeFilter,
			want:   " ▽ db",
		},
		{
			name:   "a search on the worktrees",
			screen: screenWorktrees,
			mode:   filterModeSearch,
			want:   " / db",
		},
		{
			name:   "the transcript search",
			screen: screenSearch,
			want:   " / agent39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown := screensModel()
			shown.height = 29
			shown.screen = tt.screen
			shown.filter.mode = tt.mode
			shown.filter.setQuery("db")

			rows := lines(ansi.Strip(shown.View()))
			require.Greater(t, len(rows), 2)
			assert.True(t, strings.HasPrefix(rows[2], tt.want), "the prompt is the third row: %q", rows[2])
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
// bottom of the pane, from the height each screen first fits at upwards, and that the hovered row
// is on screen from the height above that, whatever else is standing in the pane with the list.
func TestScreensFitATightPane(t *testing.T) {
	const tall = 40

	tests := []struct {
		name    string
		width   int
		prepare func(*model)
	}{
		{
			name:    "plain",
			width:   80,
			prepare: func(*model) {},
		},
		{
			name:  "with the help expanded",
			width: 80,
			prepare: func(m *model) {
				m.helpExpanded = true
			},
		},
		{
			name:  "with a message in the stack",
			width: 80,
			prepare: func(m *model) {
				m.pushMessage(message{kind: messageError, summary: "boom", detail: "it broke"})
			},
		},
		{
			name:  "with the region holding a message",
			width: 80,
			prepare: func(m *model) {
				m.pushMessage(message{kind: messageError, summary: "boom", detail: "it broke"})
				m.messages.focused = true
			},
		},
		{
			name:  "with the record focused under the expanded legend",
			width: 30,
			prepare: func(m *model) {
				m.helpExpanded = true
				m.details.focused = true
			},
		},
		{
			name:  "with the record focused under the expanded legend at the width its own legend folds",
			width: 35,
			prepare: func(m *model) {
				m.helpExpanded = true
				m.details.focused = true
			},
		},
		{
			name:  "with a search being typed over the focused record",
			width: 30,
			prepare: func(m *model) {
				m.helpExpanded = true
				m.details.focused = true
				m.details.filter.typing = true
				m.details.filter.setQuery("a")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for s := screenAgents; s <= screenWorktrees; s++ {
				if s == screenReview {
					continue
				}

				fixture, fitsFrom, hovered := screensModel, 5, "agent39"

				for height := fitsFrom; height <= tall; height++ {
					shown := fixture()
					shown.width, shown.height = tt.width, height
					shown.screen = s
					tt.prepare(&shown)

					assert.Len(t, lines(shown.View()), height, "screen %d in a pane %d lines tall", s, height)

					if height > fitsFrom {
						assert.Contains(t, outsideTheRecord(shown), hovered, "screen %d keeps the hovered row on screen in a pane %d lines tall", s, height)
					}
				}
			}
		})
	}
}

// TestTheRecordGivesWayToTheList pins the record standing down on every list screen at the
// height it would leave the list no row, and standing again one row taller.
func TestTheRecordGivesWayToTheList(t *testing.T) {
	tests := []struct {
		name    string
		screen  screen
		model   func() model
		fills   int
		hovered string
	}{
		{
			name:    "the agents tree",
			screen:  screenAgents,
			model:   screensModel,
			fills:   12,
			hovered: "agent39",
		},
		{
			name:    "the archive",
			screen:  screenArchive,
			model:   screensModel,
			fills:   12,
			hovered: "agent39",
		},
		{
			name:    "the schedules",
			screen:  screenSchedules,
			model:   screensModel,
			fills:   10,
			hovered: "agent39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shown := tt.model()
			shown.screen = tt.screen

			shown.height = tt.fills
			assert.Empty(t, shown.detailsView(), "the record gives up its rows")
			assert.Contains(t, shown.View(), tt.hovered, "to the row the cursor stands on")

			shown.height = tt.fills + 1
			assert.NotEmpty(t, shown.detailsView(), "the record is drawn once the list keeps a row beside it")
			assert.Contains(t, outsideTheRecord(shown), tt.hovered, "and the row the cursor stands on is still on screen")
		})
	}
}

func outsideTheRecord(m model) string {
	record := m.detailsView()
	if record == "" {
		return m.View()
	}

	return strings.Replace(m.View(), record, "", 1)
}

func lines(rendered string) []string {
	trimmed := strings.TrimSuffix(rendered, "\n")
	if trimmed == "" {
		return nil
	}

	return strings.Split(trimmed, "\n")
}
