package layout

import (
	"testing"

	"komrad/config"

	"github.com/stretchr/testify/assert"
)

// TestNewSpecSizes covers the sizes tmux is handed, where the sidebar is a count of columns and
// the info pane may still be a share of what it divides.
func TestNewSpecSizes(t *testing.T) {
	tests := []struct {
		name     string
		layout   config.Layout
		wantNav  int
		wantInfo string
	}{
		{
			name:     "a layout naming nothing falls back to the defaults",
			layout:   config.Layout{},
			wantNav:  config.DefaultSidebarWidth,
			wantInfo: "40%",
		},
		{
			name:     "a width in columns is handed over as it is",
			layout:   config.Layout{SidebarWidth: 50},
			wantNav:  50,
			wantInfo: "40%",
		},
		{
			name:     "a height in rows is too",
			layout:   config.Layout{InfoHeight: 12},
			wantNav:  config.DefaultSidebarWidth,
			wantInfo: "12",
		},
		{
			name:     "a share of the stage snapshots to a percent",
			layout:   config.Layout{InfoHeight: 0.335},
			wantNav:  config.DefaultSidebarWidth,
			wantInfo: "34%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := newSpec(tt.layout)

			assert.Equal(t, tt.wantNav, spec.sidebarCols)
			assert.Equal(t, tt.wantInfo, spec.infoSize())
		})
	}
}

// TestNewSpecSides covers which side of the stage the info pane opens on, below it when the config
// names no side.
func TestNewSpecSides(t *testing.T) {
	tests := []struct {
		name            string
		layout          config.Layout
		wantBottomAbove bool
	}{
		{
			name:   "a layout naming no side keeps the info pane below",
			layout: config.Layout{},
		},
		{
			name:            "the info pane above the stage",
			layout:          config.Layout{InfoSide: config.InfoSideAbove},
			wantBottomAbove: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := newSpec(tt.layout)

			assert.Equal(t, tt.wantBottomAbove, spec.infoAbove)
		})
	}
}
