package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadPalette covers what a person may write under [palette] and how a refusal names it.
func TestLoadPalette(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantPrimary string
		wantErr     string
	}{
		{
			name:    "a colour that is not one is refused with its key",
			file:    "[palette.diff_added]\ndark = \"green\"\n",
			wantErr: `palette: failed to read diff_added.dark`,
		},
		{
			name:    "a key no colour sits at is refused",
			file:    "[palette]\naccent = \"4\"\n",
			wantErr: "accent",
		},
		{
			name:        "a colour replaces its default, the rest left alone",
			file:        "[palette]\nprimary = \"#ff8800\"\n",
			wantPrimary: "#ff8800",
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

			assert.Equal(t, tt.wantPrimary, cfg.Palette.Primary)
			assert.Equal(t, DefaultPalette().Muted, cfg.Palette.Muted)
		})
	}
}
