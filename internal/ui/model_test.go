package ui

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClampCursor pins where the cursor lands for each length of list, an empty one
// included.
func TestClampCursor(t *testing.T) {
	tests := []struct {
		name   string
		idx    int
		length int
		want   int
	}{
		{
			name:   "an empty list pins to zero",
			idx:    0,
			length: 0,
			want:   0,
		},
		{
			name:   "a negative index pins to the first row",
			idx:    -1,
			length: 3,
			want:   0,
		},
		{
			name:   "an index past the end pins to the last row",
			idx:    9,
			length: 3,
			want:   2,
		},
		{
			name:   "an index inside the list is left alone",
			idx:    2,
			length: 5,
			want:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clampCursor(tt.idx, tt.length))
		})
	}
}

// TestNewModelWiresTmux pins the tmux dependency, which the model reaches for only when a form
// opens with a blank directory.
func TestNewModelWiresTmux(t *testing.T) {
	tests := []struct {
		name string
		dir  string
		want string
	}{
		{
			name: "a blank directory is answered by the invoking client",
			want: clientPath,
		},
		{
			name: "a directory the caller gave is kept",
			dir:  "/work/api",
			want: "/work/api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{path: clientPath}
			m := newModel(Deps{
				PopupWindow: NewPopupWindow(tmux, "manager"),
				Tmux:        tmux,
				Keys:        config.DefaultKeybindings(),
				Settings:    PanelSettings{Addr: "127.0.0.1:8787"},
			}, true, nil)

			require.NotNil(t, m.tmux, "the panel cannot answer a blank directory without tmux")
			assert.Equal(t, tt.want, m.currentDir(tt.dir))
		})
	}
}

const clientPath = "/work/from-tmux"
