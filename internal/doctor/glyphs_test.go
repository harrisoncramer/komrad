package doctor

import (
	"context"
	"errors"
	"testing"

	"komrad/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGlyphFontCheck covers a machine with the recommended font, with another Nerd Font, and
// with none at all.
func TestGlyphFontCheck(t *testing.T) {
	tests := []struct {
		name       string
		fonts      []string
		listErr    error
		wantStatus Status
		wantErr    error
		wantDetail string
	}{
		{
			name:       "the recommended font as a file name",
			fonts:      []string{"Menlo.ttc", "JetBrainsMonoNerdFont-Regular.ttf"},
			wantStatus: StatusOK,
			wantDetail: "JetBrainsMono Nerd Font is installed",
		},
		{
			name:       "the recommended font as a family name",
			fonts:      []string{"DejaVu Sans", "JetBrainsMono Nerd Font Mono"},
			wantStatus: StatusOK,
			wantDetail: "JetBrainsMono Nerd Font is installed",
		},
		{
			name:       "another nerd font",
			fonts:      []string{"FiraCode Nerd Font"},
			wantStatus: StatusOK,
			wantDetail: "FiraCode Nerd Font is installed, though",
		},
		{
			name:       "no nerd font",
			fonts:      []string{"Menlo.ttc", "JetBrainsMono-Regular.ttf"},
			wantStatus: StatusWarn,
			wantErr:    errNerdFontMissing,
		},
		{
			name:       "fonts that cannot be listed",
			listErr:    errors.New("fc-list: not found"),
			wantStatus: StatusWarn,
			wantErr:    errFontsUnlisted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{ListFonts: func(context.Context) ([]string, error) { return tt.fonts, tt.listErr }}

			result := newGlyphFontCheck().Run(t.Context(), env)
			require.Equal(t, tt.wantStatus, result.Status, result.Detail)
			if tt.wantErr != nil {
				require.ErrorIs(t, result.Err, tt.wantErr)
			}
			assert.Contains(t, result.Detail, tt.wantDetail)
		})
	}
}

// TestGlyphCollisionCheck covers the defaults, which never collide, and a config giving two
// glyphs in one sidebar column the same character.
func TestGlyphCollisionCheck(t *testing.T) {
	colliding := config.Defaults()
	colliding.Glyphs.PullRequest.Draft = colliding.Glyphs.PullRequest.Open

	elsewhere := config.Defaults()
	elsewhere.Glyphs.Review.Held = elsewhere.Glyphs.Schedules.Disabled

	tests := []struct {
		name       string
		cfg        *config.Config
		wantStatus Status
		wantErr    string
	}{
		{
			name:       "the defaults",
			cfg:        config.Defaults(),
			wantStatus: StatusOK,
		},
		{
			name:       "two pull request glyphs drawn as one character",
			cfg:        colliding,
			wantStatus: StatusWarn,
			wantErr:    "glyphs.pull_request.open, glyphs.pull_request.draft all draw U+",
		},
		{
			name:       "a shared character outside the sidebar",
			cfg:        elsewhere,
			wantStatus: StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := Env{LoadConfig: func() (*config.Config, error) { return tt.cfg, nil }}

			result := newGlyphCollisionCheck().Run(t.Context(), env)
			require.Equal(t, tt.wantStatus, result.Status, result.Err)
			if tt.wantErr != "" {
				require.ErrorIs(t, result.Err, errGlyphCollision)
				assert.Contains(t, result.Err.Error(), tt.wantErr)
			}
		})
	}
}
