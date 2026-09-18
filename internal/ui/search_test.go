package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"weave/config"

	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/view"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplySearchResults confirms a response is shown only when it answers the
// query the input still holds, so the list never shows hits for text the user
// has already changed.
func TestApplySearchResults(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		msg      searchResult
		wantHits int
	}{
		{
			name:  "a response for a query the input has moved past",
			query: "current",
			msg: searchResult{
				query: "stale",
				hits:  hits("a", "b"),
			},
			wantHits: 0,
		},
		{
			name:  "a response for the query on screen",
			query: "current",
			msg: searchResult{
				query: "current",
				hits:  hits("a", "b"),
			},
			wantHits: 2,
		},
		{
			name:  "a query that matched nothing clears the list",
			query: "current",
			msg: searchResult{
				query: "current",
			},
			wantHits: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen: screenSearch,
				search: searchState{query: tt.query},
				frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
			}

			m.applySearch(tt.msg)

			assert.Len(t, m.search.turns, tt.wantHits)
		})
	}
}

// TestUpdateSearchEditsQuery covers the keys that type into the query, avoiding the nav and
// open keys, which stage a preview and so need tmux.
func TestUpdateSearchEditsQuery(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyMsg
		want string
	}{
		{
			name: "backspace on an empty query is harmless",
			keys: []tea.KeyMsg{
				{Type: tea.KeyBackspace},
			},
			want: "",
		},
		{
			name: "printable runes accumulate",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("po")},
				{Type: tea.KeyRunes, Runes: []rune("gr")},
			},
			want: "pogr",
		},
		{
			name: "space is part of the query rather than a command",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("po")},
				{Type: tea.KeySpace},
				{Type: tea.KeyRunes, Runes: []rune("gr")},
			},
			want: "po gr",
		},
		{
			name: "backspace drops the last character",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("post")},
				{Type: tea.KeyBackspace},
			},
			want: "pos",
		},
		{
			name: "the nav keys are part of the query while the line holds them",
			keys: []tea.KeyMsg{
				{Type: tea.KeyRunes, Runes: []rune("j")},
				{Type: tea.KeyRunes, Runes: []rune("k")},
			},
			want: "jk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen: screenSearch,
				keys:   config.DefaultKeybindings(),
				search: searchState{typing: true},
			}

			for _, key := range tt.keys {
				next, _ := m.updateSearch(key)
				updated, ok := next.(model)
				require.True(t, ok)
				m = updated
			}

			assert.Equal(t, tt.want, m.search.query)
		})
	}
}

// TestTrimLastRune pins that a backspace drops a whole rune rather than one byte of a
// multi-byte one.
func TestTrimLastRune(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "an empty string",
			in:   "",
			want: "",
		},
		{
			name: "a multi-byte rune is dropped whole rather than half-deleted",
			in:   "aé",
			want: "a",
		},
		{
			name: "an ascii string",
			in:   "abc",
			want: "ab",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, paint.TrimLastRune(tt.in))
		})
	}
}

func marked(s string) string {
	return transcriptstore.SnippetOpen + s + transcriptstore.SnippetClose
}

// TestMarkedTerms covers the words a highlight is looked for by, which are read back off the
// snippet.
func TestMarkedTerms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "nothing marked names no word",
			in:   "a turn that matched on a stem",
		},
		{
			name: "every marked word is named once",
			in:   "the " + marked("Sandbox") + " and the " + marked("sandbox") + " and " + marked("tenant"),
			want: []string{"sandbox", "tenant"},
		},
		{
			name: "a word with a capital outside ASCII keeps it, since the transcript is folded the same way",
			in:   marked("Ökonomie"),
			want: []string{"Ökonomie"},
		},
		{
			name: "an unclosed mark is dropped",
			in:   "the " + transcriptstore.SnippetOpen + "sandbox",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, paint.MarkedTerms(tt.in))
		})
	}
}

