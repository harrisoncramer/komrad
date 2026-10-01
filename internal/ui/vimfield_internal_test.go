package ui

import (
	"testing"

	"komrad/internal/vim"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"github.com/stretchr/testify/assert"
)

// TestVimFieldBuffers covers a buffer going into a bubbles text model and coming back out whole.
func TestVimFieldBuffers(t *testing.T) {
	tests := []struct {
		name  string
		width int
		b     vim.Buffer
	}{
		{
			name:  "the start of the text",
			width: 40,
			b:     vim.Buffer{Text: "one\ntwo", Cursor: 0},
		},
		{
			name:  "the middle of a later line",
			width: 40,
			b:     vim.Buffer{Text: "one\ntwo\nthree", Cursor: 10},
		},
		{
			name:  "an empty line",
			width: 40,
			b:     vim.Buffer{Text: "one\n\nthree", Cursor: 4},
		},
		{
			name:  "a line wrapped onto several rows",
			width: 10,
			b:     vim.Buffer{Text: "a line that wraps well past the width\nnext", Cursor: 38},
		},
		{
			name:  "late on a wrapped line",
			width: 10,
			b:     vim.Buffer{Text: "first\na line that wraps well past the width", Cursor: 30},
		},
		{
			name:  "wide runes",
			width: 40,
			b:     vim.Buffer{Text: "日本語\n漢字かな", Cursor: 6},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			area := textarea.New()
			area.SetWidth(tt.width)
			area.SetHeight(10)
			area.Focus()

			writeArea(&area, tt.b)

			assert.Equal(t, tt.b, readArea(&area))
		})
	}

	t.Run("input", func(t *testing.T) {
		input := textinput.New()
		input.Focus()

		writeInput(&input, vim.Buffer{Text: "日本 語", Cursor: 3})

		assert.Equal(t, vim.Buffer{Text: "日本 語", Cursor: 3}, readInput(&input))
	})
}
