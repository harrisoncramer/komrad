package zone_test

import (
	"strings"
	"testing"

	"komrad/internal/panes"
	"komrad/internal/ui/zone"

	"github.com/stretchr/testify/assert"
)

// TestScan covers where a marked block is found in a frame.
func TestScan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		frame string
		want  map[zone.Name]panes.Rect
	}{
		{
			name:  "an empty block is not marked",
			frame: zone.MarkBlock("list", "", 4),
			want:  map[zone.Name]panes.Rect{},
		},
		{
			name:  "an end with no start is ignored",
			frame: strings.SplitN(zone.MarkBlock("list", "row", 3), "row", 2)[1],
			want:  map[zone.Name]panes.Rect{},
		},
		{
			name:  "a block on one line",
			frame: "ab" + zone.MarkBlock("list", "cd", 4) + "ef",
			want:  map[zone.Name]panes.Rect{"list": {Pane: "list", Left: 2, Top: 0, Width: 4, Height: 1}},
		},
		{
			name:  "a block over several lines",
			frame: "top\n" + zone.MarkBlock("column", "one\ntwo\nthree", 5) + "\nbottom",
			want:  map[zone.Name]panes.Rect{"column": {Pane: "column", Left: 0, Top: 1, Width: 5, Height: 3}},
		},
		{
			name:  "a block ending on a line of its own leaves that line out",
			frame: zone.MarkBlock("details", "one\ntwo\n", 3),
			want:  map[zone.Name]panes.Rect{"details": {Pane: "details", Left: 0, Top: 0, Width: 3, Height: 2}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, zone.Scan(tt.frame))
		})
	}
}

// TestHitAt covers which marked row a column of a line lands on.
func TestHitAt(t *testing.T) {
	t.Parallel()

	line := zone.MarkKey("parentmenu", 10, 0, "1") + "ab" + zone.MarkKey("menu", 3, 2, "row:7") + "cde" + "fghij"

	tests := []struct {
		name   string
		x      int
		want   zone.Hit
		wantOK bool
	}{
		{
			name:   "a column past every mark",
			x:      10,
			want:   zone.Hit{},
			wantOK: false,
		},
		{
			name:   "a column under the outer mark only",
			x:      0,
			want:   zone.Hit{Zone: "parentmenu", Key: "1"},
			wantOK: true,
		},
		{
			name:   "a column under both marks takes the inner one",
			x:      3,
			want:   zone.Hit{Zone: "menu", Key: "row:7", Occurrence: 2},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := zone.HitAt(line, tt.x)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestStripKeys covers row marks taken out of a frame, block marks kept.
func TestStripKeys(t *testing.T) {
	t.Parallel()

	block := zone.MarkBlock("list", "row", 3)
	frame := strings.Join([]string{zone.MarkKey("menu", 3, 0, "a") + "one", block}, "\n")

	assert.Equal(t, "one\n"+block, zone.StripKeys(frame))
}
