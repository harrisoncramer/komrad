package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNarrowed covers which rows a filter leaves on screen: the ones the pattern matches, the
// rows they sit under, and the rows under a match.
func TestNarrowed(t *testing.T) {
	tests := []struct {
		name  string
		mode  filterMode
		query string
		want  []string
	}{
		{
			name:  "a search takes nothing off the screen",
			mode:  filterModeSearch,
			query: "auth",
			want:  []string{"backend", "auth", "api", "db", "frontend", "ui"},
		},
		{
			name:  "a pattern that will not compile takes nothing off the screen",
			mode:  filterModeFilter,
			query: "auth[",
			want:  []string{"backend", "auth", "api", "db", "frontend", "ui"},
		},
		{
			name:  "a pattern nothing matches empties the list",
			mode:  filterModeFilter,
			query: "zzz",
			want:  []string{},
		},
		{
			name:  "a matching agent keeps its workflow's header and nothing else",
			mode:  filterModeFilter,
			query: "^db$",
			want:  []string{"backend", "db"},
		},
		{
			name:  "a matching child keeps its parent and the header above that",
			mode:  filterModeFilter,
			query: "^ui$",
			want:  []string{"frontend", "ui"},
		},
		{
			name:  "a matching header keeps the whole group under it",
			mode:  filterModeFilter,
			query: "^backend$",
			want:  []string{"backend", "auth", "api", "db"},
		},
		{
			name:  "a matching parent keeps its children",
			mode:  filterModeFilter,
			query: "^auth$",
			want:  []string{"backend", "auth", "api"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter filterState
			filter.mode = tt.mode
			filter.setQuery(tt.query)

			got := narrowedAgentRows(filter, treeRows(treeRowsParams{
				workflows: filterModeWorkflows(),
				agents:    filterModeAgents(),
			}))

			assert.Equal(t, tt.want, append([]string{}, rowNames(got)...))
		})
	}
}

// TestFilterTabFlipsTheMode covers tab while typing switching the prompt between a search and a
// filter, with the tree left whole and the cursor still until enter applies whichever stands, a
// filter leaving the cursor on a row it kept and a search walking it onto the match.
func TestFilterTabFlipsTheMode(t *testing.T) {
	m := filterModeTree()
	start := rowIndexOfKey(m.agents.rows(m.shared()), workflowRowKey("w1"))
	m.agents.cursor = start
	whole := rowNames(m.agents.rows(m.shared()))

	opened, _ := m.routeScreenMessage(keyMsg("/"))
	typed := pressMark(t, opened.(model), "d", "b")
	require.Equal(t, whole, rowNames(typed.agents.rows(typed.shared())), "a search leaves the tree whole")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(ansi.Strip(typed.filter.line(paint.Panel))), "/ db"), "the prompt opens with a slash")

	flipped := pressMark(t, typed, "tab")
	assert.Equal(t, filterModeFilter, flipped.filter.mode)
	assert.Equal(t, whole, rowNames(flipped.agents.rows(flipped.shared())), "the tab narrows nothing yet")
	assert.Equal(t, start, flipped.agents.cursor, "and moves nothing")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(ansi.Strip(flipped.filter.line(paint.Panel))), "▽ db"), "the prompt opens with the funnel")

	narrowed := pressMark(t, flipped, "enter")
	assert.Equal(t, []string{"backend", "db"}, rowNames(narrowed.agents.rows(narrowed.shared())), "enter narrows to the match and its header")
	assert.Equal(t, workflowRowKey("w1"), rowKeyAt(narrowed.agents.rows(narrowed.shared()), narrowed.agents.cursor), "and leaves the cursor on the header the filter kept")

	lit := pressMark(t, typed, "tab", "tab", "enter")
	assert.Equal(t, filterModeSearch, lit.filter.mode)
	assert.Equal(t, whole, rowNames(lit.agents.rows(lit.shared())), "a second tab puts the search back")
	assert.Equal(t, agentRowKey("a3"), rowKeyAt(lit.agents.rows(lit.shared()), lit.agents.cursor), "which enter walks onto the match")
}

