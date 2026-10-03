package diffview

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

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
			for _, cursor := range []Cursor{noCursor, LiveCursor, DimCursor} {
				drawn := view.renderRow(y, cursor, width, 0)
				assert.LessOrEqual(t, lipgloss.Width(drawn), width,
					"row %d at width %d: %q", y, width, ansi.Strip(drawn))

				window := Window{Width: width, Height: 1, Wrap: true}
				wrapped := view.renderLines(y, cursor, window, 0)
				assert.Len(t, wrapped, view.measureRow(y, window), "row %d at width %d", y, width)
				for _, line := range wrapped {
					assert.LessOrEqual(t, lipgloss.Width(line), width,
						"wrapped row %d at width %d: %q", y, width, ansi.Strip(line))
				}
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

	mark, style := view.markFor(thread)
	assert.Equal(t, glyph.Defaults().Spell(glyph.ThreadRead), mark)
	assert.Equal(t, paint.Panel.Style(paint.ToneUserMe), style, "a thread the person opened is drawn in their colour")

	thread.ResolvedAt = "2026-08-27T12:00:00Z"
	mark, style = view.markFor(thread)
	assert.Equal(t, glyph.Defaults().Spell(glyph.ThreadSettled), mark)
	assert.Equal(t, paint.Panel.Style(paint.ToneUserMe), style, "and the check keeps the author's colour")

	thread.UnreadByHuman = true
	mark, _ = view.markFor(thread)
	assert.Equal(t, glyph.Defaults().Spell(glyph.ThreadUnread), mark,
		"a reply nobody has read outranks the settling, the gutter being the only place it is said")

	thread.Messages = []*agentv1.ReviewMessage{
		{AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT},
	}
	_, style = view.markFor(thread)
	assert.Equal(t, paint.Panel.Style(paint.ToneUserAgent), style, "a thread the agent opened is drawn in its own colour")
}

// TestTheDiffReachesTheTerminalWithoutItsControlCharacters pins control bytes in a path, hunk heading
// or line against moving the cursor or repainting the pane.
func TestTheDiffReachesTheTerminalWithoutItsControlCharacters(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name string
		text string
	}

	tests := []testCase{
		{
			name: "a carriage return ending a CRLF line",
			text: "func greet() {\r",
		},
		{
			name: "an escape sequence held in the file",
			text: "fmt.Print(\"\x1b[2J\")",
		},
		{
			name: "a backspace",
			text: "ab\bc",
		},
		{
			name: "a C1 control sequence introducer",
			text: "fmt.Print(\"\u009b2J\")",
		},
	}

	colour := regexp.MustCompile(`\x1b\[[0-9;]*m`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hunk := hunkOf(diff.Line{
				Kind:      diff.Addition,
				Text:      tt.text,
				NewNumber: 1,
			})
			hunk.Header += " " + tt.text

			view := NewView(patchWith(diff.File{
				OldPath:  "greet.go",
				NewPath:  "greet" + tt.text + ".go",
				Metadata: []string{"renamed from " + tt.text},
				Hunks:    []diff.Hunk{hunk},
			}), false)

			for y := range view.Len() {
				drawn := colour.ReplaceAllString(view.renderRow(y, noCursor, 40, 0), "")
				assert.False(t, strings.ContainsFunc(drawn, unicode.IsControl), "row %d: %q", y, drawn)
			}
		})
	}
}

// TestAWrappedLineReadsBackWhole covers a code line too wide for the pane drawn on the lines
// under it, its gutter on the first and blank on the rest.
func TestAWrappedLineReadsBackWhole(t *testing.T) {
	t.Parallel()

	text := "the quick brown fox jumps over the lazy dog"
	view := NewView(patchWith(diff.FromTexts("notes.md", "one", text)), false)
	view.SetHeadless(true)

	y := view.GetLastRow()
	window := Window{Width: GutterWidth + 10, Height: 10, Wrap: true}
	lines := view.renderLines(y, noCursor, window, 0)

	var read strings.Builder
	for i, line := range lines {
		stripped := ansi.Strip(line)
		gutter := ansi.Cut(stripped, 0, GutterWidth)
		if i > 0 {
			assert.Equal(t, strings.Repeat(" ", GutterWidth), gutter, "line %d", i)
		}

		read.WriteString(strings.TrimRight(ansi.Cut(stripped, GutterWidth, window.Width), " "))
	}

	assert.Len(t, lines, 5)
	assert.Equal(t, strings.ReplaceAll(text, " ", ""), strings.ReplaceAll(read.String(), " ", ""))
}

// TestAWrappedLineIsMeasuredAsDrawn covers a row counted from the source it is drawn from when
// that source says more than the patch.
func TestAWrappedLineIsMeasuredAsDrawn(t *testing.T) {
	t.Parallel()

	file := diff.FromTexts("notes.md", "one", "short")
	file.NewSource = "the quick brown fox jumps over the lazy dog"
	view := NewView(patchWith(file), false)
	view.SetHeadless(true)

	y := view.GetLastRow()
	window := Window{Width: GutterWidth + 10, Height: 10, Wrap: true}

	assert.Len(t, view.renderLines(y, noCursor, window, 0), view.measureRow(y, window))
	assert.Greater(t, view.measureRow(y, window), 1)
}

// TestCarryStyleOpensEachLineInTheStyleLeftOpen covers a style cut by a wrap carried onto the
// next line and closed at the end of the first.
func TestCarryStyleOpensEachLineInTheStyleLeftOpen(t *testing.T) {
	t.Parallel()

	red := "\x1b[31m"

	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "a style left open is closed and opened again on the next line",
			lines: []string{red + "ab", "cd" + ansi.ResetStyle, "ef"},
			want:  []string{red + "ab" + ansi.ResetStyle, red + "cd" + ansi.ResetStyle, "ef"},
		},
		{
			name:  "a style closed on its own line carries nothing",
			lines: []string{red + "ab" + ansi.ResetStyle, "cd"},
			want:  []string{red + "ab" + ansi.ResetStyle, "cd"},
		},
		{
			name:  "plain lines are left as they were",
			lines: []string{"ab", "cd"},
			want:  []string{"ab", "cd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, carryStyle(tt.lines))
		})
	}
}
