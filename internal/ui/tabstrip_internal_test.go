package ui

import (
	"strings"
	"testing"

	"komrade/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

// TestTabStripView covers the strip drawn in the tones of where the keys are.
func TestTabStripView(t *testing.T) {
	muted := paint.Panel.Style(paint.ToneMuted)
	accent := paint.Panel.Cursor()
	title := " " + muted.Render("f") + "   "
	dot := "  " + muted.Render("·") + "  "

	tests := []struct {
		name  string
		state tabsState
		want  string
	}{
		{
			name:  "a choice the strip does not hold falls back",
			state: tabsState{chosen: "gone"},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "the zero state is the fallback with its arrow down, on the list's keys",
			state: tabsState{},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "a chosen tab keeps its arrow when the list holds the keys",
			state: tabsState{chosen: "age", direction: tabUp},
			want:  title + muted.Render("size") + "  " + muted.Render("↑ age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "the strip holding the keys draws the chosen tab in the accent",
			state: tabsState{chosen: "age", focused: true},
			want:  title + muted.Render("size") + "  " + accent.Render("↓ age") + "  " + muted.Render("name") + dot + muted.Render("repo"),
		},
		{
			name: "a focus moved off the chosen tab lights the tab it stands on and leaves the arrow",
			state: tabsState{
				chosen:  "age",
				focused: true,
				focus:   "name",
			},
			want: title + muted.Render("size") + "  " + muted.Render("↓ age") + "  " + accent.Render("name") + dot + muted.Render("repo"),
		},
		{
			name:  "a flat list names itself on the stop",
			state: tabsState{flat: true},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + muted.Render("flat"),
		},
		{
			name:  "the focus on the stop lights it and not the column",
			state: tabsState{focused: true, onGroup: true},
			want:  title + muted.Render("↓ size") + "  " + muted.Render("age") + "  " + muted.Render("name") + dot + accent.Render("repo"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildGroupedStrip().view(tt.state, paint.Panel, 0))
		})
	}

	t.Run("a strip wider than the pane ends in an ellipsis", func(t *testing.T) {
		drawn := buildGroupedStrip().view(tabsState{}, paint.Panel, 12)

		assert.Equal(t, 12, lipgloss.Width(drawn))
		assert.True(t, strings.HasSuffix(ansi.Strip(drawn), "…"))
	})
}

// TestTabStripStep covers tab and shift+tab walking the focus along the strip and wrapping at both ends.
func TestTabStripStep(t *testing.T) {
	tests := []struct {
		name  string
		from  tabsState
		delta int
		want  tabsState
	}{
		{
			name:  "back from the first column wraps to the stop",
			from:  tabsState{chosen: "size"},
			delta: -1,
			want:  tabsState{chosen: "size", onGroup: true},
		},
		{
			name: "forward from the stop wraps to the first column",
			from: tabsState{
				chosen:    "name",
				direction: tabUp,
				onGroup:   true,
				flat:      true,
			},
			delta: 1,
			want: tabsState{
				chosen:    "name",
				direction: tabUp,
				focus:     "size",
				flat:      true,
			},
		},
		{
			name: "back from the stop lands on the last column",
			from: tabsState{
				chosen:    "age",
				direction: tabUp,
				onGroup:   true,
			},
			delta: -1,
			want: tabsState{
				chosen:    "age",
				direction: tabUp,
				focus:     "name",
			},
		},
		{
			name:  "forward from the last column lands on the stop",
			from:  tabsState{chosen: "size", focus: "name"},
			delta: 1,
			want:  tabsState{chosen: "size", onGroup: true},
		},
		{
			name:  "forward steps to the next column",
			from:  tabsState{chosen: "size", direction: tabUp},
			delta: 1,
			want: tabsState{
				chosen:    "size",
				direction: tabUp,
				focus:     "age",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.from

			buildGroupedStrip().step(&state, tt.delta)

			assert.Equal(t, tt.want, state)
		})
	}
}

func buildStrip() tabStrip {
	return tabStrip{
		title:  "f",
		legend: "sort",
		options: []tabOption{
			{id: "size", label: "size"},
			{id: "age", label: "age"},
			{id: "name", label: "name"},
		},
		fallback: "size",
	}
}

func buildGroupedStrip() tabStrip {
	strip := buildStrip()
	strip.grouped = "repo"

	return strip
}