// TestFilterAppliedPlacesTheCursor covers the cursor being put on a row the narrowed list still
// holds when applying the filter takes the row it was on off the screen.
func TestFilterAppliedPlacesTheCursor(t *testing.T) {
	m := filterModeTree()
	m.agents.cursor = rowIndexOfKey(m.agents.rows(m.shared()), agentRowKey("a4"))

	opened, _ := m.routeScreenMessage(keyMsg("/"))
	typed := pressMark(t, opened.(model), "^", "a", "u", "tab")
	require.Equal(t, agentRowKey("a4"), rowKeyAt(typed.agents.rows(typed.shared()), typed.agents.cursor), "nothing moves while typing")

	narrowed := pressMark(t, typed, "enter")
	rows := narrowed.agents.rows(narrowed.shared())
	require.Less(t, narrowed.agents.cursor, len(rows))
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(rows, narrowed.agents.cursor))
}

// TestFilterEnterKeepsNarrowing covers enter putting the input away with the list still
// narrowed, and the legend naming the mode that stands.
func TestFilterEnterKeepsNarrowing(t *testing.T) {
	m := filterModeTree()

	opened, _ := m.routeScreenMessage(keyMsg("/"))
	standing := pressMark(t, opened.(model), "tab", "d", "b", "enter")

	assert.False(t, standing.filter.typing)
	assert.True(t, standing.filter.isNarrowing(), "the filter stands after enter")
	assert.Equal(t, []string{"backend", "db"}, rowNames(standing.agents.rows(standing.shared())))
	assert.Equal(t, "filter: db", standing.filterDesc())

	reopened, _ := standing.routeScreenMessage(keyMsg("/"))
	assert.Equal(t, filterModeSearch, reopened.(model).filter.mode, "opening the input again starts a search")
}

// TestFilterEscapeDropsTheMode covers escape giving the rows back along with the pattern, from
// the input and from a filter left standing.
func TestFilterEscapeDropsTheMode(t *testing.T) {
	tests := []struct {
		name string
		keys []string
	}{
		{
			name: "escape while typing",
			keys: []string{"tab", "d", "b", "esc"},
		},
		{
			name: "escape over a standing filter",
			keys: []string{"tab", "d", "b", "enter", "esc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModeTree()
			start := rowIndexOfKey(m.agents.rows(m.shared()), workflowRowKey("w2"))
			m.agents.cursor = start
			whole := rowNames(m.agents.rows(m.shared()))

			opened, _ := m.routeScreenMessage(keyMsg("/"))
			back := pressMark(t, opened.(model), tt.keys...)

			assert.Equal(t, filterModeSearch, back.filter.mode)
			assert.False(t, back.filter.isStanding())
			assert.Equal(t, whole, rowNames(back.agents.rows(back.shared())), "every row is back")
			assert.Equal(t, start, back.agents.cursor, "and the cursor is where it opened")
		})
	}
}

// TestFilterGlyphTakesTheCursorColour covers the funnel drawn in the cursor's colour like the
// slash, so its shape alone says the list is narrowed.
func TestFilterGlyphTakesTheCursorColour(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	m := filterModeTree()
	m.filter.typing = true
	m.filter.mode = filterModeFilter

	assert.Contains(t, m.filter.line(paint.Panel), paint.Panel.Cursor().Render("▽"))
	assert.NotContains(t, m.filter.line(paint.Panel), paint.Panel.Match().Render("▽"))
}

// TestFilterHelpNamesTheOtherMode covers the legend under the input saying what tab would
// switch to.
func TestFilterHelpNamesTheOtherMode(t *testing.T) {
	assert.Equal(t, "filter instead", filterHelp(filterModeSearch).short[1].Help().Desc)
	assert.Equal(t, "search instead", filterHelp(filterModeFilter).short[1].Help().Desc)
}

// TestFilterNothingMatchesLine covers each screen saying the filter left nothing rather than
// that the list is empty.
func TestFilterNothingMatchesLine(t *testing.T) {
	tests := []struct {
		name string
		view func(model) string
		idle string
	}{
		{
			name: "agents",
			view: func(m model) string { return m.agents.view(m.shared()) },
		},
		{
			name: "archive",
			view: func(m model) string { return m.archive.view(m.shared()) },
			idle: "nothing archived",
		},
		{
			name: "worktrees",
			view: func(m model) string { return m.worktrees.view(m.shared()) },
			idle: "no worktrees",
		},
		{
			name: "schedules",
			view: func(m model) string { return m.schedules.view(m.shared()) },
			idle: "no schedules yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModeScreens()
			m.filter.mode = filterModeFilter
			m.filter.setQuery("zzz")

			view := ansi.Strip(tt.view(m))
			assert.Contains(t, view, "nothing matches")
			if tt.idle != "" {
				assert.NotContains(t, view, tt.idle)
			}
		})
	}
}