// TestMatchSpans covers where the highlight goes, which the search index does not say: it
// gives the turn and the words, so the words are found within the turn here.
func TestMatchSpans(t *testing.T) {
	const content = "a sandbox up top\n\nthe Sandbox tenant\nruns sandboxed\n\nsandbox down below"

	turn := int64(strings.Index(content, "the Sandbox"))
	turnLen := int64(len("the Sandbox tenant\nruns sandboxed"))

	at := func(from int, word string) paint.Span {
		i := from + strings.Index(content[from:], word)

		return paint.Span{Start: i, End: i + len(word)}
	}

	tests := []struct {
		name   string
		offset int64
		length int64
		terms  []string
		want   []paint.Span
	}{
		{
			name:   "every spelling of the word within the turn is marked",
			offset: turn,
			length: turnLen,
			terms:  []string{"sandbox"},
			want:   []paint.Span{at(int(turn), "Sandbox"), at(strings.Index(content, "runs "), "sandbox")},
		},
		{
			name:   "the turns around it are left alone",
			offset: turn,
			length: turnLen,
			terms:  []string{"top"},
		},
		{
			name:   "two of the words spelled over the same letters merge",
			offset: turn,
			length: turnLen,
			terms:  []string{"sandbox", "sandboxed"},
			want:   []paint.Span{at(int(turn), "Sandbox"), at(strings.Index(content, "runs "), "sandboxed")},
		},
		{
			name:   "the word inside a longer word is left alone",
			offset: turn,
			length: turnLen,
			terms:  []string{"and"},
		},
		{
			name:   "no word to look for marks nothing",
			offset: turn,
			length: turnLen,
		},
		{
			name:   "a turn past the end of the transcript marks nothing",
			offset: int64(len(content) + 1),
			length: turnLen,
			terms:  []string{"sandbox"},
		},
		{
			name:   "a turn running past the end stops at it",
			offset: turn,
			length: turnLen * 4,
			terms:  []string{"tenant", "down"},
			want:   []paint.Span{at(int(turn), "tenant"), at(int(turn), "down")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, paint.MatchSpans(content, tt.offset, tt.length, tt.terms))
		})
	}
}

// TestMatchSpansMarksWholeWords pins the front of the match being a word boundary, which is
// what the listing's own snippet marks, with the punctuation these transcripts are written
// with reaching well past ASCII.
func TestMatchSpansMarksWholeWords(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "a word of its own is marked",
			content: "the sandbox here",
			want:    true,
		},
		{
			name:    "a curly quote in front of it is not part of the word",
			content: "he said \u201csandbox\u201d once",
			want:    true,
		},
		{
			name:    "an em dash in front of it is not part of the word",
			content: "the thing\u2014sandbox\u2014here",
			want:    true,
		},
		{
			name:    "a letter outside ASCII in front of it is part of the word",
			content: "the Gr\u00fcnsandbox here",
		},
		{
			name:    "a letter in front of it is part of the word",
			content: "the presandbox here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spans := paint.MatchSpans(tt.content, 0, int64(len(tt.content)), []string{"sandbox"})

			if !tt.want {
				assert.Empty(t, spans, "the word is only spelled inside a longer one")

				return
			}

			word := strings.Index(tt.content, "sandbox")
			assert.Equal(t, []paint.Span{{Start: word, End: word + len("sandbox")}}, spans)
		})
	}
}

// TestMatchSpansFoldsLikeTheTermsDo pins the two sides of the search being folded the same way:
// a word the snippet marks with a capital outside ASCII is still found in the transcript, which
// a Unicode fold on one side only would miss.
func TestMatchSpansFoldsLikeTheTermsDo(t *testing.T) {
	const content = "the Ökonomie report"

	terms := paint.MarkedTerms("the " + marked("Ökonomie") + " report")
	word := strings.Index(content, "Ökonomie")

	assert.Equal(t, []paint.Span{{Start: word, End: word + len("Ökonomie")}},
		paint.MatchSpans(content, 0, int64(len(content)), terms))
}

// TestHitSpansCoverTheHoveredTurn covers the turn a highlight is measured from, which is the hit
// under the cursor and not the preview in hand.
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
			name:   "the hovered hit's own turn is marked",
			offset: second,
			want:   []paint.Span{{Start: word, End: word + len("reconcile")}},
		},
		{
			name:   "an offset past the end marks nothing",
			offset: int64(len(content) + 1),
		},
		{
			name:   "an offset below zero marks nothing",
			offset: -1,
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

// TestRenderSnippet confirms the store's match markers are consumed into styling and the snippet
// stays on one line.
func TestRenderSnippet(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantContain string
	}{
		{
			name:        "text with no markers is passed through",
			in:          "nothing marked here",
			wantContain: "nothing marked here",
		},
		{
			name:        "a marked match survives as text",
			in:          "before " + transcriptstore.SnippetOpen + "match" + transcriptstore.SnippetClose + " after",
			wantContain: "match",
		},
		{
			name:        "newlines are collapsed",
			in:          "first" + transcriptstore.SnippetOpen + "match" + transcriptstore.SnippetClose + "\nsecond",
			wantContain: "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := paint.Panel.RenderSnippet(tt.in, 0)

			assert.Contains(t, out, tt.wantContain)
			assert.NotContains(t, out, transcriptstore.SnippetOpen, "the markers are consumed")
			assert.NotContains(t, out, transcriptstore.SnippetClose)
			assert.NotContains(t, out, "\n", "the snippet occupies one row")
		})
	}
}

