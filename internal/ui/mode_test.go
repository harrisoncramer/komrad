package ui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModeLabel covers the label at the foot of the sidebar naming the mode that holds the keys.
func TestModeLabel(t *testing.T) {
	rest := readBottomRow(openFocus(t))
	divider := findColumn(rest, "│")
	require.GreaterOrEqual(t, divider, 0, "the list is drawn beside a block")
	at := findColumn(rest, " NORMAL ")
	require.GreaterOrEqual(t, at, 0)
	assert.Less(t, at, divider, "the label sits in the sidebar, not the block beside it")

	tests := []struct {
		name  string
		press []string
		send  []tea.Msg
		want  string
	}{
		{name: "the picker is jump mode", press: []string{"ctrl+space", "j"}, want: "  JUMP  "},
		{name: "a pane entering copy-mode is visual mode", press: []string{"ctrl+space", "f11"}, want: " VISUAL "},
		{name: "a pane leaving copy-mode is normal mode", press: []string{"ctrl+space", "f11", "ctrl+space", "f12"}, want: " NORMAL "},
		{name: "the sidebar taking the keys is normal mode", press: []string{"ctrl+space", "f11"}, send: []tea.Msg{tea.FocusMsg{}}, want: " NORMAL "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFocus(t)
			harness.press(tt.press...)
			for _, msg := range tt.send {
				harness.send(msg)
			}

			assert.Equal(t, at, findColumn(readBottomRow(harness), tt.want), "every mode is named in the same place")
		})
	}
}

func readBottomRow(harness *panelHarness) string {
	rows := strings.Split(harness.readDrawn(), "\n")

	return rows[len(rows)-1]
}

func findColumn(row, text string) int {
	at := strings.Index(row, text)
	if at < 0 {
		return at
	}

	return ansi.StringWidth(row[:at])
}
