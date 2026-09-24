package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"weave/config"
	"weave/internal/ui/panel"

	"weave/internal/ui/layout"
	"weave/internal/ui/paint"
	"weave/internal/view"
	"weave/lib/xptr"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"

	tea "charm.land/bubbletea/v2"
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
				search: searchScreen{query: tt.query},
				frame:  layout.New(&recordingTmux{}, config.DefaultLayout()),
			}

			m.search.apply(&m, m.shared(), tt.msg)

			assert.Len(t, m.search.turns, tt.wantHits)
		})
	}
}

// TestSearchResultsReachTheScreenThatIsUp pins that the search's own messages arrive through
// the root's routing when the search is up, and go nowhere when another screen is.
func TestSearchResultsReachTheScreenThatIsUp(t *testing.T) {
	tests := []struct {
		name     string
		screen   screen
		wantHits int
	}{
		{
			name:     "on the search",
			screen:   screenSearch,
			wantHits: 2,
		},
		{
			name:     "on the agents",
			screen:   screenAgents,
			wantHits: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.screen = tt.screen
			m.search = searchScreen{query: "current"}

			next, _ := m.route(searchResult{query: "current", hits: hits("a", "b")})
			routed, ok := next.(model)
			require.True(t, ok)

			assert.Len(t, routed.search.turns, tt.wantHits)
		})
	}
}