func hits(names ...string) []searchHit {
	out := make([]searchHit, 0, len(names))
	for _, name := range names {
		out = append(out, searchHit{
			agentID: name,
			name:    name,
			snippet: name,
		})
	}

	return out
}

// TestJumpToSearchHit pins enter going to where the conversation already is, a live agent on
// the tree and an archived one in the archive, without resuming or restoring either.
func TestJumpToSearchHit(t *testing.T) {
	tests := []struct {
		name       string
		hit        searchHit
		hidden     bool
		wantScreen screen
		wantCursor string
	}{
		{
			name:       "a hit the panel holds no row for leaves the cursor alone",
			hit:        searchHit{agentID: "gone", name: "gone"},
			wantScreen: screenAgents,
			wantCursor: "billing",
		},
		{
			name:       "a live hit lands on its row in the tree",
			hit:        searchHit{agentID: "a2", name: "payouts"},
			wantScreen: screenAgents,
			wantCursor: "payouts",
		},
		{
			name:       "a hidden hit draws the hidden agents so its row exists",
			hit:        searchHit{agentID: "a2", name: "payouts"},
			hidden:     true,
			wantScreen: screenAgents,
			wantCursor: "payouts",
		},
		{
			name: "an archived hit lands in the archive",
			hit: searchHit{
				agentID:  "a3",
				name:     "old-work",
				archived: true,
			},
			wantScreen: screenArchive,
			wantCursor: "old-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSearchAgents{}
			m := filterModel()
			m.api = api{
				agents:    client,
				workflows: emptyLists{},
				schedules: emptyLists{},
			}
			m.screen = screenSearch
			m.search = searchState{query: "ledger", turns: []searchHit{tt.hit}}
			m.listing.agents[1].agent.Hidden = tt.hidden
			m.archive.agents = []agentstore.Agent{{
				ID:         "a3",
				Name:       "old-work",
				WorkflowID: "w1",
			}}

			updated, _ := m.route(keyMsg("enter"))
			got, ok := updated.(model)
			require.True(t, ok)

			require.Equal(t, tt.wantScreen, got.screen)
			assert.Nil(t, client.restored, "nothing is taken out of the archive")
			assert.Nil(t, client.resumed, "nothing is put back in a pane")

			rows, cursor := got.agentRows(), got.tree.cursor
			if got.screen == screenArchive {
				got.applyArchive(got.archive.agents, nil, "")
				rows, cursor = got.archiveRows(), got.archive.cursor
			}

			require.Less(t, cursor, len(rows))
			assert.Equal(t, tt.wantCursor, rows[cursor].item.agent.Name)
		})
	}
}

// TestSearchEscape covers esc on the search screen, which steps back the way it does over a
// filtered list: the hits hand the keys to the query line, and the line leaves the screen.
func TestSearchEscape(t *testing.T) {
	tests := []struct {
		name       string
		typing     bool
		hits       []searchHit
		wantScreen screen
		wantTyping bool
	}{
		{
			name:       "esc over the hits puts the keys back in the query line",
			hits:       hits("a", "b", "c"),
			wantScreen: screenSearch,
			wantTyping: true,
		},
		{
			name:       "esc over an empty result puts them back the same way",
			wantScreen: screenSearch,
			wantTyping: true,
		},
		{
			name:       "esc in the query line leaves the screen",
			typing:     true,
			hits:       hits("a", "b", "c"),
			wantScreen: screenAgents,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.api = api{
				agents:    &fakeSearchAgents{},
				workflows: emptyLists{},
				schedules: emptyLists{},
			}
			m.screen = screenSearch
			m.search = searchState{
				typing: tt.typing,
				query:  "ledger",
				turns:  tt.hits,
			}

			updated, _ := m.route(keyMsg("esc"))
			next, ok := updated.(model)
			require.True(t, ok)

			assert.Equal(t, tt.wantScreen, next.screen)
			assert.Equal(t, tt.wantTyping, next.search.typing)
		})
	}
}

