package paint

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarkSpans covers the markers written into a transcript before it is rendered, which are
// what carry the match through a rendering that reflows the lines it sat on.
func TestMarkSpans(t *testing.T) {
	const content = "first\nsecond match\nthird match"

	at := func(word string) Span {
		i := strings.Index(content, word)

		return Span{Start: i, End: i + len(word)}
	}

	tests := []struct {
		name  string
		spans []Span
		want  string
	}{
		{
			name:  "nothing matched leaves the transcript alone",
			spans: nil,
			want:  content,
		},
		{
			name:  "the matched word is marked and the words around it are not",
			spans: []Span{at("second")},
			want:  "first\n\x02second\x03 match\nthird match",
		},
		{
			name:  "every matched word takes a mark",
			spans: []Span{at("first"), at("second")},
			want:  "\x02first\x03\n\x02second\x03 match\nthird match",
		},
		{
			name:  "a span past the end of the transcript marks nothing",
			spans: []Span{{Start: len(content) + 1, End: len(content) + 4}},
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
	withColor(t)

	const content = "# A heading\n\n- a list item\n\nthe ledger is here\n"

	word := strings.Index(content, "ledger")
	rendered, line := Colored.RenderTranscript(content, []Span{{Start: word, End: word + len("ledger")}}, 40)

	plain := ansi.Strip(rendered)
	assert.Contains(t, plain, "• a list item", "the markdown is rendered rather than shown as source")
	assert.NotContains(t, rendered, "\x02", "the markers are consumed into styling")
	assert.NotContains(t, rendered, "\x03", "the markers are consumed into styling")
	assert.Contains(t, rendered, Colored.stageMatch().Render("ledger"), "the matched word is styled")

	rows := strings.Split(plain, "\n")
	require.Positive(t, line)
	assert.Contains(t, rows[line-1], "ledger", "the line reported is the one the match landed on")
}

// TestRenderTranscriptWithoutAMatch covers a transcript staged with nothing marked in it, which
// is what a jump with no position left to it looks like.
func TestRenderTranscriptWithoutAMatch(t *testing.T) {
	rendered, line := Colored.RenderTranscript("plain prose\n", nil, 40)

	assert.Equal(t, 0, line, "no match means no line to open on")
	assert.Contains(t, ansi.Strip(rendered), "plain prose")
}

// TestRenderTranscriptEstimatesADroppedMatch covers a match inside a construct the rendering
// drops whole, which takes the marker with it: the preview still opens near where the match
// sat rather than at the top of a conversation that can run for thousands of lines.
func TestRenderTranscriptEstimatesADroppedMatch(t *testing.T) {
	head := strings.Repeat("filler paragraph\n\n", 60)
	content := head + "[the report](http://ledger.example.com)\n"

	word := strings.Index(content, "ledger")
	rendered, line := Colored.RenderTranscript(content, []Span{{Start: word, End: word + len("ledger")}}, 40)

	rows := strings.Count(rendered, "\n") + 1

	assert.NotContains(t, ansi.Strip(rendered), "ledger", "the rendering dropped the destination the match sat in")
	assert.Greater(t, line, rows/2, "the preview opens down where the match sat")
	assert.LessOrEqual(t, line, rows)
}

// TestRenderTranscriptKeepsTheStylingAroundAMatch covers a match inside a run the renderer
// styled, inline code being where an identifier is matched.
func TestRenderTranscriptKeepsTheStylingAroundAMatch(t *testing.T) {
	withColor(t)

	const content = "the `author_kind` column\n"

	word := strings.Index(content, "author")
	rendered, _ := Colored.RenderTranscript(content, []Span{{Start: word, End: word + len("author")}}, 60)

	code := strings.Index(rendered, "\x1b[")
	require.GreaterOrEqual(t, code, 0, "the renderer styles inline code")

	opened := rendered[code : code+strings.IndexByte(rendered[code:], 'm')+1]

	assert.Contains(t, rendered, Colored.stageMatch().Render("author")+opened,
		"the code styling is opened again after the match")
}

// TestScrubMarks covers a transcript holding the search's own markers, which a captured pane
// can.
func TestScrubMarks(t *testing.T) {
	const content = "a \x02stray\x03 marker in the log"

	scrubbed := ScrubMarks(content)

	assert.Equal(t, "a  stray  marker in the log", scrubbed)
	assert.Len(t, scrubbed, len(content), "the offsets the search reports still point where they did")
}

// TestDocRowsFitTheColumn covers a message carrying an indented code block, whose tabs the
// terminal draws at a width and the panel's own measuring does not, and one carrying a token
// too long to break on a word, which the word wrap leaves wider than the column.
func TestDocRowsFitTheColumn(t *testing.T) {
	bodies := map[string]string{
		"a code block held with tabs": "Here is the queue:\n\n    125:\t\t\tqueues.SupervisorReconciliation: {\n    135:\t\t\t\t\tGlobalLimit: 1,\n\nSo it is there.\n",
		"a link nothing will break":   "The run is at https://github.com/chariot-giving/weave/actions/runs/1234567890/job/9876543210 and it failed.\n",
	}

	for name, body := range bodies {
		for _, width := range []int{40, 50, 60, 80} {
			t.Run(name+" at "+strconv.Itoa(width), func(t *testing.T) {
				for _, row := range Colored.DocRows(body, width) {
					plain := ansi.Strip(row)

					assert.NotContains(t, plain, "\t", "a row holds no tab for the terminal to widen")
					assert.LessOrEqual(t, ansi.StringWidth(row), width, "the row fits its column: %q", plain)
				}
			})
		}
	}
}

// TestExpandTabs covers the stops the tabs are taken to, counted from the start of each line.
func TestExpandTabs(t *testing.T) {
	assert.Equal(t, "    ", expandTabs("\t"))
	assert.Equal(t, "ab  cd", expandTabs("ab\tcd"))
	assert.Equal(t, "abc d", expandTabs("abc\td"))
	assert.Equal(t, "a   \nbc  d", expandTabs("a\t\nbc\td"))
	assert.Equal(t, "no tabs here", expandTabs("no tabs here"))
}