// TestUpdateSearchEditsQuery covers the keys that type into the query, avoiding the nav and
// open keys, which stage a preview and so need tmux.
func TestUpdateSearchEditsQuery(t *testing.T) {
	tests := []struct {
		name string
		keys []tea.KeyPressMsg
		want string
	}{
		{
			name: "backspace on an empty query is harmless",
			keys: []tea.KeyPressMsg{
				keyMsg("backspace"),
			},
			want: "",
		},
		{
			name: "printable runes accumulate",
			keys: []tea.KeyPressMsg{
				keyMsg("po"),
				keyMsg("gr"),
			},
			want: "pogr",
		},
		{
			name: "space is part of the query rather than a command",
			keys: []tea.KeyPressMsg{
				keyMsg("po"),
				keyMsg("space"),
				keyMsg("gr"),
			},
			want: "po gr",
		},
		{
			name: "backspace drops the last character",
			keys: []tea.KeyPressMsg{
				keyMsg("post"),
				keyMsg("backspace"),
			},
			want: "pos",
		},
		{
			name: "the nav keys are part of the query while the line holds them",
			keys: []tea.KeyPressMsg{
				keyMsg("j"),
				keyMsg("k"),
			},
			want: "jk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{
				screen: screenSearch,
				keys:   config.DefaultKeybindings(),
				search: searchScreen{typing: true},
			}

			for _, key := range tt.keys {
				m.search.update(&m, m.shared(), key)
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
	return paint.MarkOpen + s + paint.MarkClose
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
			in:   "the " + paint.MarkOpen + "sandbox",
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
			m.search = searchScreen{query: "ledger", turns: []searchHit{tt.hit}}
			m.listing.agents[1].Agent.Hidden = tt.hidden
			m.listing.archivedAgents = archivedItems([]panel.Agent{{
				ID:         "a3",
				Name:       "old-work",
				WorkflowID: "w1",
			}})

			updated, _ := m.route(keyMsg("enter"))
			got, ok := updated.(model)
			require.True(t, ok)

			require.Equal(t, tt.wantScreen, got.screen)
			assert.Nil(t, client.restored, "nothing is taken out of the archive")
			assert.Nil(t, client.resumed, "nothing is put back in a pane")

			rows, cursor := got.agents.rows(got.shared()), got.agents.cursor
			if got.screen == screenArchive {
				rows, cursor = got.archive.listRows(got.shared()), got.archive.cursor
			}

			require.Less(t, cursor, len(rows))
			assert.Equal(t, tt.wantCursor, rows[cursor].item.Agent.Name)
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
			m.search = searchScreen{
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
	m.search = searchScreen{
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
			name:       "the bottom key lands on the last turn",
			keys:       []string{"enter", "G"},
			wantCursor: 5,
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
			m.search = searchScreen{
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

	response := &agentv1.RestoreAgentsResponse{Agents: xslice.Map(req.Msg.GetIds(), func(id string) *agentv1.Agent {
		return &agentv1.Agent{Id: id, Name: "api-refactor"}
	})}

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

// TestSearchNamesEachAgentOnce covers two agents sharing a name, which archived rows make
// ordinary, and the rank ordering scattering one agent's hits: an agent is one row however many
// of its turns said the query, and every one of those turns is drawn under it.
func TestSearchNamesEachAgentOnce(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchScreen{query: "ledger"}

	m.search.apply(&m, m.shared(), searchResult{
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

	agents := m.search.buildAgents(m.shared())
	require.Len(t, agents, 2, "one row per agent rather than per turn")
	assert.Equal(t, "a1", agents[0].id, "the agent the best turn belongs to stands first")
	assert.Equal(t, []string{"first", "third"}, xslice.Map(agents[0].turns, func(hit searchHit) string {
		return hit.snippet
	}), "an agent's turns sit together on its row")

	view := ansi.Strip(m.search.view(m.shared()))
	assert.Equal(t, 2, strings.Count(view, "dispatcher"), "one row per agent rather than per name")
	assert.Less(t, strings.Index(view, "first"), strings.Index(view, "third"), "what each turn said is drawn under its agent")
	assert.Less(t, strings.Index(view, "third"), strings.Index(view, "second"), "in the order the turns came back")
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

// TestSearchRowCarriesTheAgent covers a hit's row wearing what the agent's own row wears, which
// is the whole reason the two screens draw through the same package.
func TestSearchRowCarriesTheAgent(t *testing.T) {
	marked := "a"

	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchScreen{query: "ledger"}
	m.listing.marks = map[string]string{"a1": marked}
	m.listing.agents = []panel.AgentRow{
		{
			Agent: panel.Agent{
				ID:         "a1",
				Name:       "billing",
				WorkflowID: "w1",
				State:      panel.StateThinking,
			},
			Alive:      true,
			UnreadMail: true,
		},
	}
	m.listing.archivedAgents = archivedItems([]panel.Agent{
		{
			ID:         "a2",
			Name:       "payouts",
			WorkflowID: "w1",
			State:      panel.StateIdle,
			ArchivedAt: xptr.Ptr(time.Now()),
		},
	})

	m.search.apply(&m, m.shared(), searchResult{
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

	rows, drawn := searchRows(m)

	live := searchRowNamed(t, rows, "billing")
	assert.Equal(t, view.StatusThinking, live.Status, "the live row wears the state it is in")
	assert.Equal(t, view.ProcessAlive, live.Process, "and has a process behind it")
	assert.Equal(t, marked, live.Mark, "the live row wears its mark")
	assert.True(t, live.UnreadMail, "the live row says mail is waiting")

	archived := searchRowNamed(t, rows, "payouts")
	assert.True(t, archived.Archived, "the archived row says so")
	assert.Equal(t, view.ProcessStopped, archived.Process, "and says a process was there and has stopped")

	// Process decides the bar on its own, so the two rows are compared as they are drawn rather
	// than by a field the renderer would never read.
	assert.Contains(t, headerLine(t, strings.Join(ansiStripped(drawn), "\n"), "payouts"),
		view.Spell(view.GlyphQuiet), "the archived row draws the quiet bar")
	assert.Contains(t, headerLine(t, strings.Join(ansiStripped(drawn), "\n"), "billing"),
		view.Spell(view.GlyphBusy), "while the thinking one draws a live bar")
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

// treeOwned blanks the fields a row carries because of where it sits in a list rather than
// because of the agent behind it, so what is left is what both screens read off that agent.
func treeOwned(row view.Row) view.Row {
	row.Key, row.Kind, row.Name = "", 0, ""
	row.Matches, row.Mark = nil, ""
	row.Repo, row.Review, row.Forge = 0, 0, 0
	row.Selected, row.HiddenCount = false, 0

	return row
}

// TestSearchRowSaysWhatTheAgentsListSays pins one agent read by both screens saying the same
// thing, every field the helper above does not blank being compared.
func TestSearchRowSaysWhatTheAgentsListSays(t *testing.T) {
	item := panel.AgentRow{
		Agent: panel.Agent{
			ID:         "a1",
			Name:       "ledger",
			WorkflowID: "w1",
			State:      panel.StateWaiting,
			Hidden:     true,
			HeldAt:     xptr.Ptr(time.Now()),
			ScheduleID: xptr.Ptr("s1"),
		},
		Alive:      true,
		UnreadMail: true,
		FailedRun:  true,
	}

	m := filterModel()
	m.screen = screenSearch
	m.search = searchScreen{query: "ledger"}
	m.listing.agents = []panel.AgentRow{item}

	hit := m.search.agentRow(m.shared(), searchAgent{id: "a1", name: "ledger"}, nil)
	tree := snapshotRow(agentRow{item: item}, snapshotParams{})

	assert.Equal(t, treeOwned(tree), treeOwned(hit))
}

// TestArchivedAgentsSayTheSameOnBothScreens pins one archived agent drawing alike on the two
// screens that can both name it, the rule living where the archive already kept it.
func TestArchivedAgentsSayTheSameOnBothScreens(t *testing.T) {
	states := []panel.State{
		panel.StateIdle,
		panel.StateFailedToCreate,
		panel.StateFailedToCleanup,
	}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			agent := panel.Agent{
				ID:         "a2",
				Name:       "payouts",
				WorkflowID: "w1",
				State:      state,
				ArchivedAt: xptr.Ptr(time.Now()),
			}

			m := filterModel()
			m.screen = screenSearch
			m.search = searchScreen{query: "payouts"}
			m.listing.archivedAgents = archivedItems([]panel.Agent{agent})

			archive := view.StatusCell(snapshotRow(agentRow{item: panel.AgentRow{Agent: agent}}, snapshotParams{}))
			search := view.StatusCell(m.search.agentRow(m.shared(), searchAgent{
				id:       "a2",
				name:     "payouts",
				archived: true,
			}, archivedByID(m.listing.archivedAgents)))

			assert.Equal(t, archive, search, "the two screens draw one agent the same")
		})
	}
}

// TestArchivedHitGreysOnWhatTheServerSaid covers the two archived hits the panel holds no stamp
// for, the greying and the tag resting on the flag the search came back with.
func TestArchivedHitGreysOnWhatTheServerSaid(t *testing.T) {
	tests := []struct {
		name   string
		listed []panel.Agent
	}{
		{
			name:   "the archive listing has not arrived",
			listed: nil,
		},
		{
			// A row archived before the stamp existed carries no ArchivedAt, so the facts read off
			// it say nothing about being archived.
			name: "the listing arrived carrying no stamp",
			listed: []panel.Agent{{
				ID:         "a2",
				Name:       "payouts",
				WorkflowID: "w1",
				State:      panel.StateIdle,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.screen = screenSearch
			m.search = searchScreen{query: "payouts"}
			m.listing.archivedAgents = archivedItems(tt.listed)

			row := m.search.agentRow(m.shared(), searchAgent{
				id:       "a2",
				name:     "payouts",
				archived: true,
			}, archivedByID(m.listing.archivedAgents))

			assert.True(t, row.Archived, "the server said it is archived, so it greys")
			assert.Equal(t, view.ProcessStopped, row.Process, "and it is over, whatever else is unknown")
		})
	}
}

// TestArchivedSearchRowKeepsTheDeleteThatFailed is the carve-out the archive already makes and
// the search screen now inherits, an agent a delete could not destroy still having a worktree
// somebody has to deal with.
func TestArchivedSearchRowKeepsTheDeleteThatFailed(t *testing.T) {
	agent := panel.Agent{
		ID:         "a2",
		Name:       "stuck",
		WorkflowID: "w1",
		State:      panel.StateFailedToCleanup,
		ArchivedAt: xptr.Ptr(time.Now()),
	}

	m := filterModel()
	m.screen = screenSearch
	m.search = searchScreen{query: "stuck"}
	m.listing.archivedAgents = archivedItems([]panel.Agent{agent})

	cell := view.StatusCell(m.search.agentRow(m.shared(), searchAgent{
		id:       "a2",
		name:     "stuck",
		archived: true,
	}, archivedByID(m.listing.archivedAgents)))

	assert.Equal(t, view.GlyphBusy, cell.Glyph, "a failed delete is not quietened by being archived")
	assert.Equal(t, paint.ToneDanger, cell.Tone)
}

// TestSearchNamesAnAgentMatchedTwiceOnce covers one agent matched by its name and by a turn of
// its conversation, which is one row carrying that turn rather than one row per way it matched.
func TestSearchNamesAnAgentMatchedTwiceOnce(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.search = searchScreen{query: "billing"}
	m.listing.agents = []panel.AgentRow{{
		Agent: panel.Agent{
			ID:         "a1",
			Name:       "billing",
			WorkflowID: "w1",
		},
		Alive: true,
	}}

	m.search.apply(&m, m.shared(), searchResult{
		query: "billing",
		hits: []searchHit{{
			agentID: "a1",
			name:    "billing",
			snippet: "the billing ledger",
		}},
	})

	agents := m.search.buildAgents(m.shared())
	require.Len(t, agents, 1, "one agent, matched by its name and by a turn, is one row")
	assert.Len(t, agents[0].turns, 1, "and the turn is kept on it")

	rows, drawn := searchRows(m)
	require.Len(t, rows, 2)
	assert.Equal(t, "a1", rows[0].Key, "the row answers to the agent")
	assert.NotEmpty(t, rows[0].Matches, "and lights the name the query said")
	assert.Equal(t, 0, caretColumn(drawn[0]), "the row wears the caret in its focus cell")
	assert.Equal(t, "a1/0", rows[1].Key, "the turn hangs under it")
	assert.Equal(t, view.KindHit, rows[1].Kind)
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
	m.search = searchScreen{query: "ledger"}
	m.listing.agents = nil

	m.search.apply(&m, m.shared(), searchResult{
		query: "ledger",
		hits: []searchHit{{
			agentID: "gone",
			name:    "billing",
			snippet: "first",
		}},
	})

	rows, _ := searchRows(m)
	row := searchRowNamed(t, rows, "billing")

	assert.Equal(t, view.ProcessNone, row.Process, "a hit nothing is known about has nothing behind it")

	header := headerLine(t, ansi.Strip(m.search.view(m.shared())), "billing")
	assert.True(t, strings.HasSuffix(header, view.Spell(view.GlyphQuiet)+" billing"),
		"and still draws the quiet bar: %q", header)
	assert.Equal(t, view.GutterColumns(view.NoColumns()), textColumn(t, header, "billing"),
		"and opens its name where every other row does")
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
			m.listing.agents[1].Alive = true
			m.listing.agents[1].Agent.Pane = "%9"
			m.listing.archivedAgents = archivedItems([]panel.Agent{{
				ID:         "a3",
				Name:       "old-work",
				WorkflowID: "w1",
			}})
			m.search = searchScreen{query: "ledger", turns: []searchHit{tt.hit}}

			updated, _ := m.route(keyMsg("enter"))
			landed, ok := updated.(model)
			require.True(t, ok)

			assert.Empty(t, landed.search.preview.conversation, "the preview does not follow the jump")
			assert.Equal(t, tt.wantSolo, landed.frame.IsSolo(), "the window goes to whatever the screen jumped to needs")
			assert.Equal(t, tt.wantSwaps, tmux.swaps, "the row's own stage is what is drawn")
			if tt.wantDrawn != "" {
				assert.Contains(t, ansi.Strip(landed.render()), tt.wantDrawn, "the row's own record is what is drawn beside the list")
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
	m.listing.agents[1].Alive = true
	m.listing.agents[1].Agent.Pane = "%9"
	m.search = searchScreen{
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
	landed.search.applyPreview(landed.shared(), late)

	assert.Empty(t, landed.search.preview.conversation, "the late preview is dropped")
}

// TestSearchViewFitsThePane covers a common query returning more hits than the pane is tall,
// which used to be drawn whole and scroll the panel away.
func TestSearchViewFitsThePane(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.height = 14
	m.search = searchScreen{query: "agent"}

	var found []searchHit
	for i := range 20 {
		found = append(found, searchHit{
			agentID: fmt.Sprintf("a%d", i),
			name:    fmt.Sprintf("agent-%d", i),
			snippet: fmt.Sprintf("hit %d", i),
		})
	}

	m.search.apply(&m, m.shared(), searchResult{query: "agent", hits: found})

	view := drawnLines(m.search.view(m.shared()))
	assert.LessOrEqual(t, len(view), m.height, "the view is no taller than the pane")
	assert.Contains(t, strings.Join(view, "\n"), "more", "the rows it could not draw are counted")

	m.search.cursor = len(m.search.rows(m.shared())) - 1
	bottom := drawnLines(m.search.view(m.shared()))
	assert.LessOrEqual(t, len(bottom), m.height, "the view stays inside the pane at the end of the list")
	assert.Contains(t, strings.Join(bottom, "\n"), "agent-19", "the agent under the cursor is drawn")
}

// drawnLines is the screen as View hands it to bubbletea, which trims the newline the last row
// carries before the pane is asked to hold it.
func drawnLines(view string) []string {
	return strings.Split(strings.TrimRight(ansi.Strip(view), "\n"), "\n")
}

// TestSearchNamesStandFirst covers the agents whose own name says the query, which are read off
// the listings and drawn above the agents whose conversation said it.
func TestSearchNamesStandFirst(t *testing.T) {
	m := filterModel()
	m.screen = screenSearch
	m.width = 80
	m.listing.archivedAgents = archivedItems([]panel.Agent{{
		ID:         "a3",
		Name:       "billing-old",
		WorkflowID: "w1",
	}})
	m.search = searchScreen{query: "BILL"}

	m.search.apply(&m, m.shared(), searchResult{
		query: "BILL",
		hits: []searchHit{{
			agentID: "a2",
			name:    "payouts",
			snippet: "bill",
		}},
	})

	agents := m.search.buildAgents(m.shared())
	require.Len(t, agents, 3)
	assert.Equal(t, []string{"a1", "a3", "a2"}, xslice.Map(agents, func(agent searchAgent) string { return agent.id }),
		"the live name, then the archived one, then the conversation")
	assert.True(t, agents[1].archived, "an archived agent is named as archived")
	assert.Empty(t, agents[0].turns, "an agent found by its name alone has no turn to open on")

	rows, drawn := searchRows(m)
	require.Len(t, rows, 4)
	assert.NotEmpty(t, rows[0].Matches, "the name that said the query is lit")
	assert.Empty(t, rows[2].Matches, "a name that did not is not")
	assert.Equal(t, view.KindHit, rows[3].Kind, "and its turn hangs under it")
	assert.Contains(t, ansi.Strip(drawn[m.search.cursor]), "billing", "the cursor opens on the first name")
}

// TestSearchNameHitsTakeTheCursor covers the cursor running from the names into the
// conversations, the two being one list to walk.
func TestSearchNameHitsTakeTheCursor(t *testing.T) {
	m := filterModel()
	m.api = api{
		agents:    &fakeSearchAgents{},
		workflows: emptyLists{},
		schedules: emptyLists{},
	}
	m.screen = screenSearch
	m.width = 80
	m.search = searchScreen{query: "bill"}

	m.search.apply(&m, m.shared(), searchResult{
		query: "bill",
		hits: []searchHit{{
			agentID: "a2",
			name:    "payouts",
			snippet: "bill",
		}},
	})

	opened, ok := m.search.hoveredAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "a1", opened.id, "the cursor opens on the name")

	m.search.list().move(&m, m.shared(), 1)

	agent, ok := m.search.hoveredAgent(m.shared())
	require.True(t, ok)
	assert.Equal(t, "a2", agent.id, "the step off the name lands on the conversation")
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
	m.search = searchScreen{query: "payout"}

	m.search.jumpToAgent(&m, m.shared())
	landed := m

	assert.Equal(t, screenAgents, landed.screen)
	require.Less(t, landed.agents.cursor, len(rowNames(landed.agents.rows(landed.shared()))))
	assert.Equal(t, "payouts", rowNames(landed.agents.rows(landed.shared()))[landed.agents.cursor])
}

// TestNameMatches covers where a name is marked, which is the query as the name spells it rather
// than as it was typed.
func TestNameMatches(t *testing.T) {
	assert.Equal(t, []view.Span{{Start: 0, End: 4}}, nameMatches("biLLing", " bill "))
	assert.Nil(t, nameMatches("billing", "  "))
	assert.Nil(t, nameMatches("billing", "payouts"))
}

// TestSearchCursorSurvivesARefresh covers the names among the rows being read off the listings:
// a refresh that changes them moves the rows the cursor is counting, and used to leave it
// standing on another agent or off the end of the list.
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
		m.search = searchScreen{query: "bill"}
		m.search.apply(&m, m.shared(), searchResult{
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

		cmd := applyListing(&m, append([]panel.AgentRow{
			{Agent: panel.Agent{
				ID:         "a4",
				Name:       "billing-two",
				WorkflowID: "w1",
			}},
		}, m.listing.agents...), m.listing.workflows)

		agent, ok := m.search.hoveredAgent(m.shared())
		require.True(t, ok)
		assert.Equal(t, "a2", agent.id, "the cursor stays on the agent it was standing on")
		assert.Nil(t, cmd, "the preview beside it is the one already fetched")
	})

	t.Run("the hit under the cursor going", func(t *testing.T) {
		m := sectioned()
		m.search.cursor = 0

		cmd := applyListing(&m, nil, m.listing.workflows)

		agent, ok := m.search.hoveredAgent(m.shared())
		require.True(t, ok, "a row still wears the cursor")
		assert.Equal(t, "a2", agent.id, "the cursor lands on what is left")
		assert.NotNil(t, cmd, "the preview is fetched for the agent it landed on")
	})
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

// TestSearchHitRowFollowsItsAgent covers the row a turn is drawn as: it hangs under its agent
// and greys with it, and never carries a process of its own.
func TestSearchHitRowFollowsItsAgent(t *testing.T) {
	hit := searchHit{
		agentID: "a1",
		role:    "assistant",
		seq:     1,
		snippet: "Hey. Ready. What " + marked("need") + "?",
	}

	row := searchHitRow(searchRow{agent: searchAgent{id: "a1", archived: true}, hit: &hit})

	assert.Equal(t, "a1/1", row.Key)
	assert.Equal(t, view.KindHit, row.Kind)
	assert.Equal(t, "agent #1", row.Lead)
	assert.Equal(t, "Hey. Ready. What need?", row.Name)
	assert.Equal(t, []view.Span{{Start: 17, End: 21}}, row.Matches)
	assert.True(t, row.Archived)
	assert.Equal(t, view.ProcessNone, row.Process)
}

// searchHelp is the search legend as the frame draws it.
func (m model) searchHelp() keyMap {
	return m.search.help(m.shared())
}
