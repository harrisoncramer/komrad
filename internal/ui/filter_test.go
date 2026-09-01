package ui

import (
	"strings"
	"testing"

	"weave/config"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// TestFilterAgentRows covers which rows survive a pattern, keeping folder headers so a matched
// agent is still drawn under the folder it belongs to.
func TestFilterAgentRows(t *testing.T) {
	rows := []agentRow{
		{header: true, workflow: workflowstore.Workflow{ID: "w1", Name: "api"}},
		{workflow: workflowstore.Workflow{ID: "w1", Name: "api"}, item: agentItem{agent: agentstore.Agent{ID: "a1", Name: "billing"}}},
		{workflow: workflowstore.Workflow{ID: "w1", Name: "api"}, item: agentItem{agent: agentstore.Agent{ID: "a2", Name: "payouts"}}},
		{header: true, workflow: workflowstore.Workflow{ID: "w2", Name: "web"}},
		{workflow: workflowstore.Workflow{ID: "w2", Name: "web"}, item: agentItem{agent: agentstore.Agent{ID: "a3", Name: "billing-ui"}}},
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "a pattern nothing matches empties the tree",
			query: "zzz",
			want:  nil,
		},
		{
			name:  "an empty query draws the whole tree",
			query: "",
			want:  []string{"api", "billing", "payouts", "web", "billing-ui"},
		},
		{
			name:  "a folder that only holds a match keeps its header",
			query: "payouts",
			want:  []string{"api", "payouts"},
		},
		{
			name:  "a pattern naming a folder draws that folder whole",
			query: "^web$",
			want:  []string{"web", "billing-ui"},
		},
		{
			name:  "a match in two folders keeps both headers",
			query: "billing",
			want:  []string{"api", "billing", "web", "billing-ui"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter filterState
			filter.setQuery(tt.query)

			var got []string
			for _, r := range filter.filterAgentRows(rows) {
				if r.header {
					got = append(got, r.workflow.Name)
					continue
				}

				got = append(got, r.item.agent.Name)
			}

			assert.Equal(t, tt.want, got)
		})
	}
}

// TestFilterInput covers the input over the list, where the pattern outlives the input itself
// so the keys that act on rows work against the narrowed list.
func TestFilterInput(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantTyping bool
		wantQuery  string
		wantRows   []string
	}{
		{
			name:       "a key that is not the filter leaves the input closed",
			keys:       []string{"i"},
			wantTyping: false,
			wantQuery:  "",
			wantRows:   []string{"core", "billing", "payouts"},
		},
		{
			name:       "typing narrows the tree as it goes",
			keys:       []string{"/", "p", "a"},
			wantTyping: true,
			wantQuery:  "pa",
			wantRows:   []string{"core", "payouts"},
		},
		{
			name:       "a key bound elsewhere types rather than acting",
			keys:       []string{"/", "i"},
			wantTyping: true,
			wantQuery:  "i",
			wantRows:   []string{"core", "billing"},
		},
		{
			name:       "backspace takes the last rune back",
			keys:       []string{"/", "p", "a", "backspace"},
			wantTyping: true,
			wantQuery:  "p",
			wantRows:   []string{"core", "payouts"},
		},
		{
			name:       "enter closes the input and leaves the tree narrowed",
			keys:       []string{"/", "p", "a", "enter"},
			wantTyping: false,
			wantQuery:  "pa",
			wantRows:   []string{"core", "payouts"},
		},
		{
			name:       "esc drops the pattern along with the input",
			keys:       []string{"/", "p", "a", "esc"},
			wantTyping: false,
			wantQuery:  "",
			wantRows:   []string{"core", "billing", "payouts"},
		},
		{
			name:       "back opens the input again over a filter it left behind",
			keys:       []string{"/", "p", "a", "enter", "esc"},
			wantTyping: true,
			wantQuery:  "pa",
			wantRows:   []string{"core", "payouts"},
		},
		{
			name:       "a second back drops the filter along with the input",
			keys:       []string{"/", "p", "a", "enter", "esc", "esc"},
			wantTyping: false,
			wantQuery:  "",
			wantRows:   []string{"core", "billing", "payouts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			for _, k := range tt.keys {
				updated, _ := m.route(keyMsg(k))
				next, ok := updated.(model)
				require.True(t, ok)
				m = next
			}

			assert.Equal(t, tt.wantTyping, m.filter.typing)
			assert.Equal(t, tt.wantQuery, m.filter.query)
			assert.Equal(t, tt.wantRows, rowNames(m.agentRows()))
		})
	}
}

// TestFilterCursor covers where the cursor lands as the tree narrows, since the row under it
// is what the next key acts on.
func TestFilterCursor(t *testing.T) {
	tests := []struct {
		name  string
		start int
		query string
		want  string
	}{
		{
			name:  "a cursor whose row is held back falls into the narrowed list",
			start: 1,
			query: "payouts",
			want:  "payouts",
		},
		{
			name:  "a cursor follows the row it was on",
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

// TestFilterKeepsSelection covers a selection made before the list narrowed, since the filter
// changes what is drawn rather than what exists.
func TestFilterKeepsSelection(t *testing.T) {
	m := filterModel()
	m.tree.selected = selection{ids: []string{"a1", "a2"}}

	got := typeIntoFilter(m, "payouts")

	assert.Equal(t, []string{"payouts"}, rowNames(got.agentRows())[1:], "the tree is narrowed")
	assert.Len(t, chosenAgents(got.tree.selected, got.allAgentRows()), 2, "both chosen agents are still acted on")
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

			got := filter.highlight(tt.row, lipgloss.NewStyle())

			assert.Equal(t, tt.row, ansi.Strip(got))
		})
	}
}

// TestFilterViewLine covers the input drawn over the list, which stays up after the input
// closes so a narrowed list never looks like a list that lost rows.
func TestFilterViewLine(t *testing.T) {
	tests := []struct {
		name   string
		query  string
		typing bool
		want   string
	}{
		{
			name: "a screen with no filter draws no input",
			want: "",
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
