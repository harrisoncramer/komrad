package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPagerArgv covers the command a pane runs, which carries the line to open on for a
// search preview and nothing for a record read from the top.
func TestPagerArgv(t *testing.T) {
	tests := []struct {
		name string
		line int
		want []string
	}{
		{
			name: "no line opens at the top",
			line: 0,
			want: []string{"less", "-R", "-X", "-Ps", "/tmp/stage"},
		},
		{
			name: "the first line is the top already",
			line: 1,
			want: []string{"less", "-R", "-X", "-Ps", "/tmp/stage"},
		},
		{
			name: "a line further down is where the pager opens",
			line: 42,
			want: []string{"less", "-R", "-X", "-Ps", "+42", "/tmp/stage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, pagerArgv("/tmp/stage", tt.line))
		})
	}
}

// TestMarkSpans covers the markers written into a transcript before it is rendered, which are
// what carry the match through a rendering that reflows the lines it sat on.
func TestMarkSpans(t *testing.T) {
	const content = "first\nsecond match\nthird match"

	at := func(word string) span {
		i := strings.Index(content, word)

		return span{start: i, end: i + len(word)}
	}

	tests := []struct {
		name  string
		spans []span
		want  string
	}{
		{
			name:  "nothing matched leaves the transcript alone",
			spans: nil,
			want:  content,
		},
		{
			name:  "the matched word is marked and the words around it are not",
			spans: []span{at("second")},
			want:  "first\n\x02second\x03 match\nthird match",
		},
		{
			name:  "every matched word takes a mark",
			spans: []span{at("first"), at("second")},
			want:  "\x02first\x03\n\x02second\x03 match\nthird match",
		},
		{
			name:  "a span past the end of the transcript marks nothing",
			spans: []span{{start: len(content) + 1, end: len(content) + 4}},
			want:  content,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, markSpans(content, tt.spans))
		})
	}
}

// TestRenderTranscript covers the preview being the markdown a conversation is written in
// rather than its source, with the matched word styled where the rendering put it.
func TestRenderTranscript(t *testing.T) {
	const content = "# A heading\n\n- a list item\n\nthe ledger is here\n"

	word := strings.Index(content, "ledger")
	rendered, line := renderTranscript(content, []span{{start: word, end: word + len("ledger")}}, 40)

	plain := ansi.Strip(rendered)
	assert.Contains(t, plain, "• a list item", "the markdown is rendered rather than shown as source")
	assert.NotContains(t, plain, "\x02", "the markers are consumed into styling")
	assert.Contains(t, rendered, matchStyle.Render("ledger"), "the matched word is styled")

	rows := strings.Split(plain, "\n")
	require.Positive(t, line)
	assert.Contains(t, rows[line-1], "ledger", "the line reported is the one the match landed on")
}

// TestRenderTranscriptWithoutAMatch covers a transcript staged with nothing marked in it, which
// is what a jump with no position left to it looks like.
func TestRenderTranscriptWithoutAMatch(t *testing.T) {
	rendered, line := renderTranscript("plain prose\n", nil, 40)

	assert.Equal(t, 0, line, "no match means no line to open on")
	assert.Contains(t, ansi.Strip(rendered), "plain prose")
}
