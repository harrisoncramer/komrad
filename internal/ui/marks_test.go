package ui

import (
	"testing"

	"weave/internal/appconfig"
	"weave/lib/xptr"

	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
)

// marks are the configured marks the cases below step through, each in a colour of
// its own so a row drawing the wrong one shows up.
var marks = []appconfig.Mark{
	{Glyph: "●", Color: "#F932A1"},
	{Glyph: "◐", Color: "#0f0"},
	{Glyph: "✓", Color: ""},
}

// TestNextMark walks the cycle the mark key drives. One key covers every mark and
// clearing, so where it goes next is the whole behaviour.
func TestNextMark(t *testing.T) {
	tests := []struct {
		name    string
		current *string
		marks   []appconfig.Mark
		want    *string
	}{
		{
			name:  "an unmarked agent with nothing configured stays unmarked",
			marks: nil,
			want:  nil,
		},
		{
			name:    "a marked agent with nothing configured clears",
			current: xptr.Ptr("●"),
			marks:   nil,
			want:    nil,
		},
		{
			name:    "a glyph the config no longer lists clears, so the key is a way back out",
			current: xptr.Ptr("★"),
			marks:   marks,
			want:    nil,
		},
		{
			name:  "an unmarked agent takes the first mark",
			marks: marks,
			want:  xptr.Ptr("●"),
		},
		{
			name:    "a marked agent moves along the list",
			current: xptr.Ptr("◐"),
			marks:   marks,
			want:    xptr.Ptr("✓"),
		},
		{
			name:    "the last mark clears rather than wrapping around",
			current: xptr.Ptr("✓"),
			marks:   marks,
			want:    nil,
		},
		{
			name:    "one configured mark leaves the key a toggle",
			current: xptr.Ptr("●"),
			marks:   []appconfig.Mark{{Glyph: "●"}},
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextMark(tt.current, tt.marks))
		})
	}
}

// TestMarkColor covers which colour a glyph is drawn in, including the glyphs the
// config has nothing to say about. A colour comes back as lipgloss reads it, so a
// configured name arrives here as the index it draws as.
func TestMarkColor(t *testing.T) {
	tests := []struct {
		name  string
		glyph string
		marks []appconfig.Mark
		want  string
	}{
		{
			name:  "a glyph the config does not list falls back to the default yellow",
			glyph: "★",
			marks: marks,
			want:  "3",
		},
		{
			name:  "so does one configured with no colour of its own",
			glyph: "✓",
			marks: marks,
			want:  "3",
		},
		{
			name:  "a configured colour is the one used",
			glyph: "●",
			marks: marks,
			want:  "#F932A1",
		},
		{
			name:  "including a three-digit one",
			glyph: "◐",
			marks: marks,
			want:  "#0f0",
		},
		{
			name:  "a colour named rather than given in hex draws as its index",
			glyph: "◆",
			marks: []appconfig.Mark{{Glyph: "◆", Color: "brightcyan"}},
			want:  "14",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, string(markColor(tt.glyph, tt.marks)))
		})
	}
}

// TestMarkedMark covers what a row draws. The glyph is the mark itself, so an agent
// carrying one the config never listed still shows it rather than vanishing. Which
// colour it takes is TestMarkColor's business, since lipgloss only writes the escape
// when it decides the terminal has a profile.
func TestMarkedMark(t *testing.T) {
	tests := []struct {
		name string
		mark *string
		want string
	}{
		{
			name: "an unmarked agent draws nothing",
		},
		{
			name: "a marked agent draws its glyph",
			mark: xptr.Ptr("●"),
			want: "●",
		},
		{
			name: "a glyph the config does not list still draws",
			mark: xptr.Ptr("★"),
			want: "★",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := markGlyph(agentstore.Agent{Mark: tt.mark}, marks)
			if tt.want == "" {
				assert.Empty(t, got)
				return
			}

			assert.Contains(t, got, tt.want)
		})
	}
}
