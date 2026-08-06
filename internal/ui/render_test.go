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

// TestHighlighted covers the mark written into a transcript preview, which is what
// makes the matched line findable in the conversation around it.
func TestHighlighted(t *testing.T) {
	content := "first\nsecond\nthird"

	tests := []struct {
		name string
		line int
		want string
	}{
		{
			name: "no matched line leaves the transcript alone",
			line: 0,
			want: content,
		},
		{
			name: "a line past the end of the transcript leaves it alone",
			line: 4,
			want: content,
		},
		{
			name: "the matched line is marked and its neighbours are not",
			line: 2,
			want: "first\n\x1b[7msecond\x1b[m\nthird",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, highlighted(content, tt.line))
		})
	}
}