// TestJumpMarkThroughAFilter covers a jump to a row the filter took off the screen putting the
// pattern back to a search rather than dropping it.
func TestJumpMarkThroughAFilter(t *testing.T) {
	m := markTree()
	markRow(&m, "a4")
	m.filter.mode = filterModeFilter
	m.filter.setQuery("auth")
	require.Equal(t, -1, rowIndexOfKey(m.agents.rows(m.shared()), agentRowKey("a4")), "the filter hides the marked row")

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")

	assert.Equal(t, filterModeSearch, jumped.filter.mode, "the filter is put back to a search")
	assert.Equal(t, "auth", jumped.filter.query, "and the pattern still stands")
	assert.False(t, jumped.agents.showHidden, "the hidden rows stay hidden")
	assert.Equal(t, agentRowKey("a4"), rowKeyAt(jumped.agents.rows(jumped.shared()), jumped.agents.cursor))
}

// TestJumpMarkThroughAFilterToAHiddenRow covers a row both hidden and filtered out needing both
// reveals, and one only hidden leaving the filter alone.
func TestJumpMarkThroughAFilterToAHiddenRow(t *testing.T) {
	tests := []struct {
		name           string
		query          string
		wantMode       filterMode
		wantShowHidden bool
	}{
		{
			name:           "a hidden row the filter keeps is revealed with the filter standing",
			query:          "sweep",
			wantMode:       filterModeFilter,
			wantShowHidden: true,
		},
		{
			name:           "a hidden row the filter drops needs both reveals",
			query:          "auth",
			wantMode:       filterModeSearch,
			wantShowHidden: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := markTree()
			markRow(&m, "a5")
			m.filter.mode = filterModeFilter
			m.filter.setQuery(tt.query)

			jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")

			assert.Equal(t, tt.wantMode, jumped.filter.mode)
			assert.Equal(t, tt.wantShowHidden, jumped.agents.showHidden)
			assert.Equal(t, agentRowKey("a5"), rowKeyAt(jumped.agents.rows(jumped.shared()), jumped.agents.cursor))
		})
	}
}

// TestSchedulesSearchWalksAndNarrows covers the schedules screen getting the same two modes as
// the trees: a search walks its matches and a filter narrows to them.
func TestSchedulesSearchWalksAndNarrows(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	m.listing.schedules = scheduleItems("nightly", "morning", "weekly")

	opened, _ := m.routeScreenMessage(keyMsg("/"))
	standing := pressMark(t, opened.(model), "ly", "enter")
	require.Len(t, standing.schedules.listRows(standing.shared()), 3, "a search leaves the list whole")
	assert.Equal(t, "nightly", standing.schedules.list().hovered(standing.shared()))

	walked := pressMark(t, standing, "n")
	assert.Equal(t, "weekly", walked.schedules.list().hovered(walked.shared()), "n walks to the next match")

	back := pressMark(t, walked, "N")
	assert.Equal(t, "nightly", back.schedules.list().hovered(back.shared()), "and N walks back")

	reopened, _ := back.routeScreenMessage(keyMsg("/"))
	narrowed := pressMark(t, reopened.(model), "tab", "ly", "enter")
	assert.Equal(t, []string{"nightly", "weekly"}, scheduleNames(narrowed.schedules.listRows(narrowed.shared())))
}

// TestOpenWorktreesThroughAFilter covers the key from a narrowed agents list landing on the hovered
// agent's directory.
func TestOpenWorktreesThroughAFilter(t *testing.T) {
	m := worktreesModel()
	m.screen = screenAgents
	m.filter.mode = filterModeFilter
	m.filter.setQuery("zzz")

	m.enter(screenWorktrees, screenTarget{agentID: "archived"})

	assert.Equal(t, screenWorktrees, m.screen)
	assert.False(t, m.filter.isStanding(), "the filter stays on the screen it was typed on")
	assert.Equal(t, "tree:/trees/archived", m.worktrees.list().hovered(m.shared()))
}

