package savedscreen_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/savedscreen"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestFit covers fitting a saved screen to the space it is drawn in.
func TestFit(t *testing.T) {
	tests := []struct {
		name   string
		screen string
		width  int
		height int
		want   []string
	}{
		{
			name:   "a screen the terminal's size is drawn as it was",
			screen: "one\ntwo",
			width:  3,
			height: 2,
			want:   []string{"one", "two"},
		},
		{
			name:   "a narrower terminal cuts each line at the right",
			screen: "abcdef\nghijkl",
			width:  3,
			height: 2,
			want:   []string{"abc", "ghi"},
		},
		{
			name:   "a bigger terminal gets a row for each of its own",
			screen: "ab",
			width:  4,
			height: 2,
			want:   []string{"ab", ""},
		},
		{
			name:   "a shorter terminal collapses the blank run above the input box",
			screen: "said\n\n\n\n❯ \nfooter",
			width:  6,
			height: 4,
			want:   []string{"said", "", "❯", "footer"},
		},
		{
			name:   "a screen still too tall once collapsed loses its top",
			screen: "first\n\n\nsecond\nthird\nfooter",
			width:  6,
			height: 3,
			want:   []string{"second", "third", "footer"},
		},
		{
			name:   "colour escapes do not count toward the width",
			screen: "\x1b[31mred\x1b[0m text",
			width:  3,
			height: 1,
			want:   []string{"red"},
		},
		{
			name:   "tabs are spent as spaces to the next stop",
			screen: "a\tb",
			width:  9,
			height: 1,
			want:   []string{"a       b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := strings.Split(ansi.Strip(savedscreen.Fit(tt.screen, tt.width, tt.height)), "\n")

			assert.Equal(t, tt.want, got)
		})
	}
}
