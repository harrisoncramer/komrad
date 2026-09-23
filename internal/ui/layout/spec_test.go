package layout

import (
	"testing"

	"weave/config"

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
			wantNav:  30,
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
			wantNav:  30,
			wantInfo: "12",
		},
		{
			name:     "a share of the stage rounds to a percent",
			layout:   config.Layout{InfoHeight: 0.335},
			wantNav:  30,
			wantInfo: "34%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := newSpec(tt.layout)

			assert.Equal(t, tt.wantNav, spec.sidebarCols)
			assert.Equal(t, tt.wantInfo, spec.infoSize)
		})
	}
}

// TestNewSpecSides covers which side each region sits on, where anything the config does not
// name is the side the panel has always put it.
func TestNewSpecSides(t *testing.T) {
	tests := []struct {
		name            string
		layout          config.Layout
		wantNavOnRight  bool
		wantBottomAbove bool
	}{
		{
			name:   "a layout naming no sides keeps the sidebar left and the info pane below",
			layout: config.Layout{},
		},
		{
			name:           "the sidebar on the right",
			layout:         config.Layout{SidebarSide: config.SidebarSideRight},
			wantNavOnRight: true,
		},
		{
			name:            "the info pane above the stage",
			layout:          config.Layout{InfoSide: config.InfoSideAbove},
			wantBottomAbove: true,
		},
		{
			name:            "both moved at once",
			layout:          config.Layout{SidebarSide: config.SidebarSideRight, InfoSide: config.InfoSideAbove},
			wantNavOnRight:  true,
			wantBottomAbove: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := newSpec(tt.layout)

			assert.Equal(t, tt.wantNavOnRight, spec.sidebarOnRight)
			assert.Equal(t, tt.wantBottomAbove, spec.infoAbove)
		})
	}
}
