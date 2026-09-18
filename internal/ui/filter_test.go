package ui

import (
	"strings"
	"testing"

	"weave/config"
	"weave/internal/ui/paint"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFilterQuery covers what a typed pattern matches, including a half-typed one that will
// not compile and so draws every row rather than looking like an empty list.
func TestFilterQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		name2      string
		wantMatch  bool
		wantBroken bool
	}{
		{
			name:      "an empty query keeps every row",
			query:     "",
			name2:     "anything",
			wantMatch: true,
		},
		{
			name:      "a name the pattern does not reach is held back",
			query:     "^api",
			name2:     "web-api",
			wantMatch: false,
		},
		{
			name:      "a pattern in lower case matches either case",
			query:     "api",
			name2:     "API-gateway",
			wantMatch: true,
		},
		{
			name:      "a pattern carrying an upper case letter matches exactly",
			query:     "API",
			name2:     "api-gateway",
			wantMatch: false,
		},
		{
			name:      "a pattern is a regular expression rather than a substring",
			query:     "^(api|web)-",
			name2:     "web-ui",
			wantMatch: true,
		},
		{
			name:       "a pattern that will not compile draws every row",
			query:      "api[",
			name2:      "unrelated",
			wantMatch:  true,
			wantBroken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter filterState
			filter.setQuery(tt.query)

			assert.Equal(t, tt.wantMatch, filter.doesMatch(tt.name2))
			assert.Equal(t, tt.wantBroken, filter.broken)
		})
	}
}

