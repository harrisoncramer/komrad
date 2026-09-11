package review

import (
	"testing"

	"weave/internal/diff"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/stretchr/testify/assert"
)

// TestAThreadIsNamedByItsOpeningLine pins what the sidebar names a thread by.
func TestAThreadIsNamedByItsOpeningLine(t *testing.T) {
	t.Parallel()

	typed := &agentv1.ReviewThread{
		Messages: []*agentv1.ReviewMessage{{Body: "this needs a nil check"}},
	}

	assert.Equal(t, "this needs a nil check", firstLine(typed))
}

// TestNoRowIsWiderThanThePaneItWasDrawnFor pins every kind of row against the width it was
// handed.
func TestNoRowIsWiderThanThePaneItWasDrawnFor(t *testing.T) {
	t.Parallel()

	view := NewView(patchWith(diff.File{
		OldPath: "internal/こんにちは.go",
		NewPath: "internal/こんにちは.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
			diff.Line{
				Kind:      diff.Addition,
				Text:      "\tsay(\"日本語のテキスト\")",
				NewNumber: 2,
			},
			diff.Line{
				Kind:      diff.Deletion,
				Text:      "\t\tolder line with tabs",
				OldNumber: 2,
			},
		)},
	}), false)

	view.PinThreads([]*agentv1.ReviewThread{{
		Id:       "t1",
		FilePath: "internal/こんにちは.go",
		NewStart: 2,
		NewEnd:   2,
		Messages: []*agentv1.ReviewMessage{{Body: "この行は長すぎます"}},
	}})

	for width := 1; width <= 40; width++ {
		for y := range view.Len() {
			for _, cursor := range []Cursor{NoCursor, LiveCursor, DimCursor} {
				drawn := view.RenderRow(y, cursor, width, 0)
				assert.LessOrEqual(t, lipgloss.Width(drawn), width,
					"row %d at width %d: %q", y, width, ansi.Strip(drawn))
			}
		}
	}
}
