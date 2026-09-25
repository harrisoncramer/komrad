package paint

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestWrapRows covers every row fitting its column with no character of the text dropped.
func TestWrapRows(t *testing.T) {
	const path = "/Users/harrisoncramer/.local/state/komrad/worktrees/01a086ae-d59b-769b-a8a0-202d4014bddd/bin/editor"

	tests := []struct {
		name  string
		width int
	}{
		{
			name:  "a width where the wrap misplaces a breakpoint in the path",
			width: 61,
		},
		{
			name:  "a narrow column",
			width: 20,
		},
		{
			name:  "a wide column",
			width: 80,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := WrapRows("a "+path, tt.width)

			for i, row := range rows {
				assert.LessOrEqual(t, ansi.StringWidth(row), tt.width, "row %d: %q", i, row)
			}
			assert.Contains(t, strings.ReplaceAll(strings.Join(rows, ""), " ", ""), path)
		})
	}
}