// TestSearchQueryOwnsTheKeys covers the query line taking every key while it is being typed
// into, which is what keeps a letter bound elsewhere from acting instead of typing.
func TestSearchQueryOwnsTheKeys(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.search = searchState{
		typing: true,
		query:  "ledger",
		turns:  hits("a"),
	}

	updated, _ := m.route(keyMsg("?"))
	got, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, "ledger?", got.search.query)
	assert.False(t, got.helpExpanded, "the help key is typed rather than drawing the legend")
}

// TestSearchHitsTakeTheKeys covers what the hits answer once enter has closed the query line:
// the shared navigation keys, which type while the line is open, and the two keys that hand
// them back to it.
func TestSearchHitsTakeTheKeys(t *testing.T) {
	tests := []struct {
		name       string
		keys       []string
		wantCursor int
		wantTyping bool
		wantQuery  string
	}{
		{
			name:       "enter closes the line and leaves the query standing",
			keys:       []string{"enter"},
			wantCursor: 0,
			wantQuery:  "ledger",
		},
		{
			name:       "the nav keys run down the hits rather than typing",
			keys:       []string{"enter", "j", "j"},
			wantCursor: 2,
			wantQuery:  "ledger",
		},
		{
			name:       "the bottom key lands on the last hit",
			keys:       []string{"enter", "G"},
			wantCursor: 2,
			wantQuery:  "ledger",
		},
		{
			name:       "the top key acts on its second press",
			keys:       []string{"enter", "G", "g", "g"},
			wantCursor: 0,
			wantQuery:  "ledger",
		},
		{
			name:       "the filter key puts the keys back in the query line",
			keys:       []string{"enter", "j", "/"},
			wantCursor: 1,
			wantTyping: true,
			wantQuery:  "ledger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.api = api{
				agents:    &fakeSearchAgents{},
				workflows: emptyLists{},
				schedules: emptyLists{},
			}
			m.screen = screenSearch
			m.search = searchState{
				typing: true,
				query:  "ledger",
				turns:  hits("a", "b", "c"),
			}

			for _, k := range tt.keys {
				updated, _ := m.route(keyMsg(k))
				next, ok := updated.(model)
				require.True(t, ok)
				m = next
			}

			require.Equal(t, screenSearch, m.screen)
			assert.Equal(t, tt.wantCursor, m.search.cursor)
			assert.Equal(t, tt.wantTyping, m.search.typing)
			assert.Equal(t, tt.wantQuery, m.search.query)
		})
	}
}

type fakeSearchAgents struct {
	agentv1connect.AgentServiceClient
	restored []string
	resumed  []string
}

func (f *fakeSearchAgents) RestoreAgents(_ context.Context, req *connect.Request[agentv1.RestoreAgentsRequest]) (*connect.Response[agentv1.RestoreAgentsResponse], error) {
	f.restored = append(f.restored, req.Msg.GetIds()...)

	response := &agentv1.RestoreAgentsResponse{}
	for _, id := range req.Msg.GetIds() {
		response.Agents = append(response.Agents, &agentv1.Agent{Id: id, Name: "api-refactor"})
	}

	return connect.NewResponse(response), nil
}

func (f *fakeSearchAgents) ResumeAgent(_ context.Context, req *connect.Request[agentv1.ResumeAgentRequest]) (*connect.Response[agentv1.ResumeAgentResponse], error) {
	f.resumed = append(f.resumed, req.Msg.GetId())

	return connect.NewResponse(&agentv1.ResumeAgentResponse{
		Agent: &agentv1.Agent{
			Id:     req.Msg.GetId(),
			Name:   "api-refactor",
			PaneId: "%9",
		},
	}), nil
}

// TestSearchGroupsByAgent covers two agents sharing a name, which archived rows make ordinary,
// and the rank ordering scattering one agent's hits: the list is headed once per agent, and an
// agent's hits are drawn together under it.
func TestSearchGroupsByAgent(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "ledger"}

	m.applySearch(searchResult{
		query: "ledger",
		hits: []searchHit{
			{
				agentID: "a1",
				name:    "dispatcher",
				snippet: "first",
			},
			{
				agentID: "a2",
				name:    "dispatcher",
				snippet: "second",
			},
			{
				agentID: "a1",
				name:    "dispatcher",
				snippet: "third",
			},
		},
	})

	var snippets []string
	for _, hit := range m.search.turns {
		snippets = append(snippets, hit.snippet)
	}
	assert.Equal(t, []string{"first", "third", "second"}, snippets, "an agent's hits sit together")

	view := ansi.Strip(m.searchView())
	assert.Equal(t, 2, strings.Count(view, "dispatcher"), "one header per agent rather than per name")
}

