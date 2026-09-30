package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPlacePopup covers where a popup stands beside the cursor and how it keeps clear of code.
func TestPlacePopup(t *testing.T) {
	tests := []struct {
		name   string
		text   int
		anchor popupAnchor
		want   popupSlot
	}{
		{
			name:   "it stands over the cursor's row when there is room above",
			text:   20,
			anchor: popupAnchor{cursor: 20, edge: 150, right: 150, code: 13, keepsClear: true},
			want:   popupSlot{x: 58, y: 6, width: popupWidth, height: popupHeight},
		},
		{
			name:   "it stands under the cursor's row when there is no room above",
			text:   20,
			anchor: popupAnchor{cursor: 3, edge: 150, right: 150, code: 13, keepsClear: true},
			want:   popupSlot{x: 58, y: 4, width: popupWidth, height: popupHeight},
		},
		{
			name:   "it runs past its edge to keep clear of long code",
			text:   70,
			anchor: popupAnchor{cursor: 20, edge: 100, right: 150, code: 13, keepsClear: true},
			want:   popupSlot{x: 71, y: 6, width: 79, height: popupHeight},
		},
		{
			name:   "it covers code rather than shrink past its narrowest",
			text:   70,
			anchor: popupAnchor{cursor: 20, edge: 100, right: 100, code: 13, keepsClear: true},
			want:   popupSlot{x: 60, y: 6, width: popupMinWidth, height: popupHeight},
		},
		{
			name:   "it slides over the room past its edge before covering code",
			text:   115,
			anchor: popupAnchor{cursor: 20, edge: 100, right: 150, code: 13, keepsClear: true},
			want:   popupSlot{x: 110, y: 6, width: popupMinWidth, height: popupHeight},
		},
		{
			name:   "it takes the whole width when it keeps nothing clear",
			text:   60,
			anchor: popupAnchor{cursor: 20, edge: 60, right: 60},
			want:   popupSlot{x: 0, y: 6, width: 60, height: popupHeight},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := strings.Repeat("x", tt.text)
			area := strings.Join(slices.Repeat([]string{line}, 30), "\n")

			assert.Equal(t, tt.want, placePopup(area, tt.anchor))
		})
	}
}
