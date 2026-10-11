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

// TestHighlightStyled covers a search hit painted over a row that already carries styling.
func TestHighlightStyled(t *testing.T) {
	t.Parallel()

	bold := "\x1b[1m"
	tests := []struct {
		name  string
		row   string
		spans []Span
		want  string
	}{
		{
			name:  "a hit in plain text is painted",
			row:   "keep it warm",
			spans: []Span{{Start: 8, End: 12}},
			want:  "keep it " + Panel.stageMatch().Render("warm"),
		},
		{
			name:  "a hit inside styling puts the styling back after it",
			row:   bold + "a warm day" + ansiReset,
			spans: []Span{{Start: 2, End: 6}},
			want:  bold + "a " + Panel.stageMatch().Render("warm") + bold + " day" + ansiReset,
		},
		{
			name:  "a hit across a change of style is painted either side of it",
			row:   "wa" + bold + "rm" + ansiReset,
			spans: []Span{{Start: 0, End: 4}},
			want:  Panel.stageMatch().Render("wa") + bold + Panel.stageMatch().Render("rm") + bold + ansiReset,
		},
		{
			name: "no hits leaves the row alone",
			row:  bold + "cold" + ansiReset,
			want: bold + "cold" + ansiReset,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := Panel.HighlightStyled(tt.row, tt.spans)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, PlainText(tt.row), PlainText(got))
		})
	}
}

// TestPlainText covers a styled row read back without its escape sequences.
func TestPlainText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "a warm day", PlainText("\x1b[1ma \x1b[38;5;3mwarm\x1b[0m day"))
	assert.Equal(t, "café ☕", PlainText("\x1b[1mcafé\x1b[0m ☕"))
}

// TestClipAround covers the cursor staying in view whichever side of it the text overflows.
func TestClipAround(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		before     string
		after      string
		width      int
		wantBefore string
		wantAfter  string
	}{
		{
			name:       "text that fits is left whole",
			before:     "ab",
			after:      "cd",
			width:      4,
			wantBefore: "ab",
			wantAfter:  "cd",
		},
		{
			name:       "a cursor near the end cuts the start",
			before:     "abcdef",
			after:      "g",
			width:      4,
			wantBefore: "…ef",
			wantAfter:  "g",
		},
		{
			name:       "a cursor near the start cuts the end",
			before:     "a",
			after:      "bcdefg",
			width:      4,
			wantBefore: "a",
			wantAfter:  "bc…",
		},
		{
			name:       "a cursor in the middle cuts both sides",
			before:     "abcdef",
			after:      "ghijkl",
			width:      6,
			wantBefore: "…ef",
			wantAfter:  "gh…",
		},
		{
			name:       "a tail wider than the column with text before the cursor",
			before:     "a",
			after:      "bcdefghij",
			width:      1,
			wantBefore: "a",
			wantAfter:  "",
		},
		{
			name:   "no room renders nothing",
			before: "abc",
			after:  "def",
			width:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before, after := ClipAround(tt.before, tt.after, tt.width)

			assert.Equal(t, tt.wantBefore, before)
			assert.Equal(t, tt.wantAfter, after)
		})
	}
}

// TestTitledRule covers where a rule sets its labels and when it falls back to a plain line.
func TestTitledRule(t *testing.T) {
	tests := []struct {
		name  string
		title string
		tail  string
		width int
		want  string
	}{
		{
			name:  "a title sits near the left end",
			title: "Info",
			width: 30,
			want:  "─── Info ─────────────────────",
		},
		{
			name:  "a tail sits against the right end",
			title: "Info",
			tail:  "2 modified",
			width: 30,
			want:  "─── Info ──────── 2 modified ─",
		},
		{
			name:  "a tail with no title still sits against the right end",
			tail:  "2 modified",
			width: 30,
			want:  "───────────────── 2 modified ─",
		},
		{
			name:  "a rule narrower than the floor is a plain line",
			title: "Info",
			width: 20,
			want:  strings.Repeat("─", 20),
		},
		{
			name:  "labels too long for the rule give way to a plain line",
			title: strings.Repeat("x", 30),
			width: 30,
			want:  strings.Repeat("─", 30),
		},
		{
			name:  "no labels is a plain line",
			width: 30,
			want:  strings.Repeat("─", 30),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ansi.Strip(Panel.TitledRule(tt.title, tt.tail, tt.width)))
		})
	}
}