// searchRowNamed is the row the screen built for the hit on the agent of that name.
func searchRowNamed(t *testing.T, lines []searchLine, name string) view.Row {
	t.Helper()

	for _, line := range lines {
		if line.header && line.row.Name == name {
			return line.row
		}
	}

	t.Fatalf("no row for %q", name)

	return view.Row{}
}

// TestSearchRowCarriesTheAgent covers a hit's row wearing what the agent's own row wears, which
// is the whole reason the two screens draw through the same package.
func TestSearchRowCarriesTheAgent(t *testing.T) {
	marked := "a"

	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "ledger"}
	m.listing.marks = map[string]string{agentRowKey("a1"): marked}
	m.listing.agents = []agentItem{
		{
			agent: agentstore.Agent{
				ID:         "a1",
				Name:       "billing",
				WorkflowID: "w1",
				State:      agentstore.StateThinking,
			},
			alive:      true,
			unreadMail: true,
		},
	}
	m.archive.agents = []agentstore.Agent{
		{
			ID:         "a2",
			Name:       "payouts",
			WorkflowID: "w1",
			State:      agentstore.StateFailedToCleanup,
		},
	}

	m.applySearch(searchResult{
		query: "ledger",
		hits: []searchHit{
			{
				agentID: "a1",
				name:    "billing",
				snippet: "first",
			},
			{
				agentID:  "a2",
				name:     "payouts",
				archived: true,
				snippet:  "second",
			},
		},
	})

	lines, _ := m.searchLines()

	live := searchRowNamed(t, lines, "billing")
	assert.Equal(t, view.StatusThinking, live.Status, "the live row wears the state it is in")
	assert.Equal(t, view.ProcessAlive, live.Process, "and has a process behind it")
	assert.Equal(t, marked, live.Mark, "the live row wears its mark")
	assert.True(t, live.UnreadMail, "the live row says mail is waiting")
	assert.Empty(t, live.Tag)

	archived := searchRowNamed(t, lines, "payouts")
	assert.Equal(t, archivedTagText, archived.Tag, "the archived row still says so")
	assert.True(t, archived.Dim)
	assert.Equal(t, view.StatusBroken, archived.Status, "an archived agent that died failing keeps its bar")
	assert.Equal(t, view.ProcessStopped, archived.Process)
}

// TestSearchSnippetsHangOffTheirHeader covers the one thing the two kinds of line have to agree
// about: a snippet is drawn in from the column the row above it opens its name at.
func TestSearchSnippetsHangOffTheirHeader(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "ledger"}
	m.listing.agents = []agentItem{{
		agent: agentstore.Agent{ID: "a1", Name: "billing", WorkflowID: "w1"},
		alive: true,
	}}

	m.applySearch(searchResult{
		query: "ledger",
		hits:  []searchHit{{agentID: "a1", name: "billing", snippet: "the ledger balanced"}},
	})

	rows, _ := m.searchRows()

	drawn := strings.Join(ansiStripped(rows), "\n")

	assert.Equal(t, snippetIndent(), textColumn(t, headerLine(t, drawn, "billing"), "billing"),
		"a header opens its name at the gutter")
	assert.Equal(t, snippetIndent(), textColumn(t, headerLine(t, drawn, "balanced"), "the ledger"),
		"and its snippets open under that name")
}

// ansiStripped is the drawn rows with their styling taken off.
func ansiStripped(rows []string) []string {
	return xslice.Map(rows, ansi.Strip)
}

// textColumn is the column a drawn row opens the given text at, counted in runes since nothing
// left of a result's name is drawn as a glyph wider than one.
func textColumn(t *testing.T, line, text string) int {
	t.Helper()

	at := strings.Index(line, text)
	require.GreaterOrEqual(t, at, 0, "no %q in %q", text, line)

	return utf8.RuneCountInString(line[:at])
}

