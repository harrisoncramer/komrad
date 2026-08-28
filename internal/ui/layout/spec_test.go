package layout

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
)

// TestNewSpecSizes covers the sizes tmux is handed, where the stage takes what the sidebar
// leaves so the two are the whole window.
func TestNewSpecSizes(t *testing.T) {
	tests := []struct {
		name     string
		layout   config.Layout
		wantNav  string
		wantMain string
		wantInfo string
	}{
		{
			name:     "a layout naming nothing falls back to the defaults",
			layout:   config.Layout{},
			wantNav:  "20%",
			wantMain: "80%",
			wantInfo: "40%",
		},
		{
			name:     "a share that does not land on a whole percent rounds",
			layout:   config.Layout{SidebarWidth: 0.335},
			wantNav:  "34%",
			wantMain: "66%",
			wantInfo: "40%",
		},
		{
			name:     "a width in columns is handed over as it is",
			layout:   config.Layout{SidebarWidth: 40},
			wantNav:  "40",
			wantMain: "50%",
			wantInfo: "40%",
		},
		{
			name:     "so is a height in rows",
			layout:   config.Layout{InfoHeight: 12},
			wantNav:  "20%",
			wantMain: "80%",
			wantInfo: "12",
		},
		{
			name:     "an even split",
			layout:   config.Layout{SidebarWidth: 0.5, InfoHeight: 0.5},
			wantNav:  "50%",
			wantMain: "50%",
			wantInfo: "50%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := NewSpec(tt.layout)

			assert.Equal(t, tt.wantNav, spec.sidebarSize)
			assert.Equal(t, tt.wantMain, spec.stageSize)
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
			spec := NewSpec(tt.layout)

			assert.Equal(t, tt.wantNavOnRight, spec.sidebarOnRight)
			assert.Equal(t, tt.wantBottomAbove, spec.infoAbove)
		})
	}
}
