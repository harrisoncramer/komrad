package ui

import (
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// TestFilterQuery covers what a typed pattern matches, including a half-typed one that will
// not compile and so draws every row rather than looking like an empty list.
func TestFilterQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		row        string
		wantMatch  bool
		wantBroken bool
	}{
		{
			name:      "an empty query keeps every row",
			query:     "",
			row:       "anything",
			wantMatch: true,
		},
		{
			name:      "a name the pattern does not reach is held back",
			query:     "^api",
			row:       "web-api",
			wantMatch: false,
		},
		{
			name:      "a pattern in lower case matches either case",
			query:     "api",
			row:       "API-gateway",
			wantMatch: true,
		},
		{
			name:      "a pattern carrying an upper case letter matches exactly",
			query:     "API",
			row:       "api-gateway",
			wantMatch: false,
		},
		{
			name:      "a pattern is a regular expression rather than a substring",
			query:     "^(api|web)-",
			row:       "web-ui",
			wantMatch: true,
		},
		{
			name:       "a pattern that will not compile draws every row",
			query:      "api[",
			row:        "unrelated",
			wantMatch:  true,
			wantBroken: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var filter filterState
			filter.setQuery(tt.query)

			assert.Equal(t, tt.wantMatch, filter.doesMatch(tt.row))
			assert.Equal(t, tt.wantBroken, filter.broken)
		})
	}
}

// TestFilterLine covers the colour of the glyph opening the line, which is all an always drawn
// line has to say whether anything is being searched for.
func TestFilterLine(t *testing.T) {
	var idle filterState
	assert.Contains(t, idle.line(paint.Panel), dimStyle.Render("/"), "an idle line is dim")
	assert.NotContains(t, idle.line(paint.Panel), paint.Panel.Cursor().Render("/"))

	typing := filterState{typing: true}
	assert.Contains(t, typing.line(paint.Panel), paint.Panel.Cursor().Render("/"), "an open input takes the accent")

	var standing filterState
	standing.setQuery("pay")
	assert.Contains(t, standing.line(paint.Panel), paint.Panel.Cursor().Render("/"), "and so does a pattern left standing")

	narrowing := filterState{typing: true, mode: filterModeFilter}
	assert.Contains(t, narrowing.line(paint.Panel), paint.Panel.Cursor().Render("▽"), "the funnel takes the cursor's colour like the slash")
	assert.NotContains(t, narrowing.line(paint.Panel), paint.Panel.Match().Render("▽"))
}

// TestFilterHelpNamesTheOtherMode covers the legend under the input saying what tab would
// switch to.
func TestFilterHelpNamesTheOtherMode(t *testing.T) {
	assert.Equal(t, "filter instead", filterHelp(filterModeSearch).short[1].Help().Desc)
	assert.Equal(t, "search instead", filterHelp(filterModeFilter).short[1].Help().Desc)
}

// TestReviewSearchIgnoresTab covers the review's own search, which shares the input's keys,
// having no mode to flip.
func TestReviewSearchIgnoresTab(t *testing.T) {
	filter := filterState{typing: true}

	assert.False(t, filter.takeKey(keyMsg("tab")))
	assert.Equal(t, filterModeSearch, filter.mode)
	assert.Equal(t, "/", filter.mode.glyph())
}
