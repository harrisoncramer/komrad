package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const kanagawa = `
[font]
size = 17.0

[[colors.indexed_colors]]
color = "0xffa066"
index = 16

[colors.bright]
black = "0x727169"
blue = "0x7fb4ca"
cyan = "0x7aa89f"
green = "0x98bb6c"
magenta = "0x938aa9"
red = "0xe82424"
white = "0xdcd7ba"
yellow = "0xe6c384"

[colors.normal]
black = "0x090618"
blue = "0x7e9cd8"
cyan = "0x6a9589"
green = "0x76946a"
magenta = "0x957fb8"
red = "0xc34043"
white = "0xc8c093"
yellow = "0xc0a36e"

[colors.primary]
background = "#1f1f28"
foreground = '0xdcd7ba'
`

// TestParseAlacrittyTheme covers the agg palette an alacritty config becomes and the
// configs that are refused.
func TestParseAlacrittyTheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		config  string
		want    string
		wantErr string
	}{
		{
			name:    "no colours",
			config:  "[font]\nsize = 17.0\n",
			wantErr: "no usable colors.primary.background",
		},
		{
			name:    "a missing bright colour",
			config:  strings.Replace(kanagawa, `white = "0xdcd7ba"`, "", 1),
			wantErr: "no usable colors.bright.white",
		},
		{
			name:   "kanagawa",
			config: kanagawa,
			want: "1f1f28,dcd7ba," +
				"090618,c34043,76946a,c0a36e,7e9cd8,957fb8,6a9589,c8c093," +
				"727169,e82424,98bb6c,e6c384,7fb4ca,938aa9,7aa89f,dcd7ba",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseAlacrittyTheme(strings.NewReader(tt.config))
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
