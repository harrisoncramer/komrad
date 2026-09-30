package theme

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheck covers which spellings a colour may take and how a refusal names it.
func TestCheck(t *testing.T) {
	tests := []struct {
		name    string
		change  func(colors *Colors)
		wantErr string
	}{
		{
			name:    "a colour number past the terminal's range",
			change:  func(colors *Colors) { colors.Primary = "256" },
			wantErr: `failed to read primary: not a terminal colour number from 0 to 255 or a hex code like #1e3a5f: "256"`,
		},
		{
			name:    "a word instead of a colour",
			change:  func(colors *Colors) { colors.UserGithub = "orange" },
			wantErr: "user_github",
		},
		{
			name:    "a hex code that is too short",
			change:  func(colors *Colors) { colors.Match.Dark = "#fff" },
			wantErr: "match.dark",
		},
		{
			name:    "a hex code holding something that is not hex",
			change:  func(colors *Colors) { colors.Selection.Light = "#12345g" },
			wantErr: "selection.light",
		},
		{
			name: "hex codes and colour numbers side by side",
			change: func(colors *Colors) {
				colors.Highlight = "#FF8800"
				colors.Inverse = "255"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			colors := Defaults()
			tt.change(&colors)

			err := Check(colors)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, ErrBadColor)
				assert.ErrorContains(t, err, tt.wantErr)

				return
			}

			assert.NoError(t, err)
		})
	}
}