// TestSearchKeysNameHitsRatherThanAgents covers the same agent standing on several lines at
// once: the row a name hit draws answers to that hit, while the row heading a run of snippets
// answers to nothing, so hovering a snippet cannot light the agent named above it.
func TestSearchKeysNameHitsRatherThanAgents(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "billing"}
	m.listing.agents = []agentItem{{
		agent: agentstore.Agent{ID: "a1", Name: "billing", WorkflowID: "w1"},
		alive: true,
	}}

	m.applySearch(searchResult{
		query: "billing",
		hits:  []searchHit{{agentID: "a1", name: "billing", snippet: "the billing ledger"}},
	})

	require.Len(t, m.searchHits(), 2, "one agent, matched by its name and by a turn")

	lines, _ := m.searchLines()
	keys := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.header {
			keys = append(keys, line.row.Key)
		}
	}
	require.Len(t, keys, 2, "the agent is named once per section")
	assert.NotEmpty(t, keys[0], "the name hit is a row the cursor can stand on")
	assert.Empty(t, keys[1], "the row heading a run of snippets is not")

	// The cursor opens on the name hit, so that row wears the caret and nothing else does.
	rows, cursor := m.searchRows()
	assert.Equal(t, 1, cursor)
	assert.Equal(t, 0, caretColumn(rows[1]), "the name hit wears the caret in its focus cell")

	moved, _ := m.moveSearchCursor(1)
	next, ok := moved.(model)
	require.True(t, ok)

	rows, cursor = next.searchRows()
	assert.Equal(t, 5, cursor, "the step lands on the snippet under the heading")
	assert.Equal(t, snippetIndent()-len(blankCursor), caretColumn(rows[5]), "the snippet wears the caret")
	assert.Equal(t, -1, caretColumn(rows[4]), "the row naming the agent does not")
	assert.Equal(t, -1, caretColumn(rows[1]), "and neither does its name hit")
}

// caretColumn is where a drawn row wears the cursor, or -1 where it wears none.
func caretColumn(line string) int {
	at := strings.Index(ansi.Strip(line), strings.TrimSpace(rowCursorGlyph))
	if at < 0 {
		return -1
	}

	return utf8.RuneCountInString(ansi.Strip(line)[:at])
}

// TestSearchRowWithoutTheAgent covers a hit named before its listing has landed, which is what a
// first paint looks like, and which is only allowed to cost the marks.
func TestSearchRowWithoutTheAgent(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "ledger"}
	m.listing.agents = nil

	m.applySearch(searchResult{
		query: "ledger",
		hits: []searchHit{{
			agentID: "gone",
			name:    "billing",
			snippet: "first",
		}},
	})

	lines, _ := m.searchLines()
	row := searchRowNamed(t, lines, "billing")

	assert.Equal(t, view.ProcessNone, row.Process, "a hit nothing is known about has no bar to draw")
	assert.Empty(t, row.Tag)

	header := headerLine(t, ansi.Strip(m.searchView()), "billing")
	assert.Equal(t, strings.Repeat(" ", snippetIndent())+"billing", header,
		"and still opens its name where every other row does")
}

func headerLine(t *testing.T, view, name string) string {
	t.Helper()

	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, name) {
			return line
		}
	}

	t.Fatalf("no header for %q in:\n%s", name, view)

	return ""
}

// TestJumpLeavesTheSearchBehind covers the window going back with the screen.
func TestJumpLeavesTheSearchBehind(t *testing.T) {
	tests := []struct {
		name      string
		hit       searchHit
		wantSwaps int
		wantDrawn string
		wantSolo  bool
	}{
		{
			name: "a live hit takes the agent's own pane",
			hit: searchHit{
				agentID: "a2",
				name:    "payouts",
				offset:  4,
				length:  3,
				snippet: marked("two"),
			},
			wantSwaps: 1,
		},
		{
			name: "an archived hit takes the archive's own record",
			hit: searchHit{
				agentID:  "a3",
				name:     "old-work",
				archived: true,
				offset:   4,
				length:   3,
				snippet:  marked("two"),
			},
			wantDrawn: "kept until deleted",
			// The archive draws its own two columns, so it keeps the window the search was
			// holding rather than handing it back to panes it is not going to use.
			wantSolo: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{width: 80, height: 40}
			m := filterModel()
			m.tmux, m.popup = tmux, NewPopupWindow(tmux, "test-session")
			m.frame = layout.New(tmux, config.DefaultLayout())
			m.frame.Attach("%0")
			m.width, m.height = 120, 40
			m.api = api{
				agents:    &fakeSearchAgents{},
				workflows: emptyLists{},
				schedules: emptyLists{},
			}
			m.screen = screenSearch
			m.listing.agents[1].alive = true
			m.listing.agents[1].agent.Pane = "%9"
			m.archive.agents = []agentstore.Agent{{
				ID:         "a3",
				Name:       "old-work",
				WorkflowID: "w1",
			}}
			m.search = searchState{query: "ledger", turns: []searchHit{tt.hit}}

			updated, _ := m.route(keyMsg("enter"))
			landed, ok := updated.(model)
			require.True(t, ok)

			assert.Empty(t, landed.search.preview.conversation, "the preview does not follow the jump")
			assert.Equal(t, tt.wantSolo, landed.frame.IsSolo(), "the window goes to whatever the screen jumped to needs")
			assert.Equal(t, tt.wantSwaps, tmux.swaps, "the row's own stage is what is drawn")
			if tt.wantDrawn != "" {
				assert.Contains(t, landed.View(), tt.wantDrawn, "the row's own record is what is drawn beside the list")
			}
			assert.Empty(t, landed.search.query, "the search is let go of")
			assert.Empty(t, landed.search.turns, "the search is let go of")
		})
	}
}

