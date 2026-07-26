package ui

import (
	"testing"

	"claude-agents/internal/appconfig"

	"claude-agents/internal/store/transcript"

	tea "github.com/charmbracelet/bubbletea"
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
		msg      searchResultsMsg
		wantHits int
	}{
		{
			name:  "a response for a query the input has moved past",
			query: "current",
			msg: searchResultsMsg{
				query: "stale",
				hits:  hits("a", "b"),
			},
			wantHits: 0,
		},
		{
			name:  "a response for the query on screen",
			query: "current",
			msg: searchResultsMsg{
				query: "current",
				hits:  hits("a", "b"),
			},
			wantHits: 2,
		},
		{
			name:  "a query that matched nothing clears the list",
			query: "current",
			msg: searchResultsMsg{
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
				screen:      screenSearch,
				searchQuery: tt.query,
				stage:       NewStage(&recordingTmux{}),
			}

			m.applySearchResults(tt.msg)

			assert.Len(t, m.searchHits, tt.wantHits)
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := model{screen: screenSearch, keys: appconfig.DefaultKeybindings()}

			for _, key := range tt.keys {
				next, _ := m.updateSearch(key)
				updated, ok := next.(model)
				require.True(t, ok)
				m = updated
			}

			assert.Equal(t, tt.want, m.searchQuery)
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
			out := renderSnippet(tt.in)

			assert.Contains(t, out, tt.wantContain)
			assert.NotContains(t, out, transcript.SnippetOpen, "the markers are consumed")
			assert.NotContains(t, out, transcript.SnippetClose)
			assert.NotContains(t, out, "\n", "the snippet occupies one row")
		})
	}
}

// TestFirstMatchLine covers where the transcript preview is anchored. Line zero
// means the top, which is where a preview with nothing to centre on belongs.
func TestFirstMatchLine(t *testing.T) {
	const content = "line one\nline two has postgres in it\nline three"

	tests := []struct {
		name  string
		query string
		want  int
	}{
		{
			name:  "a blank query anchors at the top",
			query: "  ",
			want:  0,
		},
		{
			name:  "a query that never occurs anchors at the top",
			query: "sqlite",
			want:  0,
		},
		{
			name:  "a match reports its line",
			query: "postgres",
			want:  2,
		},
		{
			name:  "matching ignores case, since the search does",
			query: "POSTGRES",
			want:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, firstMatchLine(content, tt.query))
		})
	}
}

// hits builds search results from names alone, for the cases that only care
// which results survive.
func hits(names ...string) []searchHit {
	out := make([]searchHit, 0, len(names))
	for _, name := range names {
		out = append(out, searchHit{sessionID: name, name: name, snippet: name})
	}

	return out
}