// TestMatchAt covers where a search lands: the nearest match from where it opened, and the walk
// through the rest of them in either direction, wrapping at both ends.
func TestMatchAt(t *testing.T) {
	tests := []struct {
		name  string
		query string
		from  string
		delta int
		want  string
	}{
		{
			name:  "the row the cursor is already on counts as the nearest match",
			query: "auth",
			from:  agentRowKey("a1"),
			want:  agentRowKey("a1"),
		},
		{
			name:  "a search from a row that does not match walks forward to one that does",
			query: "auth",
			from:  workflowRowKey("w1"),
			want:  agentRowKey("a1"),
		},
		{
			name:  "a header matches by its own name",
			query: "^frontend$",
			from:  agentRowKey("a1"),
			want:  workflowRowKey("w2"),
		},
		{
			name:  "a step forward leaves the row it is on rather than finding it again",
			query: "a",
			from:  agentRowKey("a1"),
			delta: 1,
			want:  agentRowKey("a2"),
		},
		{
			name:  "a step back goes the other way",
			query: "a",
			from:  agentRowKey("a2"),
			delta: -1,
			want:  agentRowKey("a1"),
		},
		{
			name:  "a step past the last match wraps to the first",
			query: "^api$",
			from:  agentRowKey("a2"),
			delta: 1,
			want:  agentRowKey("a2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := selectionTree()
			m.filter.setQuery(tt.query)

			got, ok := m.matchAt(tt.from, tt.delta)
			require.True(t, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestMatchAtWrapsBackwardsPastTheTop pins a step back from the first row reaching the last
// match rather than running off the front of the list.
func TestMatchAtWrapsBackwardsPastTheTop(t *testing.T) {
	m := selectionTree()
	m.filter.setQuery("^(backend|ui)$")

	got, ok := m.matchAt(workflowRowKey("w1"), -1)
	require.True(t, ok)
	assert.Equal(t, agentRowKey("a4"), got, "the walk back from the first row lands on the last match")
}

// TestMatchAtFindsNothing pins a pattern nothing matches leaving the cursor alone rather than
// moving it somewhere arbitrary.
func TestMatchAtFindsNothing(t *testing.T) {
	m := selectionTree()
	m.filter.setQuery("zzz")

	_, ok := m.matchAt(agentRowKey("a1"), 0)
	assert.False(t, ok)
}

// TestSearchDoesNotNarrow pins the whole tree staying on screen under a search.
func TestSearchDoesNotNarrow(t *testing.T) {
	m := selectionTree()
	before := m.agentRows()

	m.filter.setQuery("auth")

	assert.Len(t, m.agentRows(), len(before))
}

// TestSearchEnterKeepsTheRow pins enter putting the input away and leaving the cursor on what
// the search found, which is the point of typing it.
func TestSearchEnterKeepsTheRow(t *testing.T) {
	m := selectionTree()
	m.tree.cursor = rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))

	opened, _ := m.updateAgents(keyMsg("/"))
	typed := pressMark(t, opened.(model), "d", "b")
	landed := pressMark(t, typed, "enter")

	assert.False(t, landed.filter.typing, "the input is put away")
	assert.True(t, landed.filter.isStanding(), "and what it found still stands")
	assert.Equal(t, agentRowKey("a3"), rowKeyAt(landed.agentRows(), landed.tree.cursor))
}

// TestSearchEscapeGoesBack pins escape giving up on the search and putting the cursor back
// where it opened.
func TestSearchEscapeGoesBack(t *testing.T) {
	m := selectionTree()
	start := rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))
	m.tree.cursor = start

	opened, _ := m.updateAgents(keyMsg("/"))
	typed := pressMark(t, opened.(model), "d", "b")
	require.Equal(t, agentRowKey("a3"), rowKeyAt(typed.agentRows(), typed.tree.cursor), "typing walks onto the match")

	back := pressMark(t, typed, "esc")
	assert.False(t, back.filter.isStanding(), "the search is gone")
	assert.Equal(t, start, back.tree.cursor, "and the cursor is back where it started")
}

// TestSearchEscapeGoesBackFromAStandingSearch pins the escape that clears a search which is no
// longer being typed at, which is a different path from the one that abandons the input.
func TestSearchEscapeGoesBackFromAStandingSearch(t *testing.T) {
	m := selectionTree()
	start := rowIndexOfKey(m.agentRows(), workflowRowKey("w1"))
	m.tree.cursor = start

	opened, _ := m.updateAgents(keyMsg("/"))
	standing := pressMark(t, opened.(model), "d", "b", "enter")
	require.False(t, standing.filter.typing)
	require.Equal(t, agentRowKey("a3"), rowKeyAt(standing.agentRows(), standing.tree.cursor))

	back := pressMark(t, standing, "esc")
	assert.False(t, back.filter.isStanding(), "the search is gone")
	assert.Equal(t, start, back.tree.cursor, "and the cursor is back where it started")
}

// TestArchiveSearchWalksAndClears pins the archive getting the same search as the agents list.
func TestArchiveSearchWalksAndClears(t *testing.T) {
	m := archiveModel(&fakeArchiveAgents{}, &fakeArchiveWorkflows{}, config.DefaultKeybindings())
	m.archive.agents = []agentstore.Agent{
		{
			ID:         "a1",
			Name:       "api-refactor",
			WorkflowID: "w1",
		},
		{
			ID:         "a2",
			Name:       "auth-fix",
			WorkflowID: "w1",
		},
	}
	m.archive.cursor = 0
	require.Equal(t, workflowRowKey("w1"), rowKeyAt(m.archiveRows(), m.archive.cursor))

	opened, _ := m.updateArchive(keyMsg("/"))
	standing := pressMark(t, opened.(model), "^", "a", "enter")
	require.True(t, standing.filter.isStanding())
	require.Equal(t, agentRowKey("a1"), rowKeyAt(standing.archiveRows(), standing.archive.cursor))

	walked := pressMark(t, standing, "n")
	assert.Equal(t, agentRowKey("a2"), rowKeyAt(walked.archiveRows(), walked.archive.cursor), "n walks to the next match")

	back := pressMark(t, walked, "N")
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(back.archiveRows(), back.archive.cursor), "and N walks back")

	cleared := pressMark(t, back, "esc")
	assert.False(t, cleared.filter.isStanding(), "escape clears the search")
	assert.Equal(t, workflowRowKey("w1"), rowKeyAt(cleared.archiveRows(), cleared.archive.cursor), "and puts the cursor back where it opened")
}

