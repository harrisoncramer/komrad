package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"weave/config"

	agentstore "weave/internal/store/agent"
	transcriptstore "weave/internal/store/transcript"

	"weave/internal/ui/layout"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

			assert.Len(t, m.search.hits, tt.wantHits)
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
			assert.Equal(t, tt.want, trimLastRune(tt.in))
		})
	}
}

func marked(s string) string {
	return transcriptstore.SnippetOpen + s + transcriptstore.SnippetClose
}

// TestMarkedTerms covers the words a highlight is looked for by, which are read back off the
// snippet because a stemmed match spells the query's word its own way.
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
			assert.Equal(t, tt.want, markedTerms(tt.in))
		})
	}
}

// TestMatchSpans covers where the highlight goes, which the search index does not say: it
// gives the turn and the words, so the words are found within the turn here.
func TestMatchSpans(t *testing.T) {
	const content = "a sandbox up top\n\nthe Sandbox tenant\nruns sandboxed\n\nsandbox down below"

	turn := int64(strings.Index(content, "the Sandbox"))
	turnLen := int64(len("the Sandbox tenant\nruns sandboxed"))

	at := func(from int, word string) span {
		i := from + strings.Index(content[from:], word)

		return span{start: i, end: i + len(word)}
	}

	tests := []struct {
		name   string
		offset int64
		length int64
		terms  []string
		want   []span
	}{
		{
			name:   "every spelling of the word within the turn is marked",
			offset: turn,
			length: turnLen,
			terms:  []string{"sandbox"},
			want:   []span{at(int(turn), "Sandbox"), at(strings.Index(content, "runs "), "sandbox")},
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
			want:   []span{at(int(turn), "Sandbox"), at(strings.Index(content, "runs "), "sandboxed")},
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
			want:   []span{at(int(turn), "tenant"), at(int(turn), "down")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchSpans(content, tt.offset, tt.length, tt.terms))
		})
	}
}

// TestMatchSpansFoldsLikeTheTermsDo pins the two sides of the search being folded the same way:
// a word the snippet marks with a capital outside ASCII is still found in the transcript, which
// a Unicode fold on one side only would miss.
func TestMatchSpansFoldsLikeTheTermsDo(t *testing.T) {
	const content = "the Ökonomie report"

	terms := markedTerms("the " + marked("Ökonomie") + " report")
	word := strings.Index(content, "Ökonomie")

	assert.Equal(t, []span{{start: word, end: word + len("Ökonomie")}},
		matchSpans(content, 0, int64(len(content)), terms))
}

// TestHitSpansAreTheHoveredHits covers the turn a highlight is measured from, which is the hit
// under the cursor rather than the preview in hand: an agent's hits sit next to each other, so
// the transcript on hand can have been fetched for the hit before this one.
func TestHitSpansAreTheHoveredHits(t *testing.T) {
	const content = "alpha reconcile one\n\nbeta reconcile two\n"

	second := int64(strings.Index(content, "beta"))
	hit := searchHit{
		agentID: "a1",
		offset:  second,
		length:  int64(len("beta reconcile two")),
		snippet: marked("reconcile"),
	}

	stale := previewResult{agentID: "a1", content: content}

	word := strings.Index(content[second:], "reconcile") + int(second)

	assert.Equal(t, []span{{start: word, end: word + len("reconcile")}},
		matchFor(hit, stale).spans, "the hovered hit's own turn is marked")
}

// TestRenderSnippet confirms the store's match markers are consumed into styling
// and the snippet stays on one line, since the list gives it one row.
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
			out := renderSnippet(tt.in, 0)

			assert.Contains(t, out, tt.wantContain)
			assert.NotContains(t, out, transcriptstore.SnippetOpen, "the markers are consumed")
			assert.NotContains(t, out, transcriptstore.SnippetClose)
			assert.NotContains(t, out, "\n", "the snippet occupies one row")
		})
	}
}

