package ui

import (
	"strings"
	"testing"

	"claude-agents/internal/appconfig"

	"claude-agents/internal/store/transcript"

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
				screen:      screenSearch,
				searchQuery: tt.query,
				stage:       NewStage(&recordingTmux{}, appconfig.DefaultSplit),
			}

			m.applySearch(tt.msg)

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
