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

// TestFilterTabFlipsTheMode covers tab while typing switching between lighting the matches and
// narrowing to them, with the prompt saying which stands.
func TestFilterTabFlipsTheMode(t *testing.T) {
	m := filterModeTree()
	m.tree.cursor = rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))
	whole := rowNames(m.agentRows())

	opened, _ := m.updateAgents(keyMsg("/"))
	typed := pressMark(t, opened.(model), "d", "b")
	require.Equal(t, whole, rowNames(typed.agentRows()), "a search leaves the tree whole")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(ansi.Strip(typed.filter.line(paint.Panel))), "/ db"), "the prompt opens with a slash")

	narrowed := pressMark(t, typed, "tab")
	assert.Equal(t, filterModeFilter, narrowed.filter.mode)
	assert.Equal(t, []string{"backend", "db"}, rowNames(narrowed.agentRows()), "the tab narrows to the match and its header")
	assert.Equal(t, agentRowKey("a3"), rowKeyAt(narrowed.agentRows(), narrowed.tree.cursor), "the cursor stays on the match")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(ansi.Strip(narrowed.filter.line(paint.Panel))), "▽ db"), "and the prompt opens with the funnel")

	lit := pressMark(t, narrowed, "tab")
	assert.Equal(t, filterModeSearch, lit.filter.mode)
	assert.Equal(t, whole, rowNames(lit.agentRows()), "a second tab puts every row back")
	assert.Equal(t, agentRowKey("a3"), rowKeyAt(lit.agentRows(), lit.tree.cursor))
}

// TestFilterTabPlacesTheCursor covers the cursor being put on a row the narrowed list still
// holds when the flip takes the row it was on off the screen.
func TestFilterTabPlacesTheCursor(t *testing.T) {
	m := filterModeTree()
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a4"))

	opened, _ := m.updateAgents(keyMsg("/"))
	typed := pressMark(t, opened.(model), "^", "a", "u")
	require.Equal(t, agentRowKey("a1"), rowKeyAt(typed.agentRows(), typed.tree.cursor))

	narrowed := pressMark(t, typed, "tab")
	rows := narrowed.agentRows()
	require.Less(t, narrowed.tree.cursor, len(rows))
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(rows, narrowed.tree.cursor))
}

// TestFilterEnterKeepsNarrowing covers enter putting the input away with the list still
// narrowed, and the legend naming the mode that stands.
func TestFilterEnterKeepsNarrowing(t *testing.T) {
	m := filterModeTree()

	opened, _ := m.updateAgents(keyMsg("/"))
	standing := pressMark(t, opened.(model), "tab", "d", "b", "enter")

	assert.False(t, standing.filter.typing)
	assert.True(t, standing.filter.isNarrowing(), "the filter stands after enter")
	assert.Equal(t, []string{"backend", "db"}, rowNames(standing.agentRows()))
	assert.Equal(t, "filter: db", standing.filterDesc())

	reopened, _ := standing.updateAgents(keyMsg("/"))
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
			start := rowIndexOfKey(m.agentRows(), workflowRowKey("w2"))
			m.tree.cursor = start
			whole := rowNames(m.agentRows())

			opened, _ := m.updateAgents(keyMsg("/"))
			back := pressMark(t, opened.(model), tt.keys...)

			assert.Equal(t, filterModeSearch, back.filter.mode)
			assert.False(t, back.filter.isStanding())
			assert.Equal(t, whole, rowNames(back.agentRows()), "every row is back")
			assert.Equal(t, start, back.tree.cursor, "and the cursor is where it opened")
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
			view: model.agentsView,
		},
		{
			name: "archive",
			view: model.archiveView,
			idle: "nothing archived",
		},
		{
			name: "worktrees",
			view: model.worktreesView,
			idle: "no worktrees",
		},
		{
			name: "schedules",
			view: model.schedulesView,
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
	markRow(&m, agentRowKey("a4"))
	m.filter.mode = filterModeFilter
	m.filter.setQuery("auth")
	require.Equal(t, -1, rowIndexOfKey(m.agentRows(), agentRowKey("a4")), "the filter hides the marked row")

	jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")

	assert.Equal(t, filterModeSearch, jumped.filter.mode, "the filter is put back to a search")
	assert.Equal(t, "auth", jumped.filter.query, "and the pattern still stands")
	assert.False(t, jumped.tree.showHidden, "the hidden rows stay hidden")
	assert.Equal(t, agentRowKey("a4"), rowKeyAt(jumped.agentRows(), jumped.tree.cursor))
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
			markRow(&m, agentRowKey("a5"))
			m.filter.mode = filterModeFilter
			m.filter.setQuery(tt.query)

			jumped := pressMark(t, m, m.keys.Marks.Jump[0], "c")

			assert.Equal(t, tt.wantMode, jumped.filter.mode)
			assert.Equal(t, tt.wantShowHidden, jumped.tree.showHidden)
			assert.Equal(t, agentRowKey("a5"), rowKeyAt(jumped.agentRows(), jumped.tree.cursor))
		})
	}
}

