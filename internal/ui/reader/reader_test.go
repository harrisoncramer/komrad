package reader_test

import (
	"regexp"
	"testing"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"

	"github.com/stretchr/testify/assert"
)

// TestWindowSpan covers the rows a window draws around its markers.
func TestWindowSpan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		window    reader.Window
		height    int
		wantFirst int
		wantEnd   int
	}{
		{name: "a document shorter than the window is drawn whole", window: reader.Window{Len: 3, Top: 2, Markers: true}, height: 5, wantFirst: 0, wantEnd: 3},
		{name: "the top of a long document leaves a row for the marker under it", window: reader.Window{Len: 20, Markers: true}, height: 5, wantFirst: 0, wantEnd: 4},
		{name: "the middle leaves a row for a marker either side", window: reader.Window{Len: 20, Top: 6, Markers: true}, height: 5, wantFirst: 6, wantEnd: 9},
		{name: "the bottom leaves a row for the marker over it", window: reader.Window{Len: 20, Top: 99, Markers: true}, height: 5, wantFirst: 16, wantEnd: 20},
		{name: "without markers every row is text", window: reader.Window{Len: 20, Top: 6}, height: 5, wantFirst: 6, wantEnd: 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			first, end := tt.window.Span(tt.height)
			assert.Equal(t, tt.wantFirst, first)
			assert.Equal(t, tt.wantEnd, end)
		})
	}
}

// TestWindowReveal covers a window moved just far enough to draw a row.
func TestWindowReveal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		window reader.Window
		row    int
		height int
		want   int
	}{
		{name: "a row already drawn leaves the window alone", window: reader.Window{Len: 20, Top: 6, Markers: true}, row: 7, height: 5, want: 6},
		{name: "a row above brings the window up to it", window: reader.Window{Len: 20, Top: 6, Markers: true}, row: 2, height: 5, want: 2},
		{name: "a row below brings the window down just far enough", window: reader.Window{Len: 20, Markers: true}, row: 10, height: 5, want: 8},
		{name: "the last row lands the window on the bottom", window: reader.Window{Len: 20, Markers: true}, row: 19, height: 5, want: 16},
		{name: "a window too short to draw any text still settles on the row", window: reader.Window{Len: 20, Markers: true}, row: 10, height: 1, want: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.window.Reveal(tt.row, tt.height))
		})
	}
}

// TestReader covers the cursor and the window moved together and apart.
func TestReader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		start    reader.Reader
		act      func(*reader.Reader)
		wantLine int
		wantTop  int
	}{
		{
			name:     "moving past the end stops on the last row",
			act:      func(r *reader.Reader) { r.MoveTo(99, 20, 5) },
			wantLine: 19,
			wantTop:  16,
		},
		{
			name:     "moving before the start stops on the first row",
			start:    reader.Reader{Line: 5, Top: 4},
			act:      func(r *reader.Reader) { r.MoveTo(-3, 20, 5) },
			wantLine: 0,
			wantTop:  0,
		},
		{
			name:     "scrolling moves the window and not the cursor",
			start:    reader.Reader{Line: 1},
			act:      func(r *reader.Reader) { r.Scroll(6, 20, 5) },
			wantLine: 1,
			wantTop:  6,
		},
		{
			name:     "settling a cursor scrolled out of the window puts it on the first row drawn",
			start:    reader.Reader{Line: 1, Top: 6},
			act:      func(r *reader.Reader) { r.Settle(20, 5) },
			wantLine: 6,
			wantTop:  6,
		},
		{
			name:     "settling a cursor the window draws leaves it there",
			start:    reader.Reader{Line: 8, Top: 6},
			act:      func(r *reader.Reader) { r.Settle(20, 5) },
			wantLine: 8,
			wantTop:  6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := tt.start
			tt.act(&r)
			assert.Equal(t, tt.wantLine, r.Line)
			assert.Equal(t, tt.wantTop, r.Top)
		})
	}
}

// TestMatches covers the rows and spans a pattern is found in, measured without styling.
func TestMatches(t *testing.T) {
	t.Parallel()

	rows := []string{"\x1b[1mwarm\x1b[0m cache", "cold", "a warm day", ""}

	assert.Equal(t, []int{0, 2}, reader.Matches(regexp.MustCompile("warm"), rows))
	assert.Nil(t, reader.Matches(nil, rows))
	assert.Equal(t, []paint.Span{{Start: 2, End: 6}}, reader.Spans(regexp.MustCompile("warm"), rows[2]))
	assert.Equal(t, []paint.Span{{Start: 0, End: 4}}, reader.Spans(regexp.MustCompile("warm"), rows[0]), "a span is measured without the styling")
}

// TestJump covers stepping between matches, wrapping at either end.
func TestJump(t *testing.T) {
	t.Parallel()

	found := []int{2, 5, 9}
	tests := []struct {
		name      string
		found     []int
		from      int
		direction int
		want      int
		wantFound bool
	}{
		{name: "forward lands on the next match", found: found, from: 2, direction: 1, want: 5, wantFound: true},
		{name: "forward past the last wraps to the first", found: found, from: 9, direction: 1, want: 2, wantFound: true},
		{name: "backward lands on the previous match", found: found, from: 5, direction: -1, want: 2, wantFound: true},
		{name: "backward before the first wraps to the last", found: found, from: 1, direction: -1, want: 9, wantFound: true},
		{name: "nothing found stays put", from: 4, direction: 1, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := reader.Jump(tt.found, tt.from, tt.direction)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantFound, ok)
		})
	}
}

// TestFrom covers the first match at or after a row.
func TestFrom(t *testing.T) {
	t.Parallel()

	found := []int{2, 5, 9}
	tests := []struct {
		name      string
		found     []int
		from      int
		want      int
		wantFound bool
	}{
		{name: "a match on the row itself is taken", found: found, from: 5, want: 5, wantFound: true},
		{name: "the next match after the row is taken", found: found, from: 6, want: 9, wantFound: true},
		{name: "past the last wraps to the first", found: found, from: 10, want: 2, wantFound: true},
		{name: "nothing found stays put", from: 3, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := reader.From(tt.found, tt.from)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantFound, ok)
		})
	}
}

// TestCount covers where among the matches a row stands.
func TestCount(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2 of 3", reader.Count([]int{2, 5, 9}, 5))
	assert.Equal(t, "3 matches", reader.Count([]int{2, 5, 9}, 4))
	assert.Equal(t, "0 matches", reader.Count(nil, 4))
}