// TestClipToMatch pins the match surviving the cut and the row still fitting, wherever the
// match sits in the snippet.
func TestClipToMatch(t *testing.T) {
	const (
		lead  = "the quick brown fox jumps over the lazy dog and keeps on running for a while "
		trail = " and then carries on well past where any pane could show it all"
	)

	marked := func(match string) string {
		return transcriptstore.SnippetOpen + match + transcriptstore.SnippetClose
	}

	tests := []struct {
		name  string
		in    string
		width int
		want  string
	}{
		{
			name:  "an unmeasured pane clips nothing",
			in:    lead + marked("needle") + trail,
			width: 0,
			want:  "needle",
		},
		{
			name:  "a snippet already narrow enough is left alone",
			in:    "short " + marked("needle"),
			width: 40,
			want:  "needle",
		},
		{
			name:  "a match past the width is pulled into view",
			in:    lead + marked("needle") + trail,
			width: 40,
			want:  "needle",
		},
		{
			name:  "a match at the very end is still shown",
			in:    lead + trail + marked("needle"),
			width: 40,
			want:  "needle",
		},
		{
			name:  "an unmarked snippet is clipped from the start",
			in:    lead + trail,
			width: 40,
			want:  "the quick",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := clipToMatch(tt.in, tt.width)
			assert.Contains(t, out, tt.want, "the match survives the clip")

			if tt.width > 0 {
				assert.LessOrEqual(t, lipgloss.Width(renderSnippet(tt.in, tt.width)), tt.width,
					"the rendered row fits the pane")
			}

			opens := strings.Count(out, transcriptstore.SnippetOpen)
			assert.Equal(t, opens, strings.Count(out, transcriptstore.SnippetClose),
				"every marker left in the snippet is paired")
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
			m.search = searchState{query: "ledger", hits: []searchHit{tt.hit}}
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
				hits:   tt.hits,
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
		hits:   hits("a"),
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
				hits:   hits("a", "b", "c"),
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
	for _, hit := range m.search.hits {
		snippets = append(snippets, hit.snippet)
	}
	assert.Equal(t, []string{"first", "third", "second"}, snippets, "an agent's hits sit together")

	view := ansi.Strip(m.searchView())
	assert.Equal(t, 2, strings.Count(view, "dispatcher"), "one header per agent rather than per name")
}

// TestSearchHeaderCarriesTheAgent covers a header wearing what the agent's own row wears, since
// the hit itself says nothing about the state the agent it came from is in.
func TestSearchHeaderCarriesTheAgent(t *testing.T) {
	tagged := "\u2713"

	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchState{query: "ledger"}
	m.listing.agents = []agentItem{
		{
			agent: agentstore.Agent{
				ID:         "a1",
				Name:       "billing",
				WorkflowID: "w1",
				State:      agentstore.StateThinking,
				Tag:        &tagged,
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

	view := ansi.Strip(m.searchView())

	live := headerLine(t, view, "billing")
	assert.Contains(t, live, stateIcon(agentstore.StateThinking), "the live header wears the state it is in")
	assert.Contains(t, live, tagged, "the live header wears its tag")
	assert.Contains(t, live, unreadMailIcon, "the live header says mail is waiting")

	archived := headerLine(t, view, "payouts")
	assert.Contains(t, archived, "(archived)", "the archived header still says so")
	assert.Contains(t, archived, stateIcon(agentstore.StateFailedToCleanup), "the archived header wears a failed cleanup")

	for _, header := range []string{live, archived} {
		assert.NotEqual(t, snippetIndent, indentOf(header), "a header is not drawn at the indent its snippets are")
	}
}

func indentOf(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// TestSearchHeaderWithoutTheAgent covers a hit named before its listing has landed, which is
// what a first paint looks like, and which is only allowed to cost the marks.
func TestSearchHeaderWithoutTheAgent(t *testing.T) {
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

	header := headerLine(t, ansi.Strip(m.searchView()), "billing")

	assert.Equal(t, "  "+noStateIcon+" billing", header, "the header keeps the icon column a header sits behind")
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

// TestJumpLandsOnTheMatch covers enter carrying the position the snippet came from, so the
// transcript opens where the match is rather than at the top of it.
func TestJumpLandsOnTheMatch(t *testing.T) {
	const turn = "the ledger is here"

	const previewWidth = 80

	transcript := strings.Repeat("filler\n\n", 40) + turn + "\n\n" + strings.Repeat("more\n\n", 40)
	offset := int64(strings.Index(transcript, turn))
	hitSpans := matchSpans(transcript, offset, int64(len(turn)), []string{"ledger"})
	_, matched := renderTranscript(transcript, hitSpans, previewWidth)
	wantLine := max(matched-transcriptPreviewBefore, 1)
	require.Greater(t, wantLine, 1, "the match has to sit far enough down to open on")

	tests := []struct {
		name string
		hit  searchHit
	}{
		{
			name: "a live hit",
			hit: searchHit{
				agentID: "a2",
				name:    "payouts",
				offset:  offset,
				length:  int64(len(turn)),
				snippet: marked("ledger"),
			},
		},
		{
			name: "an archived hit",
			hit: searchHit{
				agentID:  "a3",
				name:     "old-work",
				archived: true,
				offset:   offset,
				length:   int64(len(turn)),
				snippet:  marked("ledger"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{width: previewWidth, height: 40}
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
			m.archive.agents = []agentstore.Agent{{
				ID:         "a3",
				Name:       "old-work",
				WorkflowID: "w1",
			}}
			m.search = searchState{
				query: "ledger",
				hits:  []searchHit{tt.hit},
				preview: previewResult{
					agentID: tt.hit.agentID,
					query:   "ledger",
					content: transcript,
				},
			}

			updated, _ := m.route(keyMsg("enter"))
			_, ok := updated.(model)
			require.True(t, ok)

			staged := strings.Join(tmux.respawns, "\n")
			assert.Contains(t, staged, "weave-transcript.ansi", "the transcript is what the stage holds")
			assert.Contains(t, staged, fmt.Sprintf("+%d ", wantLine), "the pager opens on the match")
		})
	}
}

// TestMatchGivesTheStageBackWhenTheCursorMoves pins the landing being the hit's own row and
// nothing more, so the tree goes back to drawing what it always draws as soon as the cursor
// moves off it.
func TestMatchGivesTheStageBackWhenTheCursorMoves(t *testing.T) {
	tmux := &recordingTmux{}
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
	m.search = searchState{
		query: "ledger",
		hits: []searchHit{{
			agentID: "a2",
			name:    "payouts",
			offset:  4,
			length:  3,
			snippet: marked("two"),
		}},
		preview: previewResult{
			agentID: "a2",
			query:   "ledger",
			content: "one\ntwo\nthree\n",
		},
	}

	jumped, _ := m.route(keyMsg("enter"))
	landed, ok := jumped.(model)
	require.True(t, ok)
	require.Equal(t, "a2", landed.match.agentID)

	moved, _ := landed.route(keyMsg("k"))
	next, ok := moved.(model)
	require.True(t, ok)

	assert.Empty(t, next.match.agentID, "the transcript is let go of")
}

// TestMatchIsStagedOnce pins the landing surviving the refresh, which redraws the stage every
// second: a pager restarted under the reader cannot be scrolled or searched.
func TestMatchIsStagedOnce(t *testing.T) {
	tmux := &recordingTmux{}
	m := filterModel()
	m.tmux, m.popup = tmux, NewPopupWindow(tmux, "test-session")
	m.frame = layout.New(tmux, config.DefaultLayout())
	m.frame.Attach("%0")
	m.tree.cursor = 2
	m.listing.agents[1].alive = true
	m.match = matchFocus{
		agentID: "a2",
		content: "one\ntwo\nthree\n",
		spans:   []span{{start: 4, end: 7}},
	}

	for range 3 {
		m.stageSelection()
	}

	staged := 0
	for _, argv := range tmux.respawns {
		if strings.Contains(argv, "weave-transcript.ansi") {
			staged++
		}
	}

	assert.Equal(t, 1, staged, "the pager is left running across the passes that follow")
}

// TestMatchNeedsAPosition covers a turn the transcript does not hold, which spells none of the
// words: there is nothing there to take the stage away from the agent's own pane for.
func TestMatchNeedsAPosition(t *testing.T) {
	const content = "one\ntwo\nthree\n"

	tests := []struct {
		name   string
		offset int64
		want   []span
	}{
		{
			name:   "an offset the content holds",
			offset: 4,
			want:   []span{{start: 4, end: 7}},
		},
		{
			name:   "an offset past the end",
			offset: int64(len(content) + 1),
		},
		{
			name:   "an offset below zero",
			offset: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit := searchHit{
				agentID: "a2",
				offset:  tt.offset,
				length:  3,
				snippet: marked("two"),
			}
			got := matchFor(hit, previewResult{agentID: "a2", content: content})

			assert.Equal(t, tt.want, got.spans)
			if tt.want == nil {
				assert.Empty(t, got.agentID, "nothing is held on to")
			}
		})
	}
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

	m.search.cursor = len(m.search.hits) - 1
	bottom := drawnLines(m.searchView())
	assert.LessOrEqual(t, len(bottom), m.height, "the view stays inside the pane at the end of the list")
	assert.Contains(t, strings.Join(bottom, "\n"), "hit 19", "the hit under the cursor is drawn")
}

// drawnLines is the screen as View hands it to bubbletea, which trims the newline the last row
// carries before the pane is asked to hold it.
func drawnLines(view string) []string {
	return strings.Split(strings.TrimRight(ansi.Strip(view), "\n"), "\n")
}