// TestSchedulesSearchWalksAndNarrows covers the schedules screen getting the same two modes as
// the trees: a search walks its matches and a filter narrows to them.
func TestSchedulesSearchWalksAndNarrows(t *testing.T) {
	m := scheduleModel(&fakeSchedules{}, config.DefaultKeybindings())
	m.listing.schedules = scheduleItems("nightly", "morning", "weekly")

	opened, _ := m.updateSchedules(keyMsg("/"))
	standing := pressMark(t, opened.(model), "ly", "enter")
	require.Len(t, standing.scheduleRows(), 3, "a search leaves the list whole")
	assert.Equal(t, "nightly", standing.hoveredScheduleKey())

	walked := pressMark(t, standing, "n")
	assert.Equal(t, "weekly", walked.hoveredScheduleKey(), "n walks to the next match")

	back := pressMark(t, walked, "N")
	assert.Equal(t, "nightly", back.hoveredScheduleKey(), "and N walks back")

	reopened, _ := back.updateSchedules(keyMsg("/"))
	narrowed := pressMark(t, reopened.(model), "tab", "ly", "enter")
	assert.Equal(t, []string{"nightly", "weekly"}, scheduleNames(narrowed.scheduleRows()))
}

// TestOpenWorktreesThroughAFilter covers the key from a narrowed agents list landing on the hovered
// agent's directory.
func TestOpenWorktreesThroughAFilter(t *testing.T) {
	m := worktreesModel()
	m.screen = screenAgents
	m.filter.mode = filterModeFilter
	m.filter.setQuery("zzz")

	next, _ := m.openWorktrees("archived")
	opened, ok := next.(model)
	require.True(t, ok)

	assert.Equal(t, screenWorktrees, opened.screen)
	assert.False(t, opened.filter.isStanding(), "the filter stays on the screen it was typed on")
	assert.Equal(t, "tree:/trees/archived", worktreesList().hovered(opened))
}

// TestWorktreeHeaderFiguresStayWhole covers a repository's count and size reading the whole
// listing while a filter narrows the rows under it.
func TestWorktreeHeaderFiguresStayWhole(t *testing.T) {
	m := worktreesModel()
	m.worktrees.items[0].size = &worktreeSize{bytes: 1 << 20}
	m.worktrees.items[1].size = &worktreeSize{bytes: 1 << 20}
	whole := m.countUnder("/home/h/weave")
	require.Greater(t, whole, 1)

	m.filter.mode = filterModeFilter
	m.filter.setQuery("^worktree-view$")
	rows := m.worktreeRows()
	require.Len(t, rows, 2, "the filter leaves the header and one row")

	assert.Equal(t, whole, m.countUnder("/home/h/weave"))

	snapshot := m.worktreesSnapshot(80)
	assert.Equal(t, groupTrail(m.worktrees.items, "/home/h/weave"), snapshot.Rows[0].Trail)
	assert.Contains(t, snapshot.Rows[0].Trail, "2.0M")
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
func filterModeAgents() []agentItem {
	return []agentItem{
		{agent: panel.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
		}},
		{agent: panel.Agent{
			ID:            "a2",
			Name:          "api",
			WorkflowID:    "w1",
			ParentAgentID: xptr.Ptr("a1"),
		}},
		{agent: panel.Agent{
			ID:         "a3",
			Name:       "db",
			WorkflowID: "w1",
		}},
		{agent: panel.Agent{
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
