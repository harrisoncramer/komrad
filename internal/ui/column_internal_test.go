package ui

import (
	"testing"

	"komrad/internal/ui/column"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSplitOfRendersTheWidthItIsHanded covers the split being rendered at the measured divider.
func TestSplitOfRendersTheWidthItIsHanded(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		sidebar     int
		wantSidebar int
		wantOK      bool
	}{
		{
			name:    "a window too narrow for both halves holds neither",
			width:   60,
			sidebar: 30,
			wantOK:  false,
		},
		{
			name:        "a width in the middle of the range is rendered as it stands",
			width:       200,
			sidebar:     60,
			wantSidebar: 60,
			wantOK:      true,
		},
		{
			name:        "a width narrower than a list of names is still where the divider is",
			width:       200,
			sidebar:     12,
			wantSidebar: 12,
			wantOK:      true,
		},
		{
			name:        "so is one that leaves the block less than its floor",
			width:       100,
			sidebar:     80,
			wantSidebar: 80,
			wantOK:      true,
		},
		{
			name:        "a width past the window keeps a column for the block",
			width:       100,
			sidebar:     140,
			wantSidebar: 99,
			wantOK:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			split := splitOf(tt.width, 20, tt.sidebar, column.Narrowest)

			require.Equal(t, tt.wantOK, split.IsDivided)
			if !split.IsDivided {
				return
			}

			assert.Equal(t, tt.wantSidebar, split.SidebarWidth)
			assert.Equal(t, tt.width-tt.wantSidebar, split.ColumnWidth)
		})
	}
}
