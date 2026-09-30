package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestReviewNoteWrapsBeforeTheFirstResize covers a note drawn before the panel knows its width wrapping at a readable column.
func TestReviewNoteWrapsBeforeTheFirstResize(t *testing.T) {
	note := strings.TrimRight(reviewNote(lineWidthOf(0), dimStyle, "editing this round replaces the edit of an earlier one, so press again to go on"), "\n")

	assert.Less(t, strings.Count(note, "\n")+1, 10, "rows before the first resize: %q", ansi.Strip(note))
}
