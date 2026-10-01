package tmux

import (
	"context"
	"fmt"
	"os"
	"testing"

	"komrad/internal/panes"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestMain runs the test binary as a stand-in when a test hands it to tmux as komrad visual.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "visual" {
		if err := RunCopyStandIn(context.Background(), os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		os.Exit(0)
	}

	os.Exit(m.Run())
}

// TestLayOutTiles covers the tiles a stand-in cuts the sidebar into along the borders between its
// blocks, and the tile the keys land in.
func TestLayOutTiles(t *testing.T) {
	tests := []struct {
		name      string
		spec      standInSpec
		want      []copyTile
		wantFocus string
	}{
		{
			name: "no border keeps one tile",
			spec: standInSpec{
				Width:   40,
				Height:  20,
				Divider: -1,
				Rule:    -1,
				FocusX:  5,
				FocusY:  5,
			},
			want:      []copyTile{{pane: "%0", rect: panes.Rect{Width: 40, Height: 20}}},
			wantFocus: "%0",
		},
		{
			name: "a divider parts the list from the column on its right",
			spec: standInSpec{
				Width:    40,
				Height:   20,
				Divider:  10,
				Rule:     -1,
				RuleLeft: true,
				FocusX:   20,
				FocusY:   5,
			},
			want: []copyTile{
				{pane: "%0", rect: panes.Rect{Width: 10, Height: 20}},
				{pane: "%1", rect: panes.Rect{
					Left:   11,
					Width:  29,
					Height: 20,
				}},
			},
			wantFocus: "%1",
		},
		{
			name: "a rule parts the record from the list above it",
			spec: standInSpec{
				Width:    40,
				Height:   20,
				Divider:  10,
				Rule:     12,
				RuleLeft: true,
				FocusX:   3,
				FocusY:   15,
			},
			want: []copyTile{
				{pane: "%0", rect: panes.Rect{Width: 10, Height: 12}},
				{pane: "%1", rect: panes.Rect{
					Left:   11,
					Width:  29,
					Height: 20,
				}},
				{pane: "%2", rect: panes.Rect{
					Top:    13,
					Width:  10,
					Height: 7,
				}},
			},
			wantFocus: "%2",
		},
		{
			name: "a rule parts the list on the right when the sidebar is there",
			spec: standInSpec{
				Width:   40,
				Height:  20,
				Divider: 29,
				Rule:    12,
				FocusX:  35,
				FocusY:  3,
			},
			want: []copyTile{
				{pane: "%0", rect: panes.Rect{Width: 29, Height: 20}},
				{pane: "%1", rect: panes.Rect{
					Left:   30,
					Width:  10,
					Height: 12,
				}},
				{pane: "%2", rect: panes.Rect{
					Left:   30,
					Top:    13,
					Width:  10,
					Height: 7,
				}},
			},
			wantFocus: "%1",
		},
		{
			name: "a focus outside every tile lands in the first",
			spec: standInSpec{
				Width:   40,
				Height:  20,
				Divider: -1,
				Rule:    -1,
				FocusX:  99,
				FocusY:  99,
			},
			want:      []copyTile{{pane: "%0", rect: panes.Rect{Width: 40, Height: 20}}},
			wantFocus: "%0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tiles := layOutTiles(&tt.spec)

			assert.Equal(t, tt.want, tiles)
			assert.Equal(t, tt.wantFocus, findFocusTile(&tt.spec, tiles).pane)
		})
	}
}

// TestSliceTile covers a tile's share of the sidebar keeping the cells it was drawn in.
func TestSliceTile(t *testing.T) {
	label := "\x1b[1;35m VISUAL \x1b[0m"
	rows := []string{"list│one", "list│" + label, "list│三x"}

	tile := sliceTile(rows, panes.Rect{
		Left:   5,
		Width:  8,
		Height: 5,
	})

	assert.Equal(t, "one\n VISUAL \n三x", ansi.Strip(tile), "a label keeps the spaces its style is drawn on")
	assert.Contains(t, tile, label, "the tile keeps the colours it was drawn in")
}
