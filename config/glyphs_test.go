package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"komrad/internal/glyph"
	"komrad/internal/loader"
)

// TestEveryGlyphHasAKey covers the config and the glyph package naming the same glyphs.
func TestEveryGlyphHasAKey(t *testing.T) {
	keys := map[string]bool{}
	defaults := DefaultGlyphs()
	eachGlyphField(&defaults, func(key string, _ reflect.Value) {
		keys[key] = true
	})

	for _, g := range glyph.All() {
		assert.True(t, keys[g.Key()], "glyph %s has no key under [glyphs]", g.Key())
	}
	assert.Len(t, keys, len(glyph.All()))
}

// TestLoadGlyphs covers what a person may write under [glyphs] and how a refusal names it.
func TestLoadGlyphs(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    map[glyph.Glyph]string
		loader  loader.Style
		wantErr string
	}{
		{
			name:   "a file that names no glyphs draws the defaults",
			file:   "default_harness = \"claude\"\n",
			want:   map[glyph.Glyph]string{glyph.FailedScript: "\uf489", glyph.Unread: "\uf49a"},
			loader: loader.Braille,
		},
		{
			name:   "an approved octicon replaces the default, the rest left alone",
			file:   "[glyphs.marks]\nfailed_script = \"\\uf467\"\n",
			want:   map[glyph.Glyph]string{glyph.FailedScript: "\uf467", glyph.Mail: "\uf42f"},
			loader: loader.Braille,
		},
		{
			name:   "a loader style replaces the braille default",
			file:   "[glyphs]\nloader = \"arcs\"\n",
			loader: loader.Arcs,
		},
		{
			name:    "an emoji is refused with its key and code point",
			file:    "[glyphs.marks]\nfailed_script = \"\\U0001F4A5\"\n",
			wantErr: "glyphs.marks.failed_script: U+1F4A5 is not an approved glyph",
		},
		{
			name:    "an empty glyph is refused",
			file:    "[glyphs.review]\nunread = \"\"\n",
			wantErr: `glyphs.review.unread: "" is not a single character`,
		},
		{
			name:    "a loader style the panel cannot draw is refused",
			file:    "[glyphs]\nloader = \"dots\"\n",
			wantErr: `glyphs.loader: "dots" is not one of braille, arcs, ascii`,
		},
		{
			name:    "a key no glyph sits at is refused",
			file:    "[glyphs.row]\nbusy = \"┃\"\n",
			wantErr: "busy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			require.NoError(t, os.WriteFile(path, []byte(tt.file), 0o644))

			cfg, err := LoadStack(Stack{Paths: []string{path}})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}
			require.NoError(t, err)

			assert.Equal(t, tt.loader, loader.Style(cfg.Glyphs.Loader))

			set, err := cfg.Glyphs.Set()
			require.NoError(t, err)
			for g, want := range tt.want {
				assert.Equal(t, want, set.Spell(g), "%s", g.Key())
			}
		})
	}
}
