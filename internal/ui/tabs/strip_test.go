package tabs_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/tabs"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestTabStripView covers the strip rendered in the tones of where the keys are.
func TestTabStripView(t *testing.T) {
	muted := paint.Panel.Style(paint.ToneMuted)
	accent := paint.Panel.Cursor()
	title := muted.Render("f") + "   "
	dot := "  " + muted.Render("·") + "  "

	tests := []struct {
		name  string
		state tabs.State
		want  string
	}{
		{
			name:  "a choice the strip does not hold falls back",
			state: tabs.State{ChosenColumn: "gone"},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "the zero state is the fallback with its arrow down, on the list's keys",
			state: tabs.State{},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "a chosen tab keeps its arrow when the list holds the keys",
			state: tabs.State{ChosenColumn: "age", Direction: tabs.Up},
			want:  title + muted.Render("size") + "  " + muted.Render("↑ age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "the strip holding the keys renders the chosen tab in the accent",
			state: tabs.State{ChosenColumn: "age", IsFocused: true},
			want:  title + muted.Render("size") + "  " + accent.Render("↓ age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name: "a focus moved off the chosen tab lights the tab it stands on and leaves the arrow",
			state: tabs.State{
				ChosenColumn:  "age",
				IsFocused:     true,
				FocusedColumn: "name",
			},
			want: title + muted.Render("size") + "  " + muted.Render("↓ age") + "  " + accent.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "a flat list names itself on the stop",
			state: tabs.State{AreHeadingsOff: true},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("flat"),
		},
		{
			name:  "the focus on the stop lights it and not the column",
			state: tabs.State{IsFocused: true, IsOnGroupStop: true},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + accent.Render("repo"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildGroupedStrip().View(tt.state, 0, nil))
		})
	}

	t.Run("a strip wider than the pane ends in an ellipsis", func(t *testing.T) {
		rendered := buildGroupedStrip().View(tabs.State{}, 12, nil)

		assert.Equal(t, 12, lipgloss.Width(rendered))
		assert.True(t, strings.HasSuffix(ansi.Strip(rendered), "…"))
	})
}

// TestTabStripStep covers tab and shift+tab walking the focus along the strip and wrapping at both ends.
func TestTabStripStep(t *testing.T) {
	tests := []struct {
		name  string
		from  tabs.State
		delta int
		want  tabs.State
	}{
		{
			name:  "back from the first column wraps to the stop",
			from:  tabs.State{ChosenColumn: "size"},
			delta: -1,
			want:  tabs.State{ChosenColumn: "size", IsOnGroupStop: true},
		},
		{
			name: "forward from the stop wraps to the first column",
			from: tabs.State{
				ChosenColumn:   "name",
				Direction:      tabs.Up,
				IsOnGroupStop:  true,
				AreHeadingsOff: true,
			},
			delta: 1,
			want: tabs.State{
				ChosenColumn:   "name",
				Direction:      tabs.Up,
				FocusedColumn:  "size",
				AreHeadingsOff: true,
			},
		},
		{
			name: "back from the stop lands on the last column",
			from: tabs.State{
				ChosenColumn:  "age",
				Direction:     tabs.Up,
				IsOnGroupStop: true,
			},
			delta: -1,
			want: tabs.State{
				ChosenColumn:  "age",
				Direction:     tabs.Up,
				FocusedColumn: "name",
			},
		},
		{
			name:  "forward from the last column lands on the stop",
			from:  tabs.State{ChosenColumn: "size", FocusedColumn: "name"},
			delta: 1,
			want:  tabs.State{ChosenColumn: "size", IsOnGroupStop: true},
		},
		{
			name:  "forward steps to the next column",
			from:  tabs.State{ChosenColumn: "size", Direction: tabs.Up},
			delta: 1,
			want: tabs.State{
				ChosenColumn:  "size",
				Direction:     tabs.Up,
				FocusedColumn: "age",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.from

			buildGroupedStrip().Step(&state, tt.delta)

			assert.Equal(t, tt.want, state)
		})
	}
}

func buildGroupedStrip() tabs.Strip {
	return tabs.Strip{
		Title:  "f",
		Legend: "sort",
		Options: []tabs.Option{
			{ID: "size", Label: "size"},
			{ID: "age", Label: "age"},
			{ID: "name", Label: "name"},
		},
		Fallback:     "size",
		GroupedLabel: "repo",
	}
}
