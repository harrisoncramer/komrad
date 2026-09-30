package glyph_test

import (
	"testing"

	"komrad/internal/glyph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEveryGlyphHasAKeyAndAnApprovedDefault covers the table a glyph is added to.
func TestEveryGlyphHasAKeyAndAnApprovedDefault(t *testing.T) {
	keys := map[string]bool{}
	for _, g := range glyph.All() {
		key := g.Key()
		require.NotEmpty(t, key, "glyph %d has no key in the config", g)
		assert.False(t, keys[key], "%s names two glyphs", key)
		keys[key] = true

		found, ok := glyph.ByKey(key)
		require.True(t, ok)
		assert.Equal(t, g, found)

		assert.NoError(t, glyph.Check(glyph.Defaults().Spell(g)), "%s ships a character it would refuse", key)
	}
}

// TestChromeIsApproved covers the fixed characters against the same set a person chooses from.
func TestChromeIsApproved(t *testing.T) {
	for _, chrome := range glyph.Chrome() {
		assert.NoError(t, glyph.Check(chrome.Text), "%s", chrome.Name)
	}
}

// TestZeroSetDrawsTheDefaults covers a set nobody loaded, which draws what ships rather than
// nothing.
func TestZeroSetDrawsTheDefaults(t *testing.T) {
	var zero glyph.Set
	for _, g := range glyph.All() {
		assert.Equal(t, glyph.Defaults().Spell(g), zero.Spell(g))
	}

	assert.Empty(t, zero.Spell(glyph.None))
}

// TestDefaultsSpellTheirCodePoints pins the defaults that are written as escapes.
func TestDefaultsSpellTheirCodePoints(t *testing.T) {
	tests := []struct {
		glyph glyph.Glyph
		want  rune
	}{
		{glyph.Worktree, 0xf418},
		{glyph.WorktreeGone, 0xf52f},
		{glyph.Open, 0xf407},
		{glyph.ChangesRequested, 0xf4d2},
		{glyph.Approved, 0xf42e},
		{glyph.Merged, 0xf419},
		{glyph.Draft, 0xf4dd},
		{glyph.Closed, 0xf4dc},
		{glyph.Failing, 0xf467},
		{glyph.Conflicting, 0xf47f},
		{glyph.FailedScript, 0xf489},
		{glyph.Workflow, 0xf52e},
		{glyph.Archive, 0xf411},
		{glyph.Search, 0xf422},
		{glyph.MessageSuccess, '✓'},
	}

	for _, tt := range tests {
		t.Run(tt.glyph.Key(), func(t *testing.T) {
			assert.Equal(t, string(tt.want), glyph.Defaults().Spell(tt.glyph))
		})
	}
}

// TestLoad covers what a person may choose and how a refusal names it.
func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[glyph.Glyph]string
		wantErr   string
		want      map[glyph.Glyph]string
	}{
		{
			name:      "an approved character replaces the default",
			overrides: map[glyph.Glyph]string{glyph.FailedScript: ""},
			want:      map[glyph.Glyph]string{glyph.FailedScript: "", glyph.Mail: ""},
		},
		{
			name:      "an emoji is refused with its key and code point",
			overrides: map[glyph.Glyph]string{glyph.FailedScript: "\U0001F4A5"},
			wantErr:   "failed to draw marks.failed_script: U+1F4A5 is not an approved glyph",
		},
		{
			name:      "a character from outside the set is refused",
			overrides: map[glyph.Glyph]string{glyph.Filter: "▼"},
			wantErr:   "failed to draw lists.filter: U+25BC is not an approved glyph",
		},
		{
			name:      "two characters are refused",
			overrides: map[glyph.Glyph]string{glyph.Unread: "··"},
			wantErr:   `failed to draw marks.unread: "··" is not a single character`,
		},
		{
			name:      "an empty value is refused",
			overrides: map[glyph.Glyph]string{glyph.Unread: ""},
			wantErr:   `failed to draw marks.unread: "" is not a single character`,
		},
		{
			name:      "a glyph the panel does not draw is refused",
			overrides: map[glyph.Glyph]string{glyph.None: "·"},
			wantErr:   "glyph 0 is not one the panel draws",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, err := glyph.Load(tt.overrides)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			for g, want := range tt.want {
				assert.Equal(t, want, set.Spell(g))
			}
		})
	}
}
