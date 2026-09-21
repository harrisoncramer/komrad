package doctor

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// answering is a terminal that replies to every cursor query with the columns given, in order,
// as a real one would once the sample has been drawn.
type answering struct {
	replies []string
	written strings.Builder
	pending *io.PipeWriter
	reader  *io.PipeReader
	sent    int
}

func newAnswering(replies ...string) *answering {
	reader, writer := io.Pipe()

	return &answering{
		replies: replies,
		pending: writer,
		reader:  reader,
	}
}

// Write takes what the probe drew and answers the cursor query inside it.
func (a *answering) Write(payload []byte) (int, error) {
	a.written.Write(payload)

	if !strings.Contains(string(payload), "\x1b[6n") {
		return len(payload), nil
	}

	reply := "\x1b[1;1R"
	if a.sent < len(a.replies) {
		reply = a.replies[a.sent]
	}
	a.sent++

	go func() { _, _ = a.pending.Write([]byte(reply)) }()

	return len(payload), nil
}

// TestMeasureColumnsReadsTheColumnBack covers the whole exchange, a column of n meaning the
// sample took n-1 columns because it was drawn from the first.
func TestMeasureColumnsReadsTheColumnBack(t *testing.T) {
	terminal := newAnswering("\x1b[7;2R", "\x1b[7;3R", "\x1b[7;4R")

	columns, err := measureColumns(t.Context(), terminal.reader, terminal, []string{"x", "漢", "wide"})
	require.NoError(t, err)

	assert.Equal(t, []int{1, 2, 3}, columns)
	assert.Contains(t, terminal.written.String(), "\r\x1b[Kx\x1b[6n",
		"each sample is drawn from the first column so a long one cannot wrap into the answer")
}

// TestMeasureColumnsIgnoresWhatWasTypedFirst covers a person pressing a key while the query is
// in flight, which arrives in the same read as the answer.
func TestMeasureColumnsIgnoresWhatWasTypedFirst(t *testing.T) {
	terminal := newAnswering("hello\x1b[3;3R")

	columns, err := measureColumns(t.Context(), terminal.reader, terminal, []string{"漢"})
	require.NoError(t, err)

	assert.Equal(t, []int{2}, columns)
}

// TestMeasureColumnsGivesUpOnSilence covers the terminal that answers nothing.
func TestMeasureColumnsGivesUpOnSilence(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()

	failed := make(chan error, 1)
	go func() {
		_, err := measureColumns(context.WithoutCancel(t.Context()), reader, io.Discard, []string{"x"})
		failed <- err
	}()

	select {
	case err := <-failed:
		require.ErrorIs(t, err, errCursorQueryUnanswered)
	case <-time.After(5 * time.Second):
		t.Fatal("a terminal that will not answer has to be given up on, not waited out")
	}
}

// TestMeasureColumnsStopsWhenTheContextDoes covers a cancelled doctor run, which must not be
// held open by a read of a terminal.
func TestMeasureColumnsStopsWhenTheContextDoes(t *testing.T) {
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := measureColumns(ctx, reader, io.Discard, []string{"x"})
	assert.ErrorIs(t, err, errCursorQueryUnanswered)
}

// TestMeasureColumnsRefusesAnAnswerItCannotRead covers a reply that is shaped like one and is
// not, which must be reported rather than turned into a width.
func TestMeasureColumnsRefusesAnAnswerItCannotRead(t *testing.T) {
	terminal := newAnswering("\x1b[7R")

	_, err := measureColumns(t.Context(), terminal.reader, terminal, []string{"x"})
	assert.ErrorIs(t, err, errCursorQueryUnanswered)
}

// TestParseCursorReply covers the shapes a reply arrives in.
func TestParseCursorReply(t *testing.T) {
	for _, tt := range []struct {
		name   string
		buffer string
		column int
		found  bool
		fails  bool
	}{
		{
			name:   "no column in it",
			buffer: "\x1b[12R",
			fails:  true,
		},
		{
			name:   "a column of zero",
			buffer: "\x1b[12;0R",
			fails:  true,
		},
		{
			name:   "nothing yet",
			buffer: "",
		},
		{
			name:   "half a reply",
			buffer: "\x1b[12;3",
		},
		{
			name:   "a whole reply",
			buffer: "\x1b[12;34R",
			column: 34,
			found:  true,
		},
		{
			name:   "a keypress before it",
			buffer: "q\x1b[1;9R",
			column: 9,
			found:  true,
		},
		{
			name:   "another escape before it",
			buffer: "\x1b[A\x1b[1;9R",
			column: 9,
			found:  true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			column, found, err := parseCursorReply([]byte(tt.buffer))
			if tt.fails {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.found, found)
			assert.Equal(t, tt.column, column)
		})
	}
}
