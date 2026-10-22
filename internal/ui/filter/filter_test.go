package filter_test

import (
	"testing"

	"komrad/internal/glyph"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/paint"

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
			name:       "a pattern that will not compile renders every row",
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
			var state filter.State
			state.SetQuery(tt.query)

			assert.Equal(t, tt.wantMatch, state.Pattern().DoesMatch(tt.row))
			assert.Equal(t, tt.wantBroken, state.Broken)
		})
	}
}

// TestFilterLine covers the colour of the glyph opening the filter line.
func TestFilterLine(t *testing.T) {
	var standing filter.State
	standing.SetQuery("pay")

	tests := []struct {
		name       string
		filter     filter.State
		want       string
		wantAbsent string
	}{
		{
			name:       "an idle line is dim",
			filter:     filter.State{},
			want:       paint.Panel.Style(paint.ToneMuted).Render("/"),
			wantAbsent: paint.Panel.Cursor().Render("/"),
		},
		{
			name:   "an open input takes the accent",
			filter: filter.State{Typing: true},
			want:   paint.Panel.Cursor().Render("/"),
		},
		{
			name:   "a pattern left standing takes the accent",
			filter: standing,
			want:   paint.Panel.Cursor().Render("/"),
		},
		{
			name: "the funnel takes the cursor's colour like the slash",
			filter: filter.State{
				Typing: true,
				Mode:   filter.ModeFilter,
			},
			want:       paint.Panel.Cursor().Render("▽"),
			wantAbsent: paint.Panel.Match().Render("▽"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := tt.filter.Line(glyph.Defaults())

			assert.Contains(t, line, tt.want)
			if tt.wantAbsent != "" {
				assert.NotContains(t, line, tt.wantAbsent)
			}
		})
	}
}

// TestReviewSearchIgnoresTab covers the review search having no mode for tab to flip.
func TestReviewSearchIgnoresTab(t *testing.T) {
	state := filter.State{Typing: true}

	assert.False(t, state.TakeKey(keys.Press("tab"), nil))
	assert.Equal(t, filter.ModeSearch, state.Mode)
	assert.Equal(t, "/", state.Mode.Glyph(glyph.Defaults()))
}