// TestSearchWalksMatchesOnlyWhileOneStands pins n and N moving between matches once a search
// stands, and doing nothing of the sort before one does.
func TestSearchWalksMatchesOnlyWhileOneStands(t *testing.T) {
	m := selectionTree()
	m.tree.cursor = rowIndexOfKey(m.agentRows(), agentRowKey("a1"))

	quiet := pressMark(t, m, "n")
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(quiet.agentRows(), quiet.tree.cursor), "n is not a jump with no search up")

	opened, _ := m.updateAgents(keyMsg("/"))
	standing := pressMark(t, opened.(model), "a", "enter")

	forward := pressMark(t, standing, "n")
	assert.Equal(t, agentRowKey("a2"), rowKeyAt(forward.agentRows(), forward.tree.cursor))

	back := pressMark(t, forward, "N")
	assert.Equal(t, agentRowKey("a1"), rowKeyAt(back.agentRows(), back.tree.cursor))
}

// TestFilterInput covers the input over the list, where the pattern outlives the input itself so
// the keys that walk the matches work against what it found.
func TestFilterInput(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantTyping bool
		wantQuery  string
	}{
		{
			name:       "a key that is not the search leaves the input closed",
			keys:       []string{"i"},
			wantTyping: false,
			wantQuery:  "",
		},
		{
			name:       "typing builds the pattern as it goes",
			keys:       []string{"/", "p", "a"},
			wantTyping: true,
			wantQuery:  "pa",
		},
		{
			name:       "a key bound elsewhere types rather than acting",
			keys:       []string{"/", "i"},
			wantTyping: true,
			wantQuery:  "i",
		},
		{
			name:       "backspace takes the last rune back",
			keys:       []string{"/", "p", "a", "backspace"},
			wantTyping: true,
			wantQuery:  "p",
		},
		{
			name:       "enter closes the input and leaves the search standing",
			keys:       []string{"/", "p", "a", "enter"},
			wantTyping: false,
			wantQuery:  "pa",
		},
		{
			name:       "esc drops the pattern along with the input",
			keys:       []string{"/", "p", "a", "esc"},
			wantTyping: false,
			wantQuery:  "",
		},
		{
			name:       "esc over a search that stands clears it rather than reopening it",
			keys:       []string{"/", "p", "a", "enter", "esc"},
			wantTyping: false,
			wantQuery:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			whole := rowNames(m.agentRows())

			for _, k := range tt.keys {
				updated, _ := m.route(keyMsg(k))
				next, ok := updated.(model)
				require.True(t, ok)
				m = next
			}

			assert.Equal(t, tt.wantTyping, m.filter.typing)
			assert.Equal(t, tt.wantQuery, m.filter.query)
			assert.Equal(t, whole, rowNames(m.agentRows()), "a search never takes a row off the screen")
		})
	}
}

// TestFilterCursor covers where the cursor lands as the search changes.
func TestFilterCursor(t *testing.T) {
	tests := []struct {
		name  string
		start int
		query string
		want  string
	}{
		{
			name:  "a cursor whose row does not match moves to the first match",
			start: 1,
			query: "payouts",
			want:  "payouts",
		},
		{
			name:  "a cursor stays on its row when it matches",
			start: 2,
			query: "pa",
			want:  "payouts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.tree.cursor = tt.start

			got := typeIntoFilter(m, tt.query)

			rows := got.agentRows()
			require.Less(t, got.tree.cursor, len(rows))
			assert.Equal(t, tt.want, rows[got.tree.cursor].item.agent.Name)
		})
	}
}

// TestFilterKeepsSelection covers a selection surviving a search.
func TestFilterKeepsSelection(t *testing.T) {
	m := filterModel()
	m.tree.selected = selection{ids: []string{"a1", "a2"}}

	whole := rowNames(m.agentRows())
	got := typeIntoFilter(m, "payouts")

	assert.Equal(t, whole, rowNames(got.agentRows()), "the tree is left whole")
	assert.Equal(t, []string{"a1", "a2"}, agentIDs(chosenAgents(got.tree.selected, got.agentRows())), "both chosen agents are still acted on")
}

