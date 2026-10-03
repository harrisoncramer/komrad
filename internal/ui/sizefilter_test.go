package ui_test

import (
	"testing"
	"time"

	"komrad/internal/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

// TestSizeFilter covers which window sizes reach the panel while a pane zooms or unzooms.
func TestSizeFilter(t *testing.T) {
	t.Parallel()

	zoomed := tea.WindowSizeMsg{Width: 104, Height: 31}
	unzoomed := tea.WindowSizeMsg{Width: 47, Height: 31}

	type step struct {
		msg    tea.Msg
		expect *tea.WindowSizeMsg
		after  time.Duration
	}

	tests := []struct {
		name     string
		steps    []step
		want     []tea.Msg
		rechecks int
	}{
		{
			name:  "a size with nothing expected goes through",
			steps: []step{{msg: unzoomed}},
			want:  []tea.Msg{unzoomed},
		},
		{
			name:  "the expected size goes through",
			steps: []step{{expect: &zoomed}, {msg: zoomed}},
			want:  []tea.Msg{zoomed},
		},
		{
			name:     "a passing size after a move is dropped and checked again",
			steps:    []step{{expect: &zoomed}, {msg: unzoomed, after: 50 * time.Millisecond}, {msg: zoomed, after: 50 * time.Millisecond}},
			want:     []tea.Msg{nil, zoomed},
			rechecks: 1,
		},
		{
			name:     "two passing sizes ask for one check",
			steps:    []step{{expect: &zoomed}, {msg: unzoomed}, {msg: tea.WindowSizeMsg{Width: 51, Height: 31}}},
			want:     []tea.Msg{nil, nil},
			rechecks: 1,
		},
		{
			name:  "a size once the move has settled goes through",
			steps: []step{{expect: &zoomed}, {msg: unzoomed, after: time.Second}},
			want:  []tea.Msg{unzoomed},
		},
		{
			name:     "the check lets the next size through",
			steps:    []step{{expect: &zoomed}, {msg: unzoomed}, {msg: ui.NewSizeRecheck()}, {msg: unzoomed}},
			want:     []tea.Msg{nil, ui.NewSizeRecheck(), unzoomed},
			rechecks: 1,
		},
		{
			name:  "anything else goes through untouched",
			steps: []step{{expect: &zoomed}, {msg: tea.KeyPressMsg{Code: 'j'}}},
			want:  []tea.Msg{tea.KeyPressMsg{Code: 'j'}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
			rechecks := 0
			filter, expect := ui.NewSizeFilter(func() time.Time { return at }, func(msg tea.Msg) {
				if ui.IsSizeRecheck(msg) {
					rechecks++
				}
			})

			var got []tea.Msg
			for _, step := range tt.steps {
				at = at.Add(step.after)
				if step.expect != nil {
					expect(step.expect.Width, step.expect.Height)

					continue
				}

				got = append(got, filter(nil, step.msg))
			}

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.rechecks, rechecks)
		})
	}
}
