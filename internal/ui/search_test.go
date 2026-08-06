package ui

import (
	"context"
	"strings"
	"testing"

	"weave/config"

	agentstore "weave/internal/store/agent"
	"weave/internal/store/transcript"

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
			// An empty result blanks the preview, so the model needs its stage even
			// though nothing here draws.
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

// TestUpdateSearchEditsQuery covers the keys that type into the query. It avoids
// the nav and open keys, which stage a preview and so need tmux.
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
			name: "the nav keys are part of the query, since this screen moves on the arrows",
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
			in:          "before " + transcript.SnippetOpen + "match" + transcript.SnippetClose + " after",
			wantContain: "match",
		},
		{
			name:        "newlines are collapsed",
			in:          "first" + transcript.SnippetOpen + "match" + transcript.SnippetClose + "\nsecond",
			wantContain: "second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderSnippet(tt.in, 0)

			assert.Contains(t, out, tt.wantContain)
			assert.NotContains(t, out, transcript.SnippetOpen, "the markers are consumed")
			assert.NotContains(t, out, transcript.SnippetClose)
			assert.NotContains(t, out, "\n", "the snippet occupies one row")
		})
	}
}

// TestClipToMatch covers the clipping a result row depends on. The row gets one line
// of a fixed width, and the match can sit anywhere in the snippet, so what has to
// hold is that the match survives the cut and the row fits.
func TestClipToMatch(t *testing.T) {
	const (
		lead  = "the quick brown fox jumps over the lazy dog and keeps on running for a while "
		trail = " and then carries on well past where any pane could show it all"
	)

	marked := func(match string) string {
		return transcript.SnippetOpen + match + transcript.SnippetClose
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

			opens := strings.Count(out, transcript.SnippetOpen)
			assert.Equal(t, opens, strings.Count(out, transcript.SnippetClose),
				"every marker left in the snippet is paired")
		})
	}
}

// TestLineAtOffset covers where the transcript preview is anchored. Line zero means
// the top, which is where a preview with nothing to centre on belongs.
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

// hits builds search results from names alone, for the cases that only care
// which results survive.
func hits(names ...string) []searchHit {
	out := make([]searchHit, 0, len(names))
	for _, name := range names {
		out = append(out, searchHit{agentID: name, name: name, snippet: name})
	}

	return out
}

// TestJumpToSearchHit covers what enter on a result does. It goes to where the conversation
// already is rather than changing anything about it, so a live one lands on the agents tree and
// an archived one in the archive, and neither is resumed or taken out of the archive on the way.
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
			m.search = searchState{query: "ledger", hits: []searchHit{tt.hit}, typing: true}
			m.tree.agents[1].agent.Hidden = tt.hidden
			m.archive.agents = []agentstore.Agent{{ID: "a3", Name: "old-work", WorkflowID: "w1"}}

			updated, _ := m.route(keyMsg("enter"))
			got, ok := updated.(model)
			require.True(t, ok)

			require.Equal(t, tt.wantScreen, got.screen)
			assert.Nil(t, client.restored, "nothing is taken out of the archive")
			assert.Nil(t, client.resumed, "nothing is put back in a pane")

			rows, cursor := got.agentRows(), got.tree.cursor
			if got.screen == screenArchive {
				// The archive is listed only while its screen is up, so the row the jump named
				// arrives with the pass the entry asked for.
				got.applyArchive(got.archive.agents, nil, "")
				rows, cursor = got.archiveRows(), got.archive.cursor
			}

			require.Less(t, cursor, len(rows))
			assert.Equal(t, tt.wantCursor, rows[cursor].item.agent.Name)
		})
	}
}

// TestSearchFreeze covers esc on the results. It shuts the query line rather than leaving the
// screen, so the hits stay exactly as the query left them and the ordinary nav keys walk them.
// A second esc is the way out, and a query that found nothing leaves on the first, since there
// is nothing to walk.
func TestSearchFreeze(t *testing.T) {
	tests := []struct {
		name       string
		hits       []searchHit
		keys       []string
		wantScreen screen
		wantTyping bool
		wantCursor int
	}{
		{
			name:       "esc over nothing leaves the screen",
			keys:       []string{"esc"},
			wantScreen: screenAgents,
		},
		{
			name:       "esc over results shuts the query line and stays",
			hits:       hits("a", "b", "c"),
			keys:       []string{"esc"},
			wantScreen: screenSearch,
		},
		{
			name:       "the nav keys walk the frozen results",
			hits:       hits("a", "b", "c"),
			keys:       []string{"esc", "j", "j"},
			wantScreen: screenSearch,
			wantCursor: 2,
		},
		{
			name:       "the nav keys stop at the end rather than wrapping",
			hits:       hits("a", "b"),
			keys:       []string{"esc", "j", "j", "j"},
			wantScreen: screenSearch,
			wantCursor: 1,
		},
		{
			name:       "a second esc leaves",
			hits:       hits("a", "b"),
			keys:       []string{"esc", "esc"},
			wantScreen: screenAgents,
		},
		{
			name:       "the search key reopens the query line to edit it",
			hits:       hits("a", "b"),
			keys:       []string{"esc", "s"},
			wantScreen: screenSearch,
			wantTyping: true,
		},
		{
			name:       "a letter typed at frozen results acts rather than querying",
			hits:       hits("a", "b"),
			keys:       []string{"esc", "k"},
			wantScreen: screenSearch,
			wantCursor: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.api = api{agents: &fakeSearchAgents{}, workflows: emptyLists{}, schedules: emptyLists{}}
			m.screen = screenSearch
			m.search = searchState{query: "ledger", hits: tt.hits, typing: true}

			for _, k := range tt.keys {
				updated, _ := m.route(keyMsg(k))
				next, ok := updated.(model)
				require.True(t, ok)
				m = next
			}

			assert.Equal(t, tt.wantScreen, m.screen)
			assert.Equal(t, tt.wantTyping, m.search.typing)
			assert.Equal(t, tt.wantCursor, m.search.cursor)
			assert.Equal(t, tt.hits, m.search.hits, "the results are left as the query found them")
		})
	}
}

// TestSearchQueryOwnsTheKeys covers the query line taking every key while it is open, which is
// what keeps a letter bound elsewhere from acting instead of typing.
func TestSearchQueryOwnsTheKeys(t *testing.T) {
	tests := []struct {
		name         string
		typing       bool
		key          string
		wantQuery    string
		wantExpanded bool
	}{
		{
			name:      "the help key types while the line is open",
			typing:    true,
			key:       "?",
			wantQuery: "ledger?",
		},
		{
			name:         "the help key draws the legend once the line has shut",
			key:          "?",
			wantQuery:    "ledger",
			wantExpanded: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := filterModel()
			m.screen = screenSearch
			m.search = searchState{query: "ledger", hits: hits("a"), typing: tt.typing}

			updated, _ := m.route(keyMsg(tt.key))
			got, ok := updated.(model)
			require.True(t, ok)

			assert.Equal(t, tt.wantQuery, got.search.query)
			assert.Equal(t, tt.wantExpanded, got.helpExpanded)
		})
	}
}

// fakeSearchAgents records the two calls opening a hit can make. The embedded interface
// is nil, so anything else panics rather than quietly answering.
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
