package stage

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func keyMsg() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("R")} }

// TestViewNeverOutgrowsThePane covers a frame taller than the pane, which bubbletea would draw
// as given and scroll the pane out from under it.
func TestViewNeverOutgrowsThePane(t *testing.T) {
	tests := []struct {
		name   string
		frame  string
		height int
		want   int
	}{
		{name: "a frame shorter than the pane is drawn whole", frame: "a\nb\n", height: 10, want: 2},
		{name: "a frame taller than the pane is clipped to it", frame: strings.Repeat("row\n", 40), height: 10, want: 10},
		{name: "a frame exactly the height of the pane is left alone", frame: strings.Repeat("row\n", 10), height: 10, want: 10},
		{name: "an unmeasured pane draws what it was given", frame: "a\nb\n", height: 0, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drawn := model{frame: tt.frame, height: tt.height}.View()

			assert.Equal(t, tt.want, strings.Count(drawn, "\n")+1)
			assert.False(t, strings.HasSuffix(drawn, "\n"), "a trailing newline is a row the pane does not have")
		})
	}
}

// TestUpdateRedrawsOnAKey covers the whole protocol between the panel and the drawer: the panel
// rewrites the file and taps a key, and the drawer re-reads.
func TestUpdateRedrawsOnAKey(t *testing.T) {
	next, cmd := model{path: "/does/not/exist"}.Update(keyMsg())

	assert.NotNil(t, cmd, "a key means the file has changed")
	assert.Empty(t, next.(model).frame)

	// A file it cannot read leaves the pane blank rather than taking the pane down.
	assert.Equal(t, frameMsg{}, cmd())
}