// TestWorktreeHeaderFiguresStayWhole covers a repository's count and size reading the whole
// listing while a filter narrows the rows under it.
func TestWorktreeHeaderFiguresStayWhole(t *testing.T) {
	m := worktreesModel()
	m.worktrees.items[0].size = &worktreeSize{bytes: 1 << 20}
	m.worktrees.items[1].size = &worktreeSize{bytes: 1 << 20}
	whole := m.worktrees.countUnder("/home/h/weave")
	require.Greater(t, whole, 1)

	m.filter.mode = filterModeFilter
	m.filter.setQuery("^worktree-view$")
	rows := m.worktrees.rows(m.shared())
	require.Len(t, rows, 2, "the filter leaves the header and one row")

	assert.Equal(t, whole, m.worktrees.countUnder("/home/h/weave"))

	snapshot := m.worktrees.snapshot(m.shared(), listPane{width: 80})
	assert.Equal(t, groupCells(m.worktrees.items, "/home/h/weave"), snapshot.Rows[0].Cells)
	assert.Equal(t, "2.0M", snapshot.Rows[0].Cells[0].Text)
}

// TestReviewSearchIgnoresTab covers the review's own search, which shares the input's keys,
// having no mode to flip.
func TestReviewSearchIgnoresTab(t *testing.T) {
	var filter filterState
	filter.typing = true

	assert.False(t, filter.takeKey(keyMsg("tab")))
	assert.Equal(t, filterModeSearch, filter.mode)
	assert.Equal(t, "/", filter.mode.glyph())
}

func filterModeWorkflows() []panel.Workflow {
	return []panel.Workflow{
		{ID: "w1", Name: "backend"},
		{ID: "w2", Name: "frontend"},
	}
}

// filterModeAgents is a tree with a child under an agent, so the rule for a row under a match
// and a row above one both have something to keep.
func filterModeAgents() []panel.AgentRow {
	return []panel.AgentRow{
		{Agent: panel.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
		}},
		{Agent: panel.Agent{
			ID:            "a2",
			Name:          "api",
			WorkflowID:    "w1",
			ParentAgentID: xptr.Ptr("a1"),
		}},
		{Agent: panel.Agent{
			ID:         "a3",
			Name:       "db",
			WorkflowID: "w1",
		}},
		{Agent: panel.Agent{
			ID:         "a4",
			Name:       "ui",
			WorkflowID: "w2",
		}},
	}
}

func filterModeTree() model {
	tmux := &recordingTmux{}

	return model{
		tmux:  tmux,
		popup: NewPopupWindow(tmux, "test-session"),
		frame: layout.New(tmux, config.DefaultLayout()),
		keys:  config.DefaultKeybindings(),
		listing: listing{
			workflows: filterModeWorkflows(),
			agents:    filterModeAgents(),
		},
	}
}

// filterModeScreens is a model every list screen can draw from, each holding one row a pattern
// can miss.
func filterModeScreens() model {
	m := worktreesModel()
	m.listing.schedules = scheduleItems("nightly")

	return m
}

func scheduleNames(rows []scheduleItem) []string {
	names := xslice.Map(rows, func(row scheduleItem) string {
		return row.schedule.Name
	})

	return names
}

// TestFilterAppliedLandsOnTheNearestMatch covers a filter applied from a row it takes off the
// screen landing on the nearest match forward of that row, as a search does.
func TestFilterAppliedLandsOnTheNearestMatch(t *testing.T) {
	m := filterModeTree()
	m.agents.cursor = rowIndexOfKey(m.agents.rows(m.shared()), agentRowKey("a3"))

	opened, _ := m.routeScreenMessage(keyMsg("/"))
	narrowed := pressMark(t, opened.(model), "u", "tab", "enter")

	assert.Equal(t, []string{"backend", "auth", "api", "frontend", "ui"}, rowNames(narrowed.agents.rows(narrowed.shared())), "the child api stays under its matched parent")
	assert.Equal(t, agentRowKey("a4"), rowKeyAt(narrowed.agents.rows(narrowed.shared()), narrowed.agents.cursor))
}

// TestFilterKeepsTheCursorOnARowItKept covers a filter that keeps the hovered agent under its
// matched header leaving the cursor on it, though the agent itself matches nothing and another
// row does.
func TestFilterKeepsTheCursorOnARowItKept(t *testing.T) {
	m := filterModeTree()
	m.agents.cursor = rowIndexOfKey(m.agents.rows(m.shared()), agentRowKey("a1"))

	opened, _ := m.routeScreenMessage(keyMsg("/"))
	narrowed := pressMark(t, opened.(model), "b", "a", "c", "k", "e", "n", "d", "|", "a", "p", "i", "tab", "enter")

	assert.Equal(t, []string{"backend", "auth", "api", "db"}, rowNames(narrowed.agents.rows(narrowed.shared())))
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(narrowed.agents.rows(narrowed.shared()), narrowed.agents.cursor))
}
