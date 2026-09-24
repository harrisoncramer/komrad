package ui

import (
	"strings"
	"testing"
	"time"

	"weave/internal/ui/paint"
	"weave/internal/ui/panel"
	"weave/internal/view"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func marked(s string) string {
	return paint.MarkOpen + s + paint.MarkClose
}

// TestHitSpansCoverTheHoveredTurn covers the turn a highlight is measured from, which is the turn
// the hit names and not the whole transcript.
func TestHitSpansCoverTheHoveredTurn(t *testing.T) {
	const content = "alpha reconcile one\n\nbeta reconcile two\n"

	second := int64(strings.Index(content, "beta"))
	word := strings.Index(content[second:], "reconcile") + int(second)

	tests := []struct {
		name   string
		offset int64
		want   []paint.Span
	}{
		{
			name:   "an offset past the end marks nothing",
			offset: int64(len(content) + 1),
		},
		{
			name:   "an offset below zero marks nothing",
			offset: -1,
		},
		{
			name:   "the hit's own turn is marked",
			offset: second,
			want:   []paint.Span{{Start: word, End: word + len("reconcile")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := searchHit{
				agentID: "a1",
				offset:  tt.offset,
				length:  int64(len("beta reconcile two")),
				snippet: marked("reconcile"),
			}

			assert.Equal(t, tt.want, hit.spans(content))
		})
	}
}

// TestAgentSpansCoverEveryTurn covers an agent whose conversation said the query more than once:
// every turn is marked, in the order the transcript reads rather than the order the search ranked
// them.
func TestAgentSpansCoverEveryTurn(t *testing.T) {
	const content = "alpha reconcile one\n\nbeta reconcile two\n"

	first := strings.Index(content, "reconcile")
	second := strings.LastIndex(content, "reconcile")
	turnAt := int64(strings.Index(content, "beta"))

	agent := searchAgent{
		id: "a1",
		turns: []searchHit{
			{
				agentID: "a1",
				offset:  turnAt,
				length:  int64(len("beta reconcile two")),
				snippet: marked("reconcile"),
			},
			{
				agentID: "a1",
				offset:  0,
				length:  int64(len("alpha reconcile one")),
				snippet: marked("reconcile"),
			},
		},
	}

	assert.Equal(t, []paint.Span{
		{Start: first, End: first + len("reconcile")},
		{Start: second, End: second + len("reconcile")},
	}, agent.spans(content))
}

func hits(names ...string) []searchHit {
	out := xslice.Map(names, func(name string) searchHit {
		return searchHit{
			agentID: name,
			name:    name,
			snippet: name,
		}
	})

	return out
}

// TestSearchTellsAHiddenHitFromAnArchivedOne pins the screen where the two used to draw alike:
// a hidden agent still running is bright with the hidden mark, a dormant hidden one is dim with
// it, and an archived one is dim without it.
func TestSearchTellsAHiddenHitFromAnArchivedOne(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchScreen{query: "ledger"}
	m.listing.agents = []panel.AgentRow{
		{Agent: panel.Agent{
			ID:         "a1",
			Name:       "running",
			WorkflowID: "w1",
			Hidden:     true,
		}, Alive: true},
		{Agent: panel.Agent{
			ID:         "a2",
			Name:       "dormant",
			WorkflowID: "w1",
			Hidden:     true,
			State:      panel.StateIdle,
		}},
	}
	m.listing.archivedAgents = archivedItems([]panel.Agent{
		{
			ID:         "a3",
			Name:       "put-away",
			WorkflowID: "w1",
			Hidden:     true,
			ArchivedAt: xptr.Ptr(time.Now()),
		},
	})
	m.search.apply(&m, m.shared(), searchResult{
		query: "ledger",
		hits: []searchHit{
			{
				agentID: "a1",
				name:    "running",
				snippet: "first",
			},
			{
				agentID: "a2",
				name:    "dormant",
				snippet: "second",
			},
			{
				agentID:  "a3",
				name:     "put-away",
				archived: true,
				snippet:  "third",
			},
		},
	})

	built, _ := searchRows(m)
	rows := xslice.Map([]string{"running", "dormant", "put-away"}, func(name string) view.Row {
		return searchRowNamed(t, built, name)
	})

	list := view.Prepare(view.Snapshot{Rows: rows, Columns: view.NoColumns()})
	require.Len(t, list.Lines, 3)

	running, dormant, archived := list.Lines[0], list.Lines[1], list.Lines[2]

	assert.Equal(t, view.EmphasisNormal, running.Emphasis, "a hidden agent still running is bright")
	assert.False(t, running.Hidden.IsBlank(), "and carries the hidden mark")

	assert.Equal(t, view.EmphasisDim, dormant.Emphasis, "a hidden agent whose pane has gone is dim")
	assert.False(t, dormant.Hidden.IsBlank(), "and keeps the hidden mark")

	assert.Equal(t, view.EmphasisDim, archived.Emphasis, "an archived agent is dim")
	assert.True(t, archived.Hidden.IsBlank(), "and carries no hidden mark, hidden before it was put away or not")
}

// searchRows is the rows the screen built for the agents it found, and the same rows drawn.
func searchRows(m model) ([]view.Row, []string) {
	pane := m.paneWidth()
	snapshot := m.search.snapshot(m.shared(), m.search.rows(m.shared()), m.listPane(pane))

	return snapshot.Rows, view.Rows(view.Prepare(snapshot), paint.Panel, pane)
}

// searchRowNamed is the row the screen built for the agent of that name.
func searchRowNamed(t *testing.T, rows []view.Row, name string) view.Row {
	t.Helper()

	for _, row := range rows {
		if row.Name == name {
			return row
		}
	}

	t.Fatalf("no row for %q", name)

	return view.Row{}
}

// drawnLines is the screen as View hands it to bubbletea, which trims the newline the last row
// carries before the pane is asked to hold it.
func drawnLines(view string) []string {
	return strings.Split(strings.TrimRight(ansi.Strip(view), "\n"), "\n")
}

// TestNameMatches covers where a name is marked, which is the query as the name spells it rather
// than as it was typed.
func TestNameMatches(t *testing.T) {
	assert.Equal(t, []view.Span{{Start: 0, End: 4}}, nameMatches("biLLing", " bill "))
	assert.Nil(t, nameMatches("billing", "  "))
	assert.Nil(t, nameMatches("billing", "payouts"))
}

// TestFlattenSnippet covers a snippet read into one line of text with the matched stretches
// located in it.
func TestFlattenSnippet(t *testing.T) {
	tests := []struct {
		name        string
		snippet     string
		wantText    string
		wantMatches []view.Span
	}{
		{
			name:     "nothing marked",
			snippet:  "plain words",
			wantText: "plain words",
		},
		{
			name:     "a close with no open is ignored",
			snippet:  "odd" + paint.MarkClose + " words",
			wantText: "odd words",
		},
		{
			name:        "newlines and runs of spaces are one space",
			snippet:     "the\n\n  " + marked("ledger") + "\tis   here",
			wantText:    "the ledger is here",
			wantMatches: []view.Span{{Start: 4, End: 10}},
		},
		{
			name:        "two matches",
			snippet:     marked("pgx") + " and " + marked("pool"),
			wantText:    "pgx and pool",
			wantMatches: []view.Span{{Start: 0, End: 3}, {Start: 8, End: 12}},
		},
		{
			name:        "a match in bytes rather than runes",
			snippet:     "café " + marked("ledger"),
			wantText:    "café ledger",
			wantMatches: []view.Span{{Start: 6, End: 12}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, matches := flattenSnippet(tt.snippet)

			assert.Equal(t, tt.wantText, text)
			assert.Equal(t, tt.wantMatches, matches)
		})
	}
}

// TestSearchHitLead covers who a turn is said by and where it sits, as the row leads with.
func TestSearchHitLead(t *testing.T) {
	tests := []struct {
		name string
		hit  searchHit
		want string
	}{
		{
			name: "no role at all",
			hit:  searchHit{seq: 4},
			want: "#4",
		},
		{
			name: "a role the panel has no word for",
			hit:  searchHit{role: "system", seq: 0},
			want: "system #0",
		},
		{
			name: "the person prompting",
			hit:  searchHit{role: "user", seq: 518},
			want: "you #518",
		},
		{
			name: "the agent",
			hit:  searchHit{role: "assistant", seq: 1},
			want: "agent #1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.hit.lead())
		})
	}
}

// searchHelp is the search legend as the frame draws it.
func (m model) searchHelp() keyMap {
	return m.search.help(m.shared())
}

// TestSearchDropsAnswersForAQueryMovedPast covers an answer landing after the input has changed.
func TestSearchDropsAnswersForAQueryMovedPast(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.search = searchScreen{query: "current"}

	m.search.apply(&m, m.shared(), searchResult{
		query: "stale",
		hits:  hits("a", "b"),
	})
	m.search.applyPreview(m.shared(), previewResult{
		agentID: "a",
		query:   "stale",
		content: "a transcript",
	})

	assert.Empty(t, m.search.turns)
	assert.Empty(t, m.search.preview.conversation)
}

// TestSearchForgetsAPageInFlight covers a fresh search dropping the page the old one was waiting on.
func TestSearchForgetsAPageInFlight(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.search = searchScreen{
		query:  "reconciler",
		paging: true,
	}

	m.search.apply(&m, m.shared(), searchResult{
		query: "reconciler",
		hits:  hits("b"),
	})

	assert.False(t, m.search.paging)
}
