package ui

import (
	"testing"

	"weave/internal/ui/paint"

	"github.com/stretchr/testify/assert"
)

// TestFilterQuery covers what a typed pattern matches.
func TestFilterQuery(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		row        string
		wantMatch  bool
		wantBroken bool
	}{
		{
			name:       "a pattern that will not compile draws every row",
			query:      "api[",
			row:        "unrelated",
			wantMatch:  true,
			wantBroken: true,
		},
		{
			name:      "a name the pattern does not reach is held back",
			query:     "^api",
			row:       "web-api",
			wantMatch: false,
		},
		{
			name:      "an empty query keeps every row",
			query:     "",
			row:       "anything",
			wantMatch: true,
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

// TestFilterLine covers the colour of the glyph opening the filter line.
func TestFilterLine(t *testing.T) {
	var standing filterState
	standing.setQuery("pay")

	tests := []struct {
		name       string
		filter     filterState
		want       string
		wantAbsent string
	}{
		{
			name:       "an idle line is dim",
			filter:     filterState{},
			want:       dimStyle.Render("/"),
			wantAbsent: paint.Panel.Cursor().Render("/"),
		},
		{
			name:   "an open input takes the accent",
			filter: filterState{typing: true},
			want:   paint.Panel.Cursor().Render("/"),
		},
		{
			name:   "a pattern left standing takes the accent",
			filter: standing,
			want:   paint.Panel.Cursor().Render("/"),
		},
		{
			name: "the funnel takes the cursor's colour like the slash",
			filter: filterState{
				typing: true,
				mode:   filterModeFilter,
			},
			want:       paint.Panel.Cursor().Render("▽"),
			wantAbsent: paint.Panel.Match().Render("▽"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := tt.filter.line(paint.Panel)

			assert.Contains(t, line, tt.want)
			if tt.wantAbsent != "" {
				assert.NotContains(t, line, tt.wantAbsent)
			}
		})
	}
}

// TestFilterHelpNamesTheOtherMode covers the legend naming what tab switches to.
func TestFilterHelpNamesTheOtherMode(t *testing.T) {
	tests := []struct {
		name string
		mode filterMode
		want string
	}{
		{
			name: "a search offers the filter",
			mode: filterModeSearch,
			want: "filter instead",
		},
		{
			name: "a filter offers the search",
			mode: filterModeFilter,
			want: "search instead",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, filterHelp(tt.mode).short[1].Help().Desc)
		})
	}
}

// TestReviewSearchIgnoresTab covers the review search having no mode for tab to flip.
func TestReviewSearchIgnoresTab(t *testing.T) {
	filter := filterState{typing: true}

	assert.False(t, filter.takeKey(keyMsg("tab")))
	assert.Equal(t, filterModeSearch, filter.mode)
	assert.Equal(t, "/", filter.mode.glyph())
}
