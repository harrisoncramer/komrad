package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

// TestHighlighted covers the marks written into a transcript preview, which are what make the
// matched words findable in the conversation around them.
func TestHighlighted(t *testing.T) {
	content := "first\nsecond match\nthird match"

	tests := []struct {
		name  string
		spans []matchSpan
		want  string
	}{
		{
			name:  "nothing matched leaves the transcript alone",
			spans: nil,
			want:  content,
		},
		{
			name:  "the matched word is marked apart from the line holding it",
			spans: []matchSpan{{start: 13, end: 18}},
			want:  "first\n\x1b[7msecond \x1b[m\x1b[7;1;4mmatch\x1b[m\nthird match",
		},
		{
			name:  "every match takes a mark",
			spans: []matchSpan{{start: 13, end: 18}, {start: 25, end: 30}},
			want:  "first\n\x1b[7msecond \x1b[m\x1b[7;1;4mmatch\x1b[m\n\x1b[7mthird \x1b[m\x1b[7;1;4mmatch\x1b[m",
		},
		{
			name:  "a span past the end of the transcript marks nothing",
			spans: []matchSpan{{start: 200, end: 210}},
			want:  content,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, highlighted(content, tt.spans))
		})
	}
}
