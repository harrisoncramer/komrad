package diffview

import (
	"testing"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
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

// TestMarkForSaysWhichThreadIsWhich covers the gutter glyph ranking an unread reply first,
// then settling, then whether the thread still stands on the code it was written against.
func TestMarkForSaysWhichThreadIsWhich(t *testing.T) {
	view := NewView(patchWith(diff.File{
		OldPath: "greet.go",
		NewPath: "greet.go",
		Hunks: []diff.Hunk{hunkOf(
			diff.Line{
				Kind:      diff.Context,
				Text:      "package greet",
				OldNumber: 1,
				NewNumber: 1,
			},
		)},
	}), false)

	thread := threadAt("t1", "greet.go", 1, 1, " package greet")
	view.PinThreads([]*agentv1.ReviewThread{thread})

	mark, style := view.MarkFor(thread)
	assert.Equal(t, glyph.Defaults().Spell(glyph.ThreadRead), mark)
	assert.Equal(t, paint.Panel.Style(paint.ToneSelf), style, "a thread the person opened is drawn in their colour")

	thread.ResolvedAt = "2026-08-27T12:00:00Z"
	mark, style = view.MarkFor(thread)
	assert.Equal(t, glyph.Defaults().Spell(glyph.ThreadSettled), mark)
	assert.Equal(t, paint.Panel.Style(paint.ToneSelf), style, "and the check keeps the author's colour")

	thread.UnreadByHuman = true
	mark, _ = view.MarkFor(thread)
	assert.Equal(t, glyph.Defaults().Spell(glyph.ThreadUnread), mark,
		"a reply nobody has read outranks the settling, the gutter being the only place it is said")

	thread.Messages = []*agentv1.ReviewMessage{
		{AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT},
	}
	_, style = view.MarkFor(thread)
	assert.Equal(t, paint.Panel.Style(paint.ToneOther), style, "a thread the agent opened is drawn in its own colour")
}
