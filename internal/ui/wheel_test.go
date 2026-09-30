package ui_test

import (
	"testing"
	"time"

	"komrad/internal/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
)

// TestWheelFilter covers the wheel's turns being added up ahead of the update loop.
func TestWheelFilter(t *testing.T) {
	t.Parallel()

	down := tea.MouseWheelMsg{X: 7, Button: tea.MouseWheelDown}
	up := tea.MouseWheelMsg{X: 7, Button: tea.MouseWheelUp}

	type turn struct {
		msg   tea.Msg
		after time.Duration
	}

	tests := []struct {
		name  string
		turns []turn
		want  []tea.Msg
	}{
		{
			name:  "a lone turn goes through at once",
			turns: []turn{{msg: down}},
			want:  []tea.Msg{ui.NewWheelTurns(7, 1)},
		},
		{
			name:  "turns inside a frame are held for the first turn after it",
			turns: []turn{{msg: down}, {msg: down, after: 6 * time.Millisecond}, {msg: down, after: 6 * time.Millisecond}, {msg: down, after: 6 * time.Millisecond}},
			want:  []tea.Msg{ui.NewWheelTurns(7, 1), nil, nil, ui.NewWheelTurns(7, 3)},
		},
		{
			name:  "turns held when the wheel stops are dropped",
			turns: []turn{{msg: down}, {msg: down, after: time.Millisecond}, {msg: down, after: time.Millisecond}, {msg: down, after: 10 * time.Second}},
			want:  []tea.Msg{ui.NewWheelTurns(7, 1), nil, nil, ui.NewWheelTurns(7, 1)},
		},
		{
			name:  "a turn the other way drops the turns held",
			turns: []turn{{msg: down}, {msg: down, after: time.Millisecond}, {msg: up, after: 20 * time.Millisecond}},
			want:  []tea.Msg{ui.NewWheelTurns(7, 1), nil, ui.NewWheelTurns(7, -1)},
		},
		{
			name:  "a sideways turn goes through untouched",
			turns: []turn{{msg: tea.MouseWheelMsg{Button: tea.MouseWheelLeft}}},
			want:  []tea.Msg{tea.MouseWheelMsg{Button: tea.MouseWheelLeft}},
		},
		{
			name:  "anything else goes through untouched",
			turns: []turn{{msg: down}, {msg: tea.KeyPressMsg{Code: 'j'}, after: time.Millisecond}},
			want:  []tea.Msg{ui.NewWheelTurns(7, 1), tea.KeyPressMsg{Code: 'j'}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
			filter := ui.NewWheelFilter(func() time.Time { return at })

			var got []tea.Msg
			for _, turn := range tt.turns {
				at = at.Add(turn.after)
				got = append(got, filter(nil, turn.msg))
			}

			assert.Equal(t, tt.want, got)
		})
	}
}
