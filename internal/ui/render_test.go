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
// matched lines findable in the conversation around them.
func TestHighlighted(t *testing.T) {
	content := "first\nsecond match\nthird match"

	tests := []struct {
		name  string
		lines []int
		want  string
	}{
		{
			name:  "nothing matched leaves the transcript alone",
			lines: nil,
			want:  content,
		},
		{
			name:  "the matched line is marked and its neighbours are not",
			lines: []int{2},
			want:  "first\n\x1b[7msecond match\x1b[m\nthird match",
		},
		{
			name:  "every matched line takes a mark",
			lines: []int{2, 3},
			want:  "first\n\x1b[7msecond match\x1b[m\n\x1b[7mthird match\x1b[m",
		},
		{
			name:  "a line past the end of the transcript marks nothing",
			lines: []int{9},
			want:  content,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, highlighted(content, tt.lines))
		})
	}
}
