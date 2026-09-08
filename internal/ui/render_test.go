package ui

import (
	"os"
	"path/filepath"
	"strconv"
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
	withColor(t)

	const content = "# A heading\n\n- a list item\n\nthe ledger is here\n"

	word := strings.Index(content, "ledger")
	rendered, line := renderTranscript(content, []span{{start: word, end: word + len("ledger")}}, 40)

	plain := ansi.Strip(rendered)
	assert.Contains(t, plain, "• a list item", "the markdown is rendered rather than shown as source")
	assert.NotContains(t, rendered, "\x02", "the markers are consumed into styling")
	assert.NotContains(t, rendered, "\x03", "the markers are consumed into styling")
	assert.Contains(t, rendered, stageMatchStyle.Render("ledger"), "the matched word is styled")

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

// TestRenderTranscriptEstimatesADroppedMatch covers a match inside a construct the rendering
// drops whole, which takes the marker with it: the preview still opens near where the match
// sat rather than at the top of a conversation that can run for thousands of lines.
func TestRenderTranscriptEstimatesADroppedMatch(t *testing.T) {
	head := strings.Repeat("filler paragraph\n\n", 60)
	content := head + "[the report](http://ledger.example.com)\n"

	word := strings.Index(content, "ledger")
	rendered, line := renderTranscript(content, []span{{start: word, end: word + len("ledger")}}, 40)

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
	rendered, _ := renderTranscript(content, []span{{start: word, end: word + len("author")}}, 60)

	code := strings.Index(rendered, "\x1b[")
	require.GreaterOrEqual(t, code, 0, "the renderer styles inline code")

	opened := rendered[code : code+strings.IndexByte(rendered[code:], 'm')+1]

	assert.Contains(t, rendered, stageMatchStyle.Render("author")+opened,
		"the code styling is opened again after the match")
}

// TestScrubMarks covers a transcript holding the search's own markers, which a captured pane
// can.
func TestScrubMarks(t *testing.T) {
	const content = "a \x02stray\x03 marker in the log"

	scrubbed := scrubMarks(content)

	assert.Equal(t, "a  stray  marker in the log", scrubbed)
	assert.Len(t, scrubbed, len(content), "the offsets the search reports still point where they did")
}

// TestStageFilePathIsThisPanelsOwn covers the name a screenful is written under, which carries
// the pid.
func TestStageFilePathIsThisPanelsOwn(t *testing.T) {
	path := stageFilePath("review")

	assert.Contains(t, filepath.Base(path), strconv.Itoa(os.Getpid()), "the panel that wrote it")
	assert.Equal(t, filepath.Join(os.TempDir(), filepath.Base(path)), path)
	assert.NotEqual(t, path, stageFilePath("transcript"), "two regions never share a file")
}

// TestWriteStageFileIsNeverTornInHalf covers the handover to the drawer, which reads the file
// whenever the panel taps a key and so can read one frame while the next is being written.
func TestWriteStageFileIsNeverTornInHalf(t *testing.T) {
	const frames = 200

	path := filepath.Join(t.TempDir(), "stage.ansi")
	tall := strings.Repeat("a tall frame\n", 400)
	short := "one row\n"

	require.True(t, writeStageFile(path, tall))

	written := 0
	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range frames {
			frame := tall
			if i%2 == 0 {
				frame = short
			}

			if !writeStageFile(path, frame) {
				return
			}

			written++
		}
	}()

	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}

		body, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Contains(t, []string{tall, short}, string(body), "a frame was read half written")
	}

	assert.Equal(t, frames, written, "every frame was written")
}
