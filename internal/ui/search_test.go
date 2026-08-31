package ui

import (
	"context"
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

// TestLineAtOffset pins line zero meaning the top, which is where a preview with nothing to
// centre on belongs.
func TestLineAtOffset(t *testing.T) {
	const content = "line one\nline two has postgres in it\nline three"

	tests := []struct {
		name   string
		offset int64
		want   int
	}{
		{
			name:   "a negative offset anchors at the top",
			offset: -1,
			want:   0,
		},
		{
			name:   "an offset past the end anchors at the top",
			offset: int64(len(content) + 1),
			want:   0,
		},
		{
			name:   "the start of the transcript is its first line",
			offset: 0,
			want:   1,
		},
		{
			name:   "an offset partway through reports the line it falls on",
			offset: int64(strings.Index(content, "postgres")),
			want:   2,
		},
		{
			name:   "the last line is reached like any other",
			offset: int64(strings.Index(content, "line three")),
			want:   3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lineAtOffset(content, tt.offset))
		})
	}
}

func hits(names ...string) []searchHit {
	out := make([]searchHit, 0, len(names))
	for _, name := range names {
		out = append(out, searchHit{agentID: name, name: name, snippet: name})
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
			name:       "an archived hit lands in the archive",
			hit:        searchHit{agentID: "a3", name: "old-work", archived: true},
			wantScreen: screenArchive,
			wantCursor: "old-work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeSearchAgents{}
			m := filterModel()
			m.api = api{agents: client, workflows: emptyLists{}, schedules: emptyLists{}}
			m.screen = screenSearch
			m.search = searchState{query: "ledger", hits: []searchHit{tt.hit}}
			m.listing.agents[1].agent.Hidden = tt.hidden
			m.archive.agents = []agentstore.Agent{{ID: "a3", Name: "old-work", WorkflowID: "w1"}}

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
			m.api = api{agents: &fakeSearchAgents{}, workflows: emptyLists{}, schedules: emptyLists{}}
			m.screen = screenSearch
			m.search = searchState{typing: tt.typing, query: "ledger", hits: tt.hits}

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
	m.search = searchState{typing: true, query: "ledger", hits: hits("a")}

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
			m.api = api{agents: &fakeSearchAgents{}, workflows: emptyLists{}, schedules: emptyLists{}}
			m.screen = screenSearch
			m.search = searchState{typing: true, query: "ledger", hits: hits("a", "b", "c")}

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
		Agent: &agentv1.Agent{Id: req.Msg.GetId(), Name: "api-refactor", PaneId: "%9"},
	}), nil
}
