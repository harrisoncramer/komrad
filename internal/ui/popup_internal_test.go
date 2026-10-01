package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPlacePopup covers where a popup stands beside the cursor and the code.
func TestPlacePopup(t *testing.T) {
	tests := []struct {
		name   string
		anchor popupAnchor
		top    int
		cursor int
		tall   int
		want   popupSlot
	}{
		{
			name:   "it stands over the cursor's row when there is room above",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			cursor: 30,
			tall:   10,
			want: popupSlot{
				x:      0,
				y:      20,
				width:  61,
				height: 10,
			},
		},
		{
			name:   "it stands under the cursor's row when there is no room above",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			cursor: 3,
			tall:   10,
			want: popupSlot{
				x:      0,
				y:      4,
				width:  61,
				height: 10,
			},
		},
		{
			name:   "it grows no taller than its tallest",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			cursor: 30,
			tall:   100,
			want: popupSlot{
				x:      0,
				y:      30 - popupHeight,
				width:  61,
				height: popupHeight,
			},
		},
		{
			name:   "it shrinks into the room above when neither side holds it and above is roomier",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			cursor: 20,
			tall:   30,
			want: popupSlot{
				x:      0,
				y:      0,
				width:  61,
				height: 20,
			},
		},
		{
			name:   "it shrinks into the room below when neither side holds it and below is roomier",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			cursor: 18,
			tall:   30,
			want: popupSlot{
				x:      0,
				y:      19,
				width:  61,
				height: 21,
			},
		},
		{
			name:   "it stands under the cursor's row when the rows kept clear at the top leave no room above",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			top:    4,
			cursor: 10,
			tall:   10,
			want: popupSlot{
				x:      0,
				y:      11,
				width:  61,
				height: 10,
			},
		},
		{
			name:   "it shrinks into the room above no higher than the rows kept clear at the top",
			anchor: popupAnchor{right: 61, codeOnRight: true},
			top:    4,
			cursor: 24,
			tall:   30,
			want: popupSlot{
				x:      0,
				y:      4,
				width:  61,
				height: 20,
			},
		},
		{
			name:   "it stands against code on its right",
			anchor: popupAnchor{right: 120, codeOnRight: true},
			cursor: 30,
			tall:   10,
			want: popupSlot{
				x:      28,
				y:      20,
				width:  popupWidth,
				height: 10,
			},
		},
		{
			name:   "it stands against code on its left",
			anchor: popupAnchor{left: 110, right: 160},
			cursor: 30,
			tall:   10,
			want: popupSlot{
				x:      110,
				y:      20,
				width:  50,
				height: 10,
			},
		},
		{
			name:   "it runs into code on its right rather than shrink past its narrowest",
			anchor: popupAnchor{right: 30, codeOnRight: true},
			cursor: 30,
			tall:   10,
			want: popupSlot{
				x:      0,
				y:      20,
				width:  popupMinWidth,
				height: 10,
			},
		},
		{
			name:   "it runs into code on its left rather than shrink past its narrowest",
			anchor: popupAnchor{left: 140, right: 160},
			cursor: 30,
			tall:   10,
			want: popupSlot{
				x:      120,
				y:      20,
				width:  popupMinWidth,
				height: 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, placePopup(tt.anchor, popupStage{
				top:    tt.top,
				cursor: tt.cursor,
				rows:   40,
			}, tt.tall))
		})
	}
}