// TestJumpDropsALatePreview covers the transcript a hit was still being fetched for landing
// after the jump has left, which used to put the search back over the agent it jumped to.
func TestJumpDropsALatePreview(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	m := filterModel()
	m.tmux, m.popup = tmux, NewPopupWindow(tmux, "test-session")
	m.frame = layout.New(tmux, config.DefaultLayout())
	m.frame.Attach("%0")
	m.api = api{
		agents:    &fakeSearchAgents{},
		workflows: emptyLists{},
		schedules: emptyLists{},
	}
	m.screen = screenSearch
	m.listing.agents[1].alive = true
	m.listing.agents[1].agent.Pane = "%9"
	m.search = searchState{
		query: "ledger",
		turns: []searchHit{{
			agentID: "a2",
			name:    "payouts",
			offset:  4,
			length:  3,
			snippet: marked("two"),
		}},
	}

	updated, _ := m.route(keyMsg("enter"))
	landed, ok := updated.(model)
	require.True(t, ok)

	late := previewResult{
		agentID: "a2",
		query:   "ledger",
		content: "one\ntwo\nthree\n",
	}
	landed.applyPreview(late)

	assert.Empty(t, landed.search.preview.conversation, "the late preview is dropped")
}

// TestSearchViewFitsThePane covers a common query returning more hits than the pane is tall,
// which used to be drawn whole and scroll the panel away.
func TestSearchViewFitsThePane(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.height = 14
	m.search = searchState{query: "agent"}

	var found []searchHit
	for i := range 20 {
		found = append(found, searchHit{
			agentID: fmt.Sprintf("a%d", i),
			name:    fmt.Sprintf("agent-%d", i),
			snippet: fmt.Sprintf("hit %d", i),
		})
	}

	m.applySearch(searchResult{query: "agent", hits: found})

	view := drawnLines(m.searchView())
	assert.LessOrEqual(t, len(view), m.height, "the view is no taller than the pane")
	assert.Contains(t, strings.Join(view, "\n"), "more", "the rows it could not draw are counted")

	m.search.cursor = len(m.search.turns) - 1
	bottom := drawnLines(m.searchView())
	assert.LessOrEqual(t, len(bottom), m.height, "the view stays inside the pane at the end of the list")
	assert.Contains(t, strings.Join(bottom, "\n"), "hit 19", "the hit under the cursor is drawn")
}

// drawnLines is the screen as View hands it to bubbletea, which trims the newline the last row
// carries before the pane is asked to hold it.
func drawnLines(view string) []string {
	return strings.Split(strings.TrimRight(ansi.Strip(view), "\n"), "\n")
}

// TestSearchNameSectionStandsFirst covers the agents whose own name says the query, which are
// read off the listings and drawn above the turns that said it.
func TestSearchNameSectionStandsFirst(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.archive.agents = []agentstore.Agent{{
		ID:         "a3",
		Name:       "billing-old",
		WorkflowID: "w1",
	}}
	m.search = searchState{query: "BILL"}

	m.applySearch(searchResult{
		query: "BILL",
		hits: []searchHit{{
			agentID: "a2",
			name:    "payouts",
			snippet: "bill",
		}},
	})

	hits := m.searchHits()
	require.Len(t, hits, 3)
	assert.Equal(t, []string{"a1", "a3", "a2"}, []string{hits[0].agentID, hits[1].agentID, hits[2].agentID},
		"the live name, then the archived one, then the turn")
	assert.Equal(t, nameSection, hits[0].section)
	assert.True(t, hits[1].archived, "an archived agent is named as archived")

	rows, cursor := m.searchRows()
	require.NotEmpty(t, rows)
	assert.Equal(t, "agents", ansi.Strip(rows[0]), "the names are headed as their own section")
	assert.Contains(t, ansi.Strip(rows[cursor]), "billing", "the cursor opens on the first name")
	assert.Contains(t, ansi.Strip(strings.Join(rows, "\n")), "conversations",
		"the turns are headed apart from the names")
}