// TestFilterDroppedLeavingScreen covers a pattern following the user off the list it was typed
// on, which it must not: the schedules screen would open already narrowed by a name meant for
// the agents tree.
func TestFilterDroppedLeavingScreen(t *testing.T) {
	m := filterModel()
	m.filter.setQuery("payouts")

	updated, _ := m.updateAgents(keyMsg("S"))
	got, ok := updated.(model)
	require.True(t, ok)

	require.Equal(t, screenSchedules, got.screen)
	assert.Empty(t, got.filter.query)
}

// TestFilterHighlight covers a row drawn in pieces so the matched run can take its own colour,
// and checks the pieces still add up to the name.
func TestFilterHighlight(t *testing.T) {
	tests := []struct {
		name  string
		query string
		row   string
	}{
		{
			name:  "a name with no match is drawn whole",
			query: "zzz",
			row:   "billing",
		},
		{
			name:  "a match at the start is drawn whole",
			query: "bill",
			row:   "billing",
		},
		{
			name:  "a match inside is drawn whole",
			query: "ill",
			row:   "billing",
		},
		{
			name:  "a match running to the end is drawn whole",
			query: "ing$",
			row:   "billing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter filterState
			filter.setQuery(tt.query)

			got := filter.highlight(paint.Panel, tt.row, lipgloss.NewStyle())

			assert.Equal(t, tt.row, ansi.Strip(got))
		})
	}
}

// TestFilterViewLine covers the input drawn over the list, which stands whether or not a search
// does.
func TestFilterViewLine(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		typing bool
		want   string
	}{
		{
			name: "a screen with no filter still draws the line",
			want: "/",
		},
		{
			name:   "an open input draws what has been typed",
			query:  "pay",
			typing: true,
			want:   "/ pay▏",
		},
		{
			name:  "a closed input keeps the pattern on screen",
			query: "pay",
			want:  "/ pay",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.filter.setQuery(tt.query)
			m.filter.typing = tt.typing

			assert.Equal(t, tt.want, strings.TrimSpace(ansi.Strip(m.filterView())))
		})
	}
}

// TestFilterSlashMarksAStandingSearch covers the colour of the slash, which is all a line that is
// always drawn has to say whether anything is being searched for.
func TestFilterSlashMarksAStandingSearch(t *testing.T) {
	restore := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(restore) })

	m := filterModel()

	assert.Contains(t, m.filterView(), dimStyle.Render("/"), "an idle line is dim")
	assert.NotContains(t, m.filterView(), paint.Panel.Cursor().Render("/"))

	typing := m
	typing.filter.typing = true

	assert.Contains(t, typing.filterView(), paint.Panel.Cursor().Render("/"), "an open input takes the accent")

	standing := m
	standing.filter.setQuery("pay")

	assert.Contains(t, standing.filterView(), paint.Panel.Cursor().Render("/"), "and so does a pattern left standing")
}

// typeIntoFilter types a whole pattern at the filter, the way a person does one key at a time.
func typeIntoFilter(m model, query string) model {
	m.filter.typing = true
	for _, r := range query {
		next, _ := m.updateFilterInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(model)
	}

	return m
}

func filterModel() model {
	tmux := &recordingTmux{}
	workflow := workflowstore.Workflow{ID: "w1", Name: "core"}

	return model{
		tmux:   tmux,
		popup:  NewPopupWindow(tmux, "test-session"),
		frame:  layout.New(tmux, config.DefaultLayout()),
		keys:   config.DefaultKeybindings(),
		screen: screenAgents,
		listing: listing{workflows: []workflowstore.Workflow{workflow}, agents: []agentItem{
			{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "billing",
				WorkflowID: "w1",
			}},
			{agent: agentstore.Agent{
				ID:         "a2",
				Name:       "payouts",
				WorkflowID: "w1",
			}},
		}}, tree: treeState{rowCursor: rowCursor{cursor: 1}},
	}
}

func rowNames(rows []agentRow) []string {
	var names []string
	for _, r := range rows {
		if r.header {
			names = append(names, r.workflow.Name)
			continue
		}

		names = append(names, r.item.agent.Name)
	}

	return names
}