// TestSearchNameHitsTakeTheCursor covers the cursor running from the name section into the
// turns, the two being one list to walk.
func TestSearchNameHitsTakeTheCursor(t *testing.T) {
	m := filterModel()
	m.api = api{
		agents:    &fakeSearchAgents{},
		workflows: emptyLists{},
		schedules: emptyLists{},
	}
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "bill"}

	m.applySearch(searchResult{
		query: "bill",
		hits: []searchHit{{
			agentID: "a2",
			name:    "payouts",
			snippet: "bill",
		}},
	})

	opened, ok := m.hoveredHit()
	require.True(t, ok)
	assert.Equal(t, "a1", opened.agentID, "the cursor opens on the name")

	updated, _ := m.moveSearchCursor(1)
	next, ok := updated.(model)
	require.True(t, ok)

	hit, ok := next.hoveredHit()
	require.True(t, ok)
	assert.Equal(t, "a2", hit.agentID, "the step off the name lands on the turn")
}

// TestSearchNameHitJumps covers enter on a name hit, which goes to the agent the same way a
// hit on its conversation does.
func TestSearchNameHitJumps(t *testing.T) {
	m := filterModel()
	m.api = api{
		agents:    &fakeSearchAgents{},
		workflows: emptyLists{},
		schedules: emptyLists{},
	}
	m.screen = screenSearch
	m.search = searchState{query: "payout"}

	updated, _ := m.jumpToSearchHit()
	landed, ok := updated.(model)
	require.True(t, ok)

	assert.Equal(t, screenAgents, landed.screen)
	require.Less(t, landed.tree.cursor, len(rowNames(landed.agentRows())))
	assert.Equal(t, "payouts", rowNames(landed.agentRows())[landed.tree.cursor])
}

// TestNameMatches covers where a name is marked, which is the query as the name spells it rather
// than as it was typed.
func TestNameMatches(t *testing.T) {
	assert.Equal(t, []view.Span{{Start: 0, End: 4}}, nameMatches("biLLing", " bill "))
	assert.Nil(t, nameMatches("billing", "  "))
	assert.Nil(t, nameMatches("billing", "payouts"))
}

// TestSearchCursorSurvivesARefresh covers the names among the hits being read off the listings:
// a refresh that changes them moves the rows the cursor is counting, and used to leave it
// standing on another agent's hit or off the end of the list.
func TestSearchCursorSurvivesARefresh(t *testing.T) {
	sectioned := func() model {
		m := filterModel()
		m.api = api{
			agents:    &fakeSearchAgents{},
			workflows: emptyLists{},
			schedules: emptyLists{},
		}
		m.screen = screenSearch
		m.width = 80
		m.search = searchState{query: "bill"}
		m.applySearch(searchResult{
			query: "bill",
			hits: []searchHit{{
				agentID: "a2",
				name:    "payouts",
				snippet: "bill",
			}},
		})
		m.search.cursor = 1

		return m
	}

	t.Run("an agent whose name says the query joining the listing", func(t *testing.T) {
		m := sectioned()

		cmd := m.applyRefresh(refreshResult{agents: append([]agentItem{
			{agent: agentstore.Agent{
				ID:         "a4",
				Name:       "billing-two",
				WorkflowID: "w1",
			}},
		}, m.listing.agents...)})

		hit, ok := m.hoveredHit()
		require.True(t, ok)
		assert.Equal(t, "a2", hit.agentID, "the cursor stays on the hit it was standing on")
		assert.Nil(t, cmd, "the preview beside it is the one already fetched")
	})

	t.Run("the hit under the cursor going", func(t *testing.T) {
		m := sectioned()
		m.search.cursor = 0

		cmd := m.applyRefresh(refreshResult{})

		hit, ok := m.hoveredHit()
		require.True(t, ok, "a row still wears the cursor")
		assert.Equal(t, "a2", hit.agentID, "the cursor lands on what is left")
		assert.NotNil(t, cmd, "the preview is fetched for the hit it landed on")
	})
}
